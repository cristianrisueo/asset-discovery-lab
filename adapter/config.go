package main

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// config holds every variable of spec §9.7.
type config struct {
	KafkaBrokers      []string
	TriggersTopic     string
	EntityTopicPrefix string
	DLQTopic          string
	ConsumerGroup     string
	DatacoreAddr      string
	GRPCTimeout       time.Duration
	FastRetries       int
	RetryBase         time.Duration
	RetryMax          time.Duration
	StaleMaxWait      time.Duration
	DragonflyAddr     string
	CacheTTL          time.Duration
	HTTPAddr          string
	LogLevel          slog.Level
}

func loadConfig(lookup func(string) (string, bool)) (config, error) {
	p := parser{lookup: lookup}
	ms := func(key string, def, lo, hi int) time.Duration {
		return time.Duration(p.int(key, def, lo, hi)) * time.Millisecond
	}
	c := config{
		KafkaBrokers:      p.list("KAFKA_BROKERS", "kafka:9092"),
		TriggersTopic:     p.str("TRIGGERS_TOPIC", "triggers"),
		EntityTopicPrefix: p.str("ENTITY_TOPIC_PREFIX", "entities."),
		DLQTopic:          p.str("DLQ_TOPIC", "dlq.adapter"),
		ConsumerGroup:     p.str("CONSUMER_GROUP", "adl-adapter"),
		DatacoreAddr:      p.str("DATACORE_ADDR", "datacore:50051"),
		GRPCTimeout:       ms("GRPC_TIMEOUT_MS", 500, 1, 60000),
		FastRetries:       p.int("FAST_RETRIES", 3, 0, 20),
		RetryBase:         ms("RETRY_BASE_MS", 100, 1, 60000),
		RetryMax:          ms("RETRY_MAX_MS", 5000, 1, 600000),
		StaleMaxWait:      ms("STALE_MAX_WAIT_MS", 10000, 0, 600000),
		DragonflyAddr:     p.str("DRAGONFLY_ADDR", "dragonfly:6379"),
		CacheTTL:          time.Duration(p.int("CACHE_TTL_SECONDS", 3600, 1, 7*24*3600)) * time.Second,
		HTTPAddr:          p.str("HTTP_ADDR", ":8091"),
		LogLevel:          p.level("LOG_LEVEL", "info"),
	}
	if c.RetryMax < c.RetryBase {
		p.errs = append(p.errs, fmt.Errorf("RETRY_MAX_MS (%v) is lower than RETRY_BASE_MS (%v)", c.RetryMax, c.RetryBase))
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
	for s := range strings.SplitSeq(p.str(key, def), ",") {
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

func (p *parser) level(key, def string) slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(p.str(key, def))); err != nil {
		p.errs = append(p.errs, fmt.Errorf("%s: %w", key, err))
		return slog.LevelInfo
	}
	return l
}

// localURL turns a listen address such as ":8091" into a URL on 127.0.0.1.
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
