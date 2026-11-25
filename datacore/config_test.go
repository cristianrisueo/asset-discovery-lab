package main

import (
	"log/slog"
	"net/http"
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
	if c.GRPCAddr != ":50051" || c.HTTPAddr != ":8090" || c.DBPath != "/data/datacore.db" ||
		len(c.KafkaBrokers) != 1 || c.KafkaBrokers[0] != "kafka:9092" || c.TriggersTopic != "triggers" ||
		c.Seed != 42 || c.SeedCounts.Groups != 8 || c.SeedCounts.Users != 300 || c.SeedCounts.Assets != 3000 ||
		c.Generator.GetRatePerSecond() != 10 || c.Generator.GetPaused() || c.OutboxBatch != 500 ||
		c.OutboxPoll != 50*time.Millisecond || c.LogLevel != slog.LevelInfo {
		t.Fatalf("defaults differ from spec §7.10: %+v", c)
	}
	ch := c.Chaos
	if ch.GetDuplicateRate() != 0 || ch.GetMalformedRate() != 0 || ch.GetPhantomRate() != 0 ||
		ch.GetStaleReadRate() != 0 || ch.GetUnavailableRate() != 0 || ch.GetStaleWindowMs() != 300 {
		t.Fatalf("chaos defaults differ from spec §7.9: %v", ch)
	}
}

func TestConfigOverridesAndErrors(t *testing.T) {
	c, err := loadConfig(lookupFrom(map[string]string{
		"KAFKA_BROKERS": "a:1, b:2", "GENERATOR_PAUSED": "true", "GENERATOR_RATE": "20",
		"CHAOS_UNAVAILABLE_RATE": "0.5", "LOG_LEVEL": "debug",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.KafkaBrokers) != 2 || !c.Generator.GetPaused() || c.Generator.GetRatePerSecond() != 20 ||
		c.Chaos.GetUnavailableRate() != 0.5 || c.LogLevel != slog.LevelDebug {
		t.Fatalf("overrides not applied: %+v", c)
	}
	_, err = loadConfig(lookupFrom(map[string]string{
		"GENERATOR_RATE": "900", "CHAOS_DUPLICATE_RATE": "2", "OUTBOX_BATCH": "x", "SEED_USERS": "9500",
	}))
	if err == nil {
		t.Fatal("invalid values accepted")
	}
	for _, key := range []string{"GENERATOR_RATE", "CHAOS_DUPLICATE_RATE", "OUTBOX_BATCH", "SEED_USERS"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error does not name %s: %v", key, err)
		}
	}
}

func TestLocalURL(t *testing.T) {
	for in, want := range map[string]string{
		":8090":         "http://127.0.0.1:8090/readyz",
		"0.0.0.0:8091":  "http://127.0.0.1:8091/readyz",
		"localhost:900": "http://localhost:900/readyz",
	} {
		if got := localURL(in, "/readyz"); got != want {
			t.Errorf("localURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHealthEndpoints(t *testing.T) {
	ready := false
	srv := httptest.NewServer(healthHandler(func() (bool, string) { return ready, "seed in progress" }))
	defer srv.Close()
	if err := healthcheck(srv.URL + "/healthz"); err != nil {
		t.Fatalf("healthz: %v", err)
	}
	if err := healthcheck(srv.URL + "/readyz"); err == nil {
		t.Fatal("readyz answered 200 before ready")
	}
	ready = true
	if err := healthcheck(srv.URL + "/readyz"); err != nil {
		t.Fatalf("readyz once ready: %v", err)
	}
	resp, err := http.Post(srv.URL+"/healthz", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST /healthz: %d", resp.StatusCode)
	}
}
