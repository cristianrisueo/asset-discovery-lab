package main

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/cristianrisueo/asset-discovery-lab/datacore/internal/chaos"
	"github.com/cristianrisueo/asset-discovery-lab/datacore/internal/model"
	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
)

// config holds every variable of spec §7.10.
type config struct {
	GRPCAddr      string
	HTTPAddr      string
	DBPath        string
	KafkaBrokers  []string
	TriggersTopic string
	Seed          uint64
	SeedCounts    model.SeedCounts
	Generator     *adlv1.GeneratorConfig
	OutboxBatch   int
	OutboxPoll    time.Duration
	Chaos         *adlv1.ChaosConfig
	LogLevel      slog.Level
}

// loadConfig reads the configuration through lookup and reports every
// invalid value at once.
func loadConfig(lookup func(string) (string, bool)) (config, error) {
	p := parser{lookup: lookup}
	c := config{
		GRPCAddr:      p.str("GRPC_ADDR", ":50051"),
		HTTPAddr:      p.str("HTTP_ADDR", ":8090"),
		DBPath:        p.str("DB_PATH", "/data/datacore.db"),
		KafkaBrokers:  p.list("KAFKA_BROKERS", "kafka:9092"),
		TriggersTopic: p.str("TRIGGERS_TOPIC", "triggers"),
		Seed:          uint64(p.int("SEED", 42, 0, 1<<62)),
		SeedCounts: model.SeedCounts{
			Groups: p.int("SEED_GROUPS", 8, 1, model.MaxSeedGroups),
			Users:  p.int("SEED_USERS", 300, 1, model.MaxSeedUsers),
			Assets: p.int("SEED_ASSETS", 3000, 1, 3*model.MaxSeedAssetsPerPx),
		},
		Generator: &adlv1.GeneratorConfig{
			RatePerSecond: p.float("GENERATOR_RATE", 10, 0, 500),
			Paused:        p.bool("GENERATOR_PAUSED", false),
		},
		OutboxBatch: p.int("OUTBOX_BATCH", 500, 1, 10000),
		OutboxPoll:  time.Duration(p.int("OUTBOX_POLL_INTERVAL_MS", 50, 1, 60000)) * time.Millisecond,
		Chaos: chaos.FromEnv(
			p.float("CHAOS_DUPLICATE_RATE", 0, 0, 1),
			p.float("CHAOS_MALFORMED_RATE", 0, 0, 1),
			p.float("CHAOS_PHANTOM_RATE", 0, 0, 1),
			p.float("CHAOS_STALE_READ_RATE", 0, 0, 1),
			p.float("CHAOS_UNAVAILABLE_RATE", 0, 0, 1),
			int32(p.int("CHAOS_STALE_WINDOW_MS", 300, 0, chaos.MaxStaleWindowMs)),
		),
		LogLevel: p.level("LOG_LEVEL", "info"),
	}
	if err := c.SeedCounts.Validate(); err != nil {
		p.errs = append(p.errs, err)
	}
	return c, errors.Join(p.errs...)
}

// parser reads typed environment values and collects errors.
type parser struct {
	lookup func(string) (string, bool)
	errs   []error
}

func (p *parser) raw(key string) (string, bool) {
	v, ok := p.lookup(key)
	v = strings.TrimSpace(v)
	return v, ok && v != ""
}

func (p *parser) str(key, def string) string {
	if v, ok := p.raw(key); ok {
		return v
	}
	return def
}

func (p *parser) list(key, def string) []string {
	var out []string
	for _, s := range strings.Split(p.str(key, def), ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		p.errs = append(p.errs, fmt.Errorf("%s is empty", key))
	}
	return out
}

func (p *parser) int(key string, def, lo, hi int) int {
	v, ok := p.raw(key)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < lo || n > hi {
		p.errs = append(p.errs, fmt.Errorf("%s=%q: want an integer in [%d, %d]", key, v, lo, hi))
		return def
	}
	return n
}

func (p *parser) float(key string, def, lo, hi float64) float64 {
	v, ok := p.raw(key)
	if !ok {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f != f || f < lo || f > hi {
		p.errs = append(p.errs, fmt.Errorf("%s=%q: want a number in [%v, %v]", key, v, lo, hi))
		return def
	}
	return f
}

func (p *parser) bool(key string, def bool) bool {
	v, ok := p.raw(key)
	if !ok {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		p.errs = append(p.errs, fmt.Errorf("%s=%q: want true or false", key, v))
		return def
	}
	return b
}

func (p *parser) level(key, def string) slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(p.str(key, def))); err != nil {
		p.errs = append(p.errs, fmt.Errorf("%s: %w", key, err))
		return slog.LevelInfo
	}
	return l
}

// localURL turns a listen address such as ":8090" into a URL on 127.0.0.1.
func localURL(listen, path string) string {
	host, port := "127.0.0.1", listen
	if i := strings.LastIndex(listen, ":"); i >= 0 {
		port = listen[i+1:]
		if h := listen[:i]; h != "" && h != "0.0.0.0" && h != "[::]" {
			host = h
		}
	}
	return "http://" + host + ":" + port + path
}
