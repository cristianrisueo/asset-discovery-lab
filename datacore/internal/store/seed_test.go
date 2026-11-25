package store

import (
	"context"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/cristianrisueo/asset-discovery-lab/datacore/internal/model"
	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
)

var defaultCounts = model.SeedCounts{Groups: 8, Users: 300, Assets: 3000}

func TestDatasetIsDeterministic(t *testing.T) {
	a, b := NewDataset(42, defaultCounts), NewDataset(42, defaultCounts)
	if len(a.Groups) != 8 || len(a.Users) != 300 || len(a.Assets) != 3000 {
		t.Fatalf("sizes %d/%d/%d, want 8/300/3000", len(a.Groups), len(a.Users), len(a.Assets))
	}
	for i := range a.Assets {
		if !proto.Equal(a.Assets[i], b.Assets[i]) {
			t.Fatalf("asset %d differs: %v vs %v", i, a.Assets[i], b.Assets[i])
		}
	}
	for i := range a.Users {
		if !proto.Equal(a.Users[i], b.Users[i]) {
			t.Fatalf("user %d differs: %v vs %v", i, a.Users[i], b.Users[i])
		}
	}
	other := NewDataset(43, defaultCounts)
	same := 0
	for i := range a.Users {
		if a.Users[i].GetDisplayName() == other.Users[i].GetDisplayName() {
			same++
		}
	}
	if same == len(a.Users) {
		t.Fatal("a different seed value gives the same names")
	}
}

func TestDatasetFollowsSpec(t *testing.T) {
	d := NewDataset(42, defaultCounts)
	groups := map[string]*adlv1.Group{}
	for _, g := range d.Groups {
		groups[g.GetId()] = g
	}
	users := map[string]*adlv1.User{}
	for _, u := range d.Users {
		users[u.GetId()] = u
		if u.GetEmail() != "u-"+u.GetId()[2:]+"@acme.example" {
			t.Fatalf("user %s email %q", u.GetId(), u.GetEmail())
		}
		if groups[u.GetGroupId()] == nil {
			t.Fatalf("user %s has unknown group %q", u.GetId(), u.GetGroupId())
		}
	}
	assigned, clients := 0, 0
	for _, a := range d.Assets {
		if a.GetKind() != model.KindForID(a.GetId()) {
			t.Fatalf("asset %s kind %s", a.GetId(), a.GetKind())
		}
		if a.GetHostname() == "" || a.GetOs() == "" || a.GetSite() == "" {
			t.Fatalf("asset %s has empty fields: %v", a.GetId(), a)
		}
		if a.GetKind() == adlv1.AssetKind_ASSET_KIND_SERVER {
			if a.GetOwnerUserId() != "" {
				t.Fatalf("server %s is assigned", a.GetId())
			}
			continue
		}
		clients++
		if o := a.GetOwnerUserId(); o != "" {
			assigned++
			if users[o] == nil {
				t.Fatalf("asset %s owned by unknown user %s", a.GetId(), o)
			}
		}
	}
	if pct := float64(assigned) / float64(clients) * 100; pct < 87 || pct > 93 {
		t.Fatalf("%.1f%% of laptops and desktops assigned, want about 90%%", pct)
	}

	// The map's example (spec §5.3).
	var lt42 *adlv1.Asset
	for _, a := range d.Assets {
		if a.GetId() == "LT-0042" {
			lt42 = a
		}
	}
	if lt42 == nil || lt42.GetOs() != "Windows 11" || lt42.GetSite() != "Alicante" ||
		lt42.GetKind() != adlv1.AssetKind_ASSET_KIND_LAPTOP || lt42.GetOwnerUserId() != "U-0117" ||
		lt42.GetHostname() != "lt-0042.acme.local" {
		t.Fatalf("LT-0042 = %v", lt42)
	}
	if u := users["U-0117"]; u.GetDisplayName() != "Ana Ruiz" || u.GetGroupId() != "G-03" {
		t.Fatalf("U-0117 = %v", u)
	}
	if g := groups["G-03"]; g.GetName() != "Ventas" {
		t.Fatalf("G-03 = %v", g)
	}
}

func TestSeedWritesOnceWithOneTriggerPerEntity(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	inserted, err := s.Seed(ctx, 42, defaultCounts)
	if err != nil || !inserted {
		t.Fatalf("Seed = %v, %v", inserted, err)
	}
	a, u, g, err := s.Counts(ctx)
	if err != nil || a != 3000 || u != 300 || g != 8 {
		t.Fatalf("counts %d/%d/%d (%v), want 3000/300/8", a, u, g, err)
	}
	if n, _ := s.OutboxPending(ctx); n != 3308 {
		t.Fatalf("outbox %d rows, want 3308", n)
	}
	if s.ChangesApplied() != 0 {
		t.Fatalf("the seed counted %d changes, want 0", s.ChangesApplied())
	}
	snap, err := s.Get(ctx, model.TypeAsset, "LT-0042")
	if err != nil {
		t.Fatal(err)
	}
	if v := snap.Current.(*adlv1.Asset).GetVersion(); v != 1 {
		t.Fatalf("seeded version %d, want 1", v)
	}
	// A second start does not seed again.
	inserted, err = s.Seed(ctx, 42, defaultCounts)
	if err != nil || inserted {
		t.Fatalf("second Seed = %v, %v; want false", inserted, err)
	}
	if n, _ := s.OutboxPending(ctx); n != 3308 {
		t.Fatalf("outbox %d rows after a second start, want 3308", n)
	}
}

func TestSeedIntoTwoDatabasesIsIdentical(t *testing.T) {
	ctx := context.Background()
	s1, s2 := openTemp(t), openTemp(t)
	small := model.SeedCounts{Groups: 4, Users: 50, Assets: 200}
	for _, s := range []*Store{s1, s2} {
		if _, err := s.Seed(ctx, 42, small); err != nil {
			t.Fatal(err)
		}
	}
	for _, typ := range []model.Type{model.TypeAsset, model.TypeUser, model.TypeGroup} {
		l1, _ := s1.List(ctx, typ)
		l2, _ := s2.List(ctx, typ)
		if len(l1) != len(l2) || len(l1) == 0 {
			t.Fatalf("%s: %d vs %d entities", typ, len(l1), len(l2))
		}
		for i := range l1 {
			strip := func(m proto.Message) proto.Message {
				c := proto.Clone(m)
				stamp(c, 1, nil)
				return c
			}
			if !proto.Equal(strip(l1[i]), strip(l2[i])) {
				t.Fatalf("%s %d differs: %v vs %v", typ, i, l1[i], l2[i])
			}
		}
	}
}
