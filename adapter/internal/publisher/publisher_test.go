package publisher

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/twmb/franz-go/pkg/kgo"
)

func headers(r *kgo.Record) map[string]string {
	m := map[string]string{}
	for _, h := range r.Headers {
		m[h.Key] = string(h.Value)
	}
	return m
}

func TestDeadLetterHeaders(t *testing.T) {
	src := Source{Topic: "triggers", Partition: 2, Offset: 18342, Key: []byte("test:garbage:1"), Value: []byte{0xff, 0x01}}
	at := time.Date(2026, 11, 25, 10, 20, 11, 4_000_000, time.FixedZone("CET", 3600))
	r := DeadLetterRecord("dlq.adapter", src, ReasonDecode, "proto: cannot parse invalid wire-format data", at)
	if r.Topic != "dlq.adapter" || string(r.Key) != string(src.Key) || string(r.Value) != string(src.Value) {
		t.Fatalf("dead letter must keep the original key and bytes: %+v", r)
	}
	want := map[string]string{
		"error.reason":     "DECODE_ERROR",
		"error.message":    "proto: cannot parse invalid wire-format data",
		"source.topic":     "triggers",
		"source.partition": "2",
		"source.offset":    "18342",
		"failed.at":        "2026-11-25T09:20:11.004Z",
		"service":          "adapter",
	}
	got := headers(r)
	if len(got) != len(want) {
		t.Fatalf("headers %v, want exactly %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("header %s = %q, want %q", k, got[k], v)
		}
	}
}

func TestErrorMessageIsAtMost1KB(t *testing.T) {
	long := strings.Repeat("é", 1000) // 2000 bytes
	r := DeadLetterRecord("dlq.adapter", Source{}, ReasonValidation, long, time.Now())
	msg := headers(r)["error.message"]
	if len(msg) > MaxErrorMessage || !utf8.ValidString(msg) || len(msg) < MaxErrorMessage-4 {
		t.Fatalf("error.message is %d bytes (valid UTF-8 %v)", len(msg), utf8.ValidString(msg))
	}
	if Truncate("short", 1024) != "short" {
		t.Fatal("Truncate changed a short string")
	}
}

type fakeProducer struct {
	mu      sync.Mutex
	records []*kgo.Record
	fails   int // fail this many produces first
	pending []func()
	manual  bool
}

func (f *fakeProducer) Produce(_ context.Context, r *kgo.Record, promise func(*kgo.Record, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records = append(f.records, r)
	var err error
	if f.fails > 0 {
		f.fails--
		err = errors.New("broker unavailable")
	}
	if f.manual {
		f.pending = append(f.pending, func() { promise(r, err) })
		return
	}
	go promise(r, err)
}

func newPub(prod Producer, life context.Context) *Publisher {
	return New(life, prod, "entities.", "dlq.adapter", slog.New(slog.NewJSONHandler(io.Discard, nil)))
}

func TestEntityRecord(t *testing.T) {
	prod := &fakeProducer{}
	p := newPub(prod, context.Background())
	if err := p.Entity("assets", "LT-0042", []byte("v"), 7).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := prod.records[0]
	if r.Topic != "entities.assets" || string(r.Key) != "LT-0042" || headers(r)["trigger.version"] != "7" {
		t.Fatalf("entity record %+v headers %v", r, headers(r))
	}
}

func TestAckWaitsForTheBroker(t *testing.T) {
	prod := &fakeProducer{manual: true}
	p := newPub(prod, context.Background())
	ack := p.Entity("assets", "LT-0042", []byte("v"), 7)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := ack.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Ack resolved before the broker answered: %v", err)
	}
	prod.pending[0]()
	if err := ack.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// A failed produce is final and never re-sent: re-sending it later could put
// an older version after a newer one on a compacted topic (audit F-03).
func TestFailedProduceFailsTheAckWithoutResending(t *testing.T) {
	prod := &fakeProducer{fails: 1}
	p := newPub(prod, context.Background())
	if err := p.DeadLetter(Source{Topic: "triggers"}, ReasonNotFound, "x").Wait(context.Background()); err == nil {
		t.Fatal("a failed produce resolved without error")
	}
	time.Sleep(50 * time.Millisecond)
	prod.mu.Lock()
	defer prod.mu.Unlock()
	if len(prod.records) != 1 {
		t.Fatalf("%d produce attempts, want exactly 1", len(prod.records))
	}
}

func TestAckedRecordReportsSuccessAfterContextEnds(t *testing.T) {
	a := newAck()
	a.resolve(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for range 100 {
		if err := a.Wait(ctx); err != nil {
			t.Fatalf("an acknowledged record reported %v", err)
		}
	}
}

func TestFailedProduceFailsOnShutdown(t *testing.T) {
	life, stop := context.WithCancel(context.Background())
	stop()
	prod := &fakeProducer{fails: 1}
	p := newPub(prod, life)
	if err := p.Entity("assets", "LT-0042", nil, 1).Wait(context.Background()); err == nil {
		t.Fatal("a failed produce after shutdown resolved without error")
	}
	if Failed(errors.New("x")).Wait(context.Background()) == nil {
		t.Fatal("Failed ack has no error")
	}
}
