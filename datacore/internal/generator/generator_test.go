package generator

import (
	"context"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/cristianrisueo/asset-discovery-lab/datacore/internal/model"
	"github.com/cristianrisueo/asset-discovery-lab/datacore/internal/store"
	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
)

var counts = model.SeedCounts{Groups: 8, Users: 300, Assets: 3000}

func seededStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "datacore.db"), "triggers")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if _, err := s.Seed(ctx, 42, counts); err != nil {
		t.Fatal(err)
	}
	return s
}

func newGen(t *testing.T, a Applier, cfg *adlv1.GeneratorConfig) *Generator {
	t.Helper()
	g, err := New(a, counts, cfg, rand.New(rand.NewPCG(1, 2)), slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestMixWeightsSumTo100(t *testing.T) {
	total := 0
	for _, m := range Mix {
		total += m.Weight
	}
	if total != 100 {
		t.Fatalf("weights sum to %d", total)
	}
}

func TestOnlySeededEntitiesChangeAndTheMixHolds(t *testing.T) {
	seeded := map[string]bool{}
	for _, id := range append(append(counts.AssetIDs(), counts.UserIDs()...), counts.GroupIDs()...) {
		seeded[id] = true
	}
	users := map[string]bool{"": true}
	for _, id := range counts.UserIDs() {
		users[id] = true
	}
	groups := map[string]bool{}
	for _, id := range counts.GroupIDs() {
		groups[id] = true
	}
	kinds := map[Kind]int{}
	fake := ApplyFunc(func(_ context.Context, typ model.Type, id string, mutate func(proto.Message) (proto.Message, error)) (proto.Message, error) {
		if !seeded[id] {
			t.Fatalf("generator touched non-seeded id %s", id)
		}
		var cur proto.Message
		switch typ {
		case model.TypeAsset:
			cur = &adlv1.Asset{Id: id, Kind: model.KindForID(id), Os: "Windows 11", Site: "Madrid", OwnerUserId: "U-0001"}
		case model.TypeUser:
			cur = &adlv1.User{Id: id, DisplayName: "Ana Ruiz", GroupId: "G-01"}
		default:
			cur = &adlv1.Group{Id: id, Name: "Ventas"}
		}
		next, err := mutate(cur)
		if err != nil {
			return nil, err
		}
		if proto.Equal(cur, next) {
			t.Fatalf("change on %s left the entity unchanged", id)
		}
		switch e := next.(type) {
		case *adlv1.Asset:
			if !users[e.GetOwnerUserId()] {
				t.Fatalf("asset assigned to non-seeded user %q", e.GetOwnerUserId())
			}
		case *adlv1.User:
			if !groups[e.GetGroupId()] {
				t.Fatalf("user moved to non-seeded group %q", e.GetGroupId())
			}
		}
		return next, nil
	})
	g := newGen(t, fake, &adlv1.GeneratorConfig{RatePerSecond: 10})
	const n = 20000
	for range n {
		k, err := g.ApplyOne(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		kinds[k]++
	}
	for _, m := range Mix {
		got := float64(kinds[m.Kind]) / n * 100
		if math.Abs(got-float64(m.Weight)) > 1.5 {
			t.Errorf("%s: %.2f%%, want about %d%%", m.Kind, got, m.Weight)
		}
	}
}

func TestBurstAppliesNChanges(t *testing.T) {
	s := seededStore(t)
	g := newGen(t, s, &adlv1.GeneratorConfig{RatePerSecond: 10, Paused: true})
	ctx := context.Background()
	before, _ := s.OutboxPending(ctx)
	applied, err := g.Burst(ctx, 50)
	if err != nil || applied != 50 {
		t.Fatalf("Burst = %d, %v", applied, err)
	}
	after, _ := s.OutboxPending(ctx)
	if after-before != 50 || s.ChangesApplied() != 50 {
		t.Fatalf("outbox grew by %d and %d changes counted, want 50", after-before, s.ChangesApplied())
	}
}

func TestRunHonoursRateAndPause(t *testing.T) {
	s := seededStore(t)
	g := newGen(t, s, &adlv1.GeneratorConfig{RatePerSecond: 100})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { g.Run(ctx); close(done) }()
	time.Sleep(time.Second)
	if _, err := g.SetConfig(&adlv1.GeneratorConfig{RatePerSecond: 100, Paused: true}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	n := s.ChangesApplied()
	if n < 50 || n > 150 {
		t.Errorf("%d changes in about one second at 100/s", n)
	}
	time.Sleep(300 * time.Millisecond)
	if s.ChangesApplied() != n {
		t.Errorf("changes applied while paused: %d -> %d", n, s.ChangesApplied())
	}
	cancel()
	<-done
}

func TestSetConfigValidates(t *testing.T) {
	g := newGen(t, ApplyFunc(nil), &adlv1.GeneratorConfig{RatePerSecond: 10})
	if _, err := g.SetConfig(&adlv1.GeneratorConfig{RatePerSecond: 501}); err == nil {
		t.Fatal("rate 501 accepted")
	}
	if g.Config().GetRatePerSecond() != 10 {
		t.Fatal("a rejected config changed the generator")
	}
}
