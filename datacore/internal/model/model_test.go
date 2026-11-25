package model

import (
	"errors"
	"math"
	"math/rand/v2"
	"testing"

	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
)

func validAsset() *adlv1.UpsertAssetRequest {
	return &adlv1.UpsertAssetRequest{
		Id: "LT-9001", Hostname: "lt-9001.acme.local", Kind: adlv1.AssetKind_ASSET_KIND_LAPTOP,
		Os: "Windows 11", Site: "Madrid", OwnerUserId: "U-0117",
	}
}

func TestValidateAssetRequest(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(r *adlv1.UpsertAssetRequest)
		ok     bool
	}{
		{"valid", func(*adlv1.UpsertAssetRequest) {}, true},
		{"valid unassigned", func(r *adlv1.UpsertAssetRequest) { r.OwnerUserId = "" }, true},
		{"valid server", func(r *adlv1.UpsertAssetRequest) { r.Id, r.Kind = "SV-9001", adlv1.AssetKind_ASSET_KIND_SERVER }, true},
		{"valid desktop", func(r *adlv1.UpsertAssetRequest) { r.Id, r.Kind = "DT-0001", adlv1.AssetKind_ASSET_KIND_DESKTOP }, true},
		{"bad id", func(r *adlv1.UpsertAssetRequest) { r.Id = "XX-0001" }, false},
		{"short id", func(r *adlv1.UpsertAssetRequest) { r.Id = "LT-001" }, false},
		{"injection id", func(r *adlv1.UpsertAssetRequest) { r.Id = "LT-0042' OR '1'='1" }, false},
		{"kind unspecified", func(r *adlv1.UpsertAssetRequest) { r.Kind = adlv1.AssetKind_ASSET_KIND_UNSPECIFIED }, false},
		{"kind mismatches prefix", func(r *adlv1.UpsertAssetRequest) { r.Kind = adlv1.AssetKind_ASSET_KIND_SERVER }, false},
		{"empty hostname", func(r *adlv1.UpsertAssetRequest) { r.Hostname = " " }, false},
		{"empty os", func(r *adlv1.UpsertAssetRequest) { r.Os = "" }, false},
		{"empty site", func(r *adlv1.UpsertAssetRequest) { r.Site = "" }, false},
		{"bad owner", func(r *adlv1.UpsertAssetRequest) { r.OwnerUserId = "U-17" }, false},
		{"not-found id", func(r *adlv1.UpsertAssetRequest) { r.Id = "LT-9999" }, false},
		{"phantom id", func(r *adlv1.UpsertAssetRequest) { r.Id = "LT-8001" }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := validAsset()
			tt.mutate(r)
			err := ValidateAssetRequest(r)
			if tt.ok != (err == nil) {
				t.Fatalf("ValidateAssetRequest(%v) = %v, want ok=%v", r, err, tt.ok)
			}
			if err != nil && !errors.Is(err, ErrInvalid) {
				t.Fatalf("error %v does not wrap ErrInvalid", err)
			}
		})
	}
}

func TestValidateUserAndGroupRequests(t *testing.T) {
	users := []struct {
		r  *adlv1.UpsertUserRequest
		ok bool
	}{
		{&adlv1.UpsertUserRequest{Id: "U-9001", DisplayName: "A", Email: "a@x", GroupId: "G-90"}, true},
		{&adlv1.UpsertUserRequest{Id: "U-9001", DisplayName: "A", Email: "a@x"}, true},
		{&adlv1.UpsertUserRequest{Id: "U-901", DisplayName: "A", Email: "a@x"}, false},
		{&adlv1.UpsertUserRequest{Id: "U-9001", Email: "a@x"}, false},
		{&adlv1.UpsertUserRequest{Id: "U-9001", DisplayName: "A"}, false},
		{&adlv1.UpsertUserRequest{Id: "U-9001", DisplayName: "A", Email: "a@x", GroupId: "G-1"}, false},
	}
	for _, u := range users {
		if err := ValidateUserRequest(u.r); u.ok != (err == nil) {
			t.Errorf("ValidateUserRequest(%v) = %v, want ok=%v", u.r, err, u.ok)
		}
	}
	groups := []struct {
		r  *adlv1.UpsertGroupRequest
		ok bool
	}{
		{&adlv1.UpsertGroupRequest{Id: "G-90", Name: "QA"}, true},
		{&adlv1.UpsertGroupRequest{Id: "G-9", Name: "QA"}, false},
		{&adlv1.UpsertGroupRequest{Id: "G-90"}, false},
	}
	for _, g := range groups {
		if err := ValidateGroupRequest(g.r); g.ok != (err == nil) {
			t.Errorf("ValidateGroupRequest(%v) = %v, want ok=%v", g.r, err, g.ok)
		}
	}
}

func TestNeverCreated(t *testing.T) {
	for id, want := range map[string]bool{
		"LT-9999": true, "LT-8000": true, "LT-8999": true, "LT-8500": true,
		"LT-7999": false, "LT-9000": false, "LT-9998": false, "DT-8001": false, "SV-9999": false,
	} {
		if got := NeverCreated(id); got != want {
			t.Errorf("NeverCreated(%s) = %v, want %v", id, got, want)
		}
	}
}

func TestGeneratorAndBurstRanges(t *testing.T) {
	for _, tc := range []struct {
		rate float64
		ok   bool
	}{{0, true}, {10, true}, {500, true}, {-1, false}, {500.1, false}, {math.NaN(), false}} {
		err := ValidateGeneratorConfig(&adlv1.GeneratorConfig{RatePerSecond: tc.rate})
		if tc.ok != (err == nil) {
			t.Errorf("rate %v: %v, want ok=%v", tc.rate, err, tc.ok)
		}
	}
	if ValidateGeneratorConfig(nil) == nil {
		t.Error("nil generator config accepted")
	}
	for n, ok := range map[int32]bool{0: false, 1: true, 10000: true, 10001: false, -5: false} {
		if err := ValidateBurst(n); ok != (err == nil) {
			t.Errorf("burst %d: %v, want ok=%v", n, err, ok)
		}
	}
}

func TestSeedCounts(t *testing.T) {
	c := SeedCounts{Groups: 8, Users: 300, Assets: 3000}
	lt, dt, sv := c.Split()
	if lt != 1800 || dt != 900 || sv != 300 {
		t.Fatalf("split = %d/%d/%d, want 1800/900/300", lt, dt, sv)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	a := c.AssetIDs()
	if len(a) != 3000 || a[0] != "LT-0001" || a[1799] != "LT-1800" || a[1800] != "DT-0001" || a[2999] != "SV-0300" {
		t.Fatalf("asset ids wrong: %d, %s %s %s %s", len(a), a[0], a[1799], a[1800], a[2999])
	}
	if g := c.GroupIDs(); g[7] != "G-08" {
		t.Fatalf("group ids wrong: %v", g)
	}
	if (SeedCounts{Groups: 90, Users: 1, Assets: 1}).Validate() == nil {
		t.Error("90 groups accepted, G-90 is reserved")
	}
	if (SeedCounts{Groups: 1, Users: 9000, Assets: 1}).Validate() == nil {
		t.Error("9000 users accepted, U-9000 is reserved")
	}
	if (SeedCounts{Groups: 1, Users: 1, Assets: 14000}).Validate() == nil {
		t.Error("14000 assets accepted, LT-8000 is phantom")
	}
}

func TestPickWeighted(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	counts := map[string]int{}
	const n = 100000
	for range n {
		counts[PickWeighted(r, ClientOS)]++
	}
	for _, v := range ClientOS {
		got := float64(counts[v.Value]) / n * 100
		if math.Abs(got-float64(v.Weight)) > 1 {
			t.Errorf("%s: %.2f%%, want about %d%%", v.Value, got, v.Weight)
		}
	}
}
