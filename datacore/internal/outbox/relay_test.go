package outbox

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/cristianrisueo/asset-discovery-lab/datacore/internal/chaos"
	"github.com/cristianrisueo/asset-discovery-lab/datacore/internal/store"
	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
)

type fakeOutbox struct {
	mu      sync.Mutex
	rows    []store.OutboxRow
	deleted []int64
}

func (f *fakeOutbox) OutboxBatch(_ context.Context, limit int) ([]store.OutboxRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := min(limit, len(f.rows))
	return append([]store.OutboxRow(nil), f.rows[:n]...), nil
}

func (f *fakeOutbox) DeleteOutboxThrough(_ context.Context, seq int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, seq)
	kept := f.rows[:0]
	for _, r := range f.rows {
		if r.Seq > seq {
			kept = append(kept, r)
		}
	}
	f.rows = kept
	return nil
}

type fakeProducer struct {
	mu       sync.Mutex
	produced []*kgo.Record
	fail     error
}

func (p *fakeProducer) Ping(context.Context) error { return nil }

func (p *fakeProducer) ProduceSync(_ context.Context, rs ...*kgo.Record) kgo.ProduceResults {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(kgo.ProduceResults, 0, len(rs))
	for _, r := range rs {
		if p.fail == nil {
			p.produced = append(p.produced, r)
		}
		out = append(out, kgo.ProduceResult{Record: r, Err: p.fail})
	}
	return out
}

func rows(n int) []store.OutboxRow {
	out := make([]store.OutboxRow, n)
	for i := range out {
		out[i] = store.OutboxRow{Seq: int64(i + 1), Topic: "triggers", Key: "asset:LT-0001", Value: []byte{byte(i)}}
	}
	return out
}

func newRelay(t *testing.T, out Outbox, prod Producer, cfg *adlv1.ChaosConfig, batch int) *Relay {
	t.Helper()
	ch, err := chaos.New(cfg, rand.New(rand.NewPCG(3, 4)))
	if err != nil {
		t.Fatal(err)
	}
	return New(out, prod, ch, slog.New(slog.NewJSONHandler(io.Discard, nil)), batch, 10*time.Millisecond)
}

func TestRelayDeletesOnlyAfterAcksAndKeepsOrder(t *testing.T) {
	out := &fakeOutbox{rows: rows(7)}
	prod := &fakeProducer{}
	r := newRelay(t, out, prod, &adlv1.ChaosConfig{}, 5)
	ctx := context.Background()

	n, err := r.Once(ctx)
	if err != nil || n != 5 {
		t.Fatalf("Once = %d, %v; want 5", n, err)
	}
	if len(out.deleted) != 1 || out.deleted[0] != 5 {
		t.Fatalf("deleted through %v, want [5]", out.deleted)
	}
	if n, _ := r.Once(ctx); n != 2 {
		t.Fatalf("second batch %d rows, want 2", n)
	}
	for i, rec := range prod.produced {
		if rec.Value[0] != byte(i) {
			t.Fatalf("record %d out of order: %v", i, rec.Value)
		}
	}
	if r.Published() != 7 {
		t.Fatalf("published %d, want 7", r.Published())
	}
}

func TestRelayFailureKeepsRows(t *testing.T) {
	out := &fakeOutbox{rows: rows(3)}
	prod := &fakeProducer{fail: errors.New("broker down")}
	r := newRelay(t, out, prod, &adlv1.ChaosConfig{}, 10)
	if _, err := r.Once(context.Background()); err == nil {
		t.Fatal("Once succeeded although every produce failed")
	}
	if len(out.deleted) != 0 || len(out.rows) != 3 {
		t.Fatalf("rows deleted after a failed produce: deleted %v, kept %d", out.deleted, len(out.rows))
	}
	if r.Published() != 0 {
		t.Fatalf("published %d after a failure", r.Published())
	}
}

func TestChaosOnlyAddsRecords(t *testing.T) {
	r := newRelay(t, &fakeOutbox{}, &fakeProducer{}, &adlv1.ChaosConfig{DuplicateRate: 1, MalformedRate: 1, PhantomRate: 1}, 10)
	in := rows(4)
	recs := r.Records(in)
	if len(recs) != 16 {
		t.Fatalf("%d records, want 4 triggers x (1 + duplicate + malformed + phantom) = 16", len(recs))
	}
	for i, row := range in {
		base := recs[i*4:]
		if string(base[0].Key) != row.Key || string(base[0].Value) != string(row.Value) {
			t.Fatalf("real trigger %d replaced: %v", i, base[0])
		}
		if string(base[1].Key) != row.Key || string(base[1].Value) != string(row.Value) {
			t.Fatalf("duplicate %d differs: %v", i, base[1])
		}
		if string(base[2].Key) != chaos.MalformedKey(row.Seq) {
			t.Fatalf("malformed key %q", base[2].Key)
		}
		if !strings.HasPrefix(string(base[3].Key), "asset:LT-8") {
			t.Fatalf("phantom key %q", base[3].Key)
		}
	}

	off := newRelay(t, &fakeOutbox{}, &fakeProducer{}, &adlv1.ChaosConfig{}, 10)
	if got := off.Records(in); len(got) != 4 {
		t.Fatalf("chaos off: %d records, want 4", len(got))
	}
}

func TestRunConnectsAndDrains(t *testing.T) {
	out := &fakeOutbox{rows: rows(12)}
	prod := &fakeProducer{}
	r := newRelay(t, out, prod, &adlv1.ChaosConfig{}, 5)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for r.Published() < 12 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if !r.Connected() || r.Published() != 12 {
		t.Fatalf("connected %v, published %d; want true and 12", r.Connected(), r.Published())
	}
}
