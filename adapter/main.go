// Command adapter turns light triggers into full entities (spec §9). It
// consumes triggers in group adl-adapter, looks the entity up in Dragonfly,
// fetches it from datacore over gRPC when needed, publishes it to the topic of
// its type, and dead-letters what can never succeed. Delivery is at least once.
//
// Usage: adapter [serve]   run the service (default)
//
//	adapter healthcheck     exit 0 if /healthz answers 200 on localhost
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/cristianrisueo/asset-discovery-lab/adapter/internal/cache"
	"github.com/cristianrisueo/asset-discovery-lab/adapter/internal/consumer"
	"github.com/cristianrisueo/asset-discovery-lab/adapter/internal/fetcher"
	"github.com/cristianrisueo/asset-discovery-lab/adapter/internal/handler"
	"github.com/cristianrisueo/asset-discovery-lab/adapter/internal/publisher"
	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
)

// shutdownGrace bounds the batch in progress after SIGTERM, below the 10 s
// compose stop timeout.
const shutdownGrace = 7 * time.Second

func main() {
	cfg, err := loadConfig(os.LookupEnv)
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		addr := ":8091"
		if err == nil {
			addr = cfg.HTTPAddr
		}
		if herr := healthcheck(localURL(addr, "/healthz")); herr != nil {
			fmt.Fprintln(os.Stderr, "adapter healthcheck:", herr)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] != "serve" {
		fmt.Fprintf(os.Stderr, "usage: adapter [serve|healthcheck]\n")
		os.Exit(2)
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel})).With("service", "adapter")
	if err != nil {
		log.Error("invalid configuration", "error", err.Error())
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if err := run(ctx, cfg, log); err != nil {
		log.Error("adapter stopped with an error", "error", err.Error())
		os.Exit(1)
	}
	log.Info("adapter stopped")
}

func run(ctx context.Context, cfg config, log *slog.Logger) error {
	var joined atomic.Bool
	tracker := fetcher.NewTracker()

	// One franz-go client consumes triggers and produces entities and dead
	// letters: manual commits, rebalances blocked while a batch is in flight
	// (spec §9.2), idempotent producer with acks=all and a 5 ms linger (§9.5).
	kc, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.KafkaBrokers...),
		kgo.ConsumerGroup(cfg.ConsumerGroup),
		kgo.ConsumeTopics(cfg.TriggersTopic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		kgo.DisableAutoCommit(),
		kgo.BlockRebalanceOnPoll(),
		kgo.SessionTimeout(10*time.Second),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.ProducerLinger(5*time.Millisecond),
		kgo.OnPartitionsAssigned(func(_ context.Context, _ *kgo.Client, m map[string][]int32) {
			joined.Store(true)
			log.Info("partitions assigned", "partitions", partitions(m))
		}),
		kgo.OnPartitionsRevoked(func(_ context.Context, _ *kgo.Client, m map[string][]int32) {
			log.Info("partitions revoked", "partitions", partitions(m))
		}),
		kgo.OnPartitionsLost(func(_ context.Context, _ *kgo.Client, m map[string][]int32) {
			joined.Store(false)
			log.Warn("partitions lost", "partitions", partitions(m))
		}),
		kgo.WithLogger(kgoLogger{log}),
	)
	if err != nil {
		return fmt.Errorf("kafka client: %w", err)
	}

	conn, err := grpc.NewClient(cfg.DatacoreAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		kc.Close()
		return fmt.Errorf("datacore client: %w", err)
	}
	defer func() { _ = conn.Close() }()

	dragonfly := cache.New(cfg.DragonflyAddr, cfg.CacheTTL, log)
	defer func() { _ = dragonfly.Close() }()

	// life ends after the final commit: until then failed produces are retried.
	life, endLife := context.WithCancel(context.WithoutCancel(ctx))
	defer endLife()
	pub := publisher.New(life, kc, cfg.EntityTopicPrefix, cfg.DLQTopic, log)
	h := &handler.Handler{
		Cache: dragonfly,
		Fetcher: &fetcher.Fetcher{
			Client:   adlv1.NewDataCoreServiceClient(conn),
			Timeout:  cfg.GRPCTimeout,
			Backoff:  fetcher.NewBackoff(cfg.FastRetries, cfg.RetryBase, cfg.RetryMax, rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))),
			StaleMax: cfg.StaleMaxWait,
			Tracker:  tracker,
			Log:      log,
		},
		Publisher: pub,
		Log:       log,
	}
	cons := &consumer.Consumer{
		Client: kc,
		Handle: func(ctx context.Context, r *kgo.Record) consumer.Pending { return h.Handle(ctx, r) },
		Log:    log,
		Grace:  shutdownGrace,
	}

	ready := func() (bool, string) {
		if !joined.Load() {
			return false, "consumer has not joined group " + cfg.ConsumerGroup
		}
		if s := tracker.Stalled(time.Now(), TransientStallLimit); len(s) > 0 {
			return false, fmt.Sprintf("partitions %v in transient retry for more than %s", s, TransientStallLimit)
		}
		return true, ""
	}
	hs := &http.Server{Addr: cfg.HTTPAddr, Handler: healthHandler(ready), ReadHeaderTimeout: 5 * time.Second}
	httpErr := make(chan error, 1)
	go func() {
		if err := hs.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			httpErr <- err
		}
	}()
	log.Info("adapter started", "group", cfg.ConsumerGroup, "topic", cfg.TriggersTopic,
		"datacore", cfg.DatacoreAddr, "dragonfly", cfg.DragonflyAddr, "http", cfg.HTTPAddr)

	consumeCtx, stopConsume := context.WithCancel(ctx)
	defer stopConsume()
	done := make(chan struct{})
	go func() { cons.Run(consumeCtx); close(done) }()

	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-httpErr:
	}
	// SIGTERM (spec §9.8): stop polling, finish the current batch, commit
	// (all inside Run), then close.
	log.Info("adapter shutting down")
	stopConsume()
	<-done
	endLife()
	kc.Close() // leaves the group
	sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = hs.Shutdown(sctx)
	return runErr
}

func partitions(m map[string][]int32) string {
	var parts []string
	for t, ps := range m {
		for _, p := range ps {
			parts = append(parts, t+"/"+strconv.Itoa(int(p)))
		}
	}
	return strings.Join(parts, ",")
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
