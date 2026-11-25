package main

import (
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func lookupFrom(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestConfigDefaults(t *testing.T) {
	c, err := loadConfig(lookupFrom(nil))
	if err != nil {
		t.Fatal(err)
	}
	want := config{
		KafkaBrokers: []string{"kafka:9092"}, TriggersTopic: "triggers", EntityTopicPrefix: "entities.",
		DLQTopic: "dlq.adapter", ConsumerGroup: "adl-adapter", DatacoreAddr: "datacore:50051",
		GRPCTimeout: 500 * time.Millisecond, FastRetries: 3, RetryBase: 100 * time.Millisecond,
		RetryMax: 5 * time.Second, StaleMaxWait: 10 * time.Second, DragonflyAddr: "dragonfly:6379",
		CacheTTL: time.Hour, HTTPAddr: ":8091", LogLevel: slog.LevelInfo,
	}
	if strings.Join(c.KafkaBrokers, ",") != "kafka:9092" {
		t.Fatalf("brokers %v", c.KafkaBrokers)
	}
	c.KafkaBrokers = want.KafkaBrokers
	if c.TriggersTopic != want.TriggersTopic || c.EntityTopicPrefix != want.EntityTopicPrefix || c.DLQTopic != want.DLQTopic ||
		c.ConsumerGroup != want.ConsumerGroup || c.DatacoreAddr != want.DatacoreAddr || c.GRPCTimeout != want.GRPCTimeout ||
		c.FastRetries != want.FastRetries || c.RetryBase != want.RetryBase || c.RetryMax != want.RetryMax ||
		c.StaleMaxWait != want.StaleMaxWait || c.DragonflyAddr != want.DragonflyAddr || c.CacheTTL != want.CacheTTL ||
		c.HTTPAddr != want.HTTPAddr || c.LogLevel != want.LogLevel {
		t.Fatalf("defaults differ from spec §9.7:\n got %+v\nwant %+v", c, want)
	}
}

func TestConfigErrors(t *testing.T) {
	_, err := loadConfig(lookupFrom(map[string]string{"GRPC_TIMEOUT_MS": "abc", "RETRY_BASE_MS": "6000", "LOG_LEVEL": "loud"}))
	if err == nil {
		t.Fatal("invalid configuration accepted")
	}
	for _, s := range []string{"GRPC_TIMEOUT_MS", "RETRY_MAX_MS", "LOG_LEVEL"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("error does not mention %s: %v", s, err)
		}
	}
}

func TestHealth(t *testing.T) {
	ready, why := false, "consumer has not joined"
	srv := httptest.NewServer(healthHandler(func() (bool, string) { return ready, why }))
	defer srv.Close()
	if err := healthcheck(srv.URL + "/healthz"); err != nil {
		t.Fatalf("healthz must answer 200 even when not ready: %v", err)
	}
	if err := healthcheck(srv.URL + "/readyz"); err == nil {
		t.Fatal("readyz answered 200 while not ready")
	}
	ready = true
	if err := healthcheck(srv.URL + "/readyz"); err != nil {
		t.Fatal(err)
	}
	if got := localURL(":8091", "/healthz"); got != "http://127.0.0.1:8091/healthz" {
		t.Fatalf("localURL = %s", got)
	}
}
