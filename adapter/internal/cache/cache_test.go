package cache

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"
)

func TestKeyFormat(t *testing.T) {
	for _, tc := range []struct {
		typ, id string
		v       int64
		want    string
	}{
		{"asset", "LT-0042", 7, "entity:asset:LT-0042:v7"},
		{"user", "U-0117", 1, "entity:user:U-0117:v1"},
		{"group", "G-03", 12345, "entity:group:G-03:v12345"},
	} {
		if got := Key(tc.typ, tc.id, tc.v); got != tc.want {
			t.Errorf("Key(%s, %s, %d) = %q, want %q", tc.typ, tc.id, tc.v, got, tc.want)
		}
	}
}

// closedAddr returns an address where nothing listens.
func closedAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func TestErrorIsAMissAndFailsFast(t *testing.T) {
	var logs bytes.Buffer
	c := New(closedAddr(t), time.Hour, slog.New(slog.NewJSONHandler(&logs, nil)))
	defer func() { _ = c.Close() }()
	start := time.Now()
	v, hit := c.Get(context.Background(), Key("asset", "LT-0042", 7))
	if hit || v != nil {
		t.Fatalf("Get on a dead cache = %v, %v; want a miss", v, hit)
	}
	c.Set(context.Background(), Key("asset", "LT-0042", 7), []byte("x"))
	if d := time.Since(start); d > time.Second {
		t.Fatalf("two operations on a dead cache took %v; they must fail fast", d)
	}
	if !strings.Contains(logs.String(), "treated as a miss") || !strings.Contains(logs.String(), "cache set failed") {
		t.Fatalf("cache errors not logged: %s", logs.String())
	}
}

// TestTimeoutIsAMiss uses a server that accepts connections and never answers.
func TestTimeoutIsAMiss(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func() { _, _ = io.Copy(io.Discard, conn) }()
		}
	}()
	c := New(l.Addr().String(), time.Hour, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	defer func() { _ = c.Close() }()
	start := time.Now()
	if _, hit := c.Get(context.Background(), "k"); hit {
		t.Fatal("a hung cache gave a hit")
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("Get on a hung cache took %v, want about %v", d, Timeout)
	}
}
