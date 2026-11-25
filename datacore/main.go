// Command datacore simulates Lansweeper's DataCore (spec §7): the source of
// truth for assets, users and groups. It publishes one trigger per change
// through a transactional outbox, serves entities over gRPC, and offers the
// lab admin API.
//
// Usage: datacore [serve]   run the service (default)
//
//	datacore healthcheck     exit 0 if /readyz answers 200 on localhost
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/cristianrisueo/asset-discovery-lab/datacore/internal/chaos"
	"github.com/cristianrisueo/asset-discovery-lab/datacore/internal/generator"
	"github.com/cristianrisueo/asset-discovery-lab/datacore/internal/grpcapi"
	"github.com/cristianrisueo/asset-discovery-lab/datacore/internal/outbox"
	"github.com/cristianrisueo/asset-discovery-lab/datacore/internal/store"
	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
)

func main() {
	cfg, err := loadConfig(os.LookupEnv)
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		addr := ":8090"
		if err == nil {
			addr = cfg.HTTPAddr
		}
		if herr := healthcheck(localURL(addr, "/readyz")); herr != nil {
			fmt.Fprintln(os.Stderr, "datacore healthcheck:", herr)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] != "serve" {
		fmt.Fprintf(os.Stderr, "usage: datacore [serve|healthcheck]\n")
		os.Exit(2)
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel})).With("service", "datacore")
	if err != nil {
		log.Error("invalid configuration", "error", err.Error())
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if err := run(ctx, cfg, log); err != nil {
		log.Error("datacore stopped with an error", "error", err.Error())
		os.Exit(1)
	}
	log.Info("datacore stopped")
}

func run(ctx context.Context, cfg config, log *slog.Logger) error {
	st, err := store.Open(ctx, cfg.DBPath, cfg.TriggersTopic)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	ch, err := chaos.New(cfg.Chaos, rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())))
	if err != nil {
		return err
	}
	gen, err := generator.New(st, cfg.SeedCounts, cfg.Generator, rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())), log)
	if err != nil {
		return err
	}

	kafka, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.KafkaBrokers...),
		kgo.RequiredAcks(kgo.AllISRAcks()), // idempotent writes are franz-go's default (spec §7.5)
		kgo.ProducerLinger(5*time.Millisecond),
		kgo.WithLogger(kgoLogger{log}),
	)
	if err != nil {
		return fmt.Errorf("kafka client: %w", err)
	}
	defer kafka.Close()
	relay := outbox.New(st, kafka, ch, log, cfg.OutboxBatch, cfg.OutboxPoll)

	var seeded atomic.Bool
	ready := func() (bool, string) {
		switch {
		case !seeded.Load():
			return false, "seed in progress"
		case !relay.Connected():
			return false, "relay not connected to Kafka"
		}
		return true, ""
	}
	svc := grpcapi.New(grpcapi.Deps{Store: st, Chaos: ch, Generator: gen, Log: log, Published: relay.Published})

	// Transport: gRPC (both services and reflection) and HTTP health.
	grpcLis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.GRPCAddr, err)
	}
	gs := grpc.NewServer()
	adlv1.RegisterDataCoreServiceServer(gs, svc)
	adlv1.RegisterAdminServiceServer(gs, svc)
	reflection.Register(gs)
	hs := &http.Server{Addr: cfg.HTTPAddr, Handler: healthHandler(ready), ReadHeaderTimeout: 5 * time.Second}

	errc := make(chan error, 2)
	go func() { errc <- gs.Serve(grpcLis) }()
	go func() {
		if err := hs.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()
	log.Info("datacore listening", "grpc", cfg.GRPCAddr, "http", cfg.HTTPAddr, "db", cfg.DBPath)

	// The relay starts at once: seed rows are published while seeding runs.
	relayCtx, stopRelay := context.WithCancel(context.WithoutCancel(ctx))
	genCtx, stopGen := context.WithCancel(context.WithoutCancel(ctx))
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); relay.Run(relayCtx) }()

	seedDone := make(chan error, 1)
	go func() {
		start := time.Now()
		inserted, err := st.Seed(ctx, cfg.Seed, cfg.SeedCounts)
		if err != nil {
			seedDone <- fmt.Errorf("seed: %w", err)
			return
		}
		seeded.Store(true)
		log.Info("seed complete", "inserted", inserted, "duration_ms", time.Since(start).Milliseconds())
		seedDone <- nil
	}()

	var genWG sync.WaitGroup
	var runErr error
	select {
	case <-ctx.Done():
	case err := <-errc:
		runErr = err
	case err := <-seedDone:
		if err != nil {
			runErr = err
			break
		}
		genWG.Add(1)
		go func() { defer genWG.Done(); gen.Run(genCtx) }()
		select {
		case <-ctx.Done():
		case runErr = <-errc:
		}
	}

	// Shutdown order of spec §7.11: stop the generator (the change in
	// progress finishes), stop the relay after its current batch, then close.
	log.Info("datacore shutting down")
	stopGen()
	genWG.Wait()
	stopRelay()
	wg.Wait()
	stopped := make(chan struct{})
	go func() { gs.GracefulStop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		gs.Stop()
	}
	sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = hs.Shutdown(sctx)
	return runErr
}

// kgoLogger routes franz-go logs to slog at warn level and above.
type kgoLogger struct{ log *slog.Logger }

func (l kgoLogger) Level() kgo.LogLevel { return kgo.LogLevelWarn }

func (l kgoLogger) Log(level kgo.LogLevel, msg string, keyvals ...any) {
	lvl := slog.LevelWarn
	if level == kgo.LogLevelError {
		lvl = slog.LevelError
	}
	l.log.Log(context.Background(), lvl, "kafka: "+msg, keyvals...)
}
