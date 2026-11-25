package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/cristianrisueo/asset-discovery-lab/datacore/internal/model"
	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "datacore.db"), "triggers")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func setSite(site string) Mutator {
	return func(cur proto.Message) (proto.Message, error) {
		a := &adlv1.Asset{Id: "LT-9001", Hostname: "lt-9001.acme.local", Kind: adlv1.AssetKind_ASSET_KIND_LAPTOP, Os: "Windows 11"}
		if cur != nil {
			a = proto.Clone(cur).(*adlv1.Asset)
		}
		a.Site = site
		return a, nil
	}
}

func outboxTriggers(t *testing.T, s *Store) []*adlv1.Trigger {
	t.Helper()
	rows, err := s.OutboxBatch(context.Background(), 1000)
	if err != nil {
		t.Fatal(err)
	}
	var out []*adlv1.Trigger
	for _, r := range rows {
		tr := &adlv1.Trigger{}
		if err := proto.Unmarshal(r.Value, tr); err != nil {
			t.Fatal(err)
		}
		if r.Topic != "triggers" {
			t.Fatalf("outbox topic %q, want triggers", r.Topic)
		}
		if want := model.TriggerKey(model.TypeAsset, tr.GetEntityId()); r.Key != want && tr.GetEntityType() == adlv1.EntityType_ENTITY_TYPE_ASSET {
			t.Fatalf("outbox key %q, want %q", r.Key, want)
		}
		out = append(out, tr)
	}
	return out
}

func TestApplyCreatesAtVersionOneWithoutPrevious(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	m, err := s.Apply(ctx, model.TypeAsset, "LT-9001", setSite("Madrid"))
	if err != nil {
		t.Fatal(err)
	}
	a := m.(*adlv1.Asset)
	if a.GetVersion() != 1 {
		t.Fatalf("version %d, want 1", a.GetVersion())
	}
	snap, err := s.Get(ctx, model.TypeAsset, "LT-9001")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Previous != nil {
		t.Fatalf("a created entity has a previous state: %v", snap.Previous)
	}
	if !proto.Equal(snap.Current, a) {
		t.Fatalf("stored %v, returned %v", snap.Current, a)
	}
}

func TestApplyRaisesVersionByOneAndWritesOutboxInSameTransaction(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	first, err := s.Apply(ctx, model.TypeAsset, "LT-9001", setSite("Alicante"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Apply(ctx, model.TypeAsset, "LT-9001", setSite("Madrid"))
	if err != nil {
		t.Fatal(err)
	}
	a1, a2 := first.(*adlv1.Asset), second.(*adlv1.Asset)
	if a2.GetVersion() != a1.GetVersion()+1 {
		t.Fatalf("version went from %d to %d, want +1", a1.GetVersion(), a2.GetVersion())
	}
	if ms := a2.GetChangedAt().AsTime().Nanosecond() % int(time.Millisecond); ms != 0 {
		t.Fatalf("changed_at %v has sub-millisecond precision", a2.GetChangedAt().AsTime())
	}

	trigs := outboxTriggers(t, s)
	if len(trigs) != 2 {
		t.Fatalf("%d outbox rows, want 2", len(trigs))
	}
	last := trigs[1]
	if last.GetEntityType() != adlv1.EntityType_ENTITY_TYPE_ASSET || last.GetEntityId() != "LT-9001" ||
		last.GetVersion() != a2.GetVersion() || !proto.Equal(last.GetChangedAt(), a2.GetChangedAt()) {
		t.Fatalf("trigger %v does not match the change %v", last, a2)
	}

	snap, err := s.Get(ctx, model.TypeAsset, "LT-9001")
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(snap.Previous, a1) {
		t.Fatalf("previous state %v, want %v", snap.Previous, a1)
	}
	if s.ChangesApplied() != 2 {
		t.Fatalf("changes applied %d, want 2", s.ChangesApplied())
	}
}

func TestApplyFailureLeavesNeitherChangeNorOutboxRow(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if _, err := s.Apply(ctx, model.TypeAsset, "LT-9001", setSite("Alicante")); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	s.beforeCommit = func() error { return boom }
	if _, err := s.Apply(ctx, model.TypeAsset, "LT-9001", setSite("Madrid")); !errors.Is(err, boom) {
		t.Fatalf("Apply error %v, want boom", err)
	}
	s.beforeCommit = nil

	snap, err := s.Get(ctx, model.TypeAsset, "LT-9001")
	if err != nil {
		t.Fatal(err)
	}
	if a := snap.Current.(*adlv1.Asset); a.GetVersion() != 1 || a.GetSite() != "Alicante" {
		t.Fatalf("failed change was applied: %v", a)
	}
	if n, _ := s.OutboxPending(ctx); n != 1 {
		t.Fatalf("outbox has %d rows, want 1", n)
	}
	// A mutator error aborts the change too.
	if _, err := s.Apply(ctx, model.TypeAsset, "LT-9001", func(proto.Message) (proto.Message, error) { return nil, boom }); !errors.Is(err, boom) {
		t.Fatalf("mutator error %v, want boom", err)
	}
	if n, _ := s.OutboxPending(ctx); n != 1 {
		t.Fatalf("outbox has %d rows after a mutator error, want 1", n)
	}
}

func TestOutboxDeleteThroughAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "datacore.db")
	ctx := context.Background()
	s, err := Open(ctx, path, "triggers")
	if err != nil {
		t.Fatal(err)
	}
	for _, site := range []string{"Alicante", "Madrid", "Sevilla"} {
		if _, err := s.Apply(ctx, model.TypeAsset, "LT-9001", setSite(site)); err != nil {
			t.Fatal(err)
		}
	}
	rows, _ := s.OutboxBatch(ctx, 2)
	if len(rows) != 2 || rows[0].Seq >= rows[1].Seq {
		t.Fatalf("batch not in seq order: %+v", rows)
	}
	if err := s.DeleteOutboxThrough(ctx, rows[1].Seq); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.OutboxPending(ctx); n != 1 {
		t.Fatalf("pending %d, want 1", n)
	}
	_ = s.Close()

	// Versions survive a restart (spec §7.2).
	s, err = Open(ctx, path, "triggers")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	m, err := s.Apply(ctx, model.TypeAsset, "LT-9001", setSite("Bilbao"))
	if err != nil {
		t.Fatal(err)
	}
	if v := m.(*adlv1.Asset).GetVersion(); v != 4 {
		t.Fatalf("version after reopen %d, want 4", v)
	}
}

func TestGetNotFoundAndExists(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if _, err := s.Get(ctx, model.TypeAsset, "LT-9999"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get unknown: %v, want ErrNotFound", err)
	}
	if ok, err := s.Exists(ctx, model.TypeUser, "U-0001"); err != nil || ok {
		t.Fatalf("Exists = %v, %v; want false", ok, err)
	}
}
