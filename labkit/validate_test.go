package labkit

import (
	"errors"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
)

func validAsset() *adlv1.Asset {
	return &adlv1.Asset{
		Id: "LT-0042", Hostname: "lt-0042.acme.local", Kind: adlv1.AssetKind_ASSET_KIND_LAPTOP,
		Os: "Windows 11", Site: "Madrid", OwnerUserId: "U-0117", Version: 7, ChangedAt: timestamppb.Now(),
	}
}

func TestValidateTrigger(t *testing.T) {
	ok := &adlv1.Trigger{EntityType: adlv1.EntityType_ENTITY_TYPE_ASSET, EntityId: "LT-0042", Version: 7, ChangedAt: timestamppb.Now()}
	cases := []struct {
		name  string
		key   string
		mod   func(*adlv1.Trigger)
		valid bool
	}{
		{"valid", "asset:LT-0042", func(*adlv1.Trigger) {}, true},
		{"unspecified type", "asset:LT-0042", func(t *adlv1.Trigger) { t.EntityType = adlv1.EntityType_ENTITY_TYPE_UNSPECIFIED }, false},
		{"bad id for type", "user:LT-0042", func(t *adlv1.Trigger) { t.EntityType = adlv1.EntityType_ENTITY_TYPE_USER }, false},
		{"version zero", "asset:LT-0042", func(t *adlv1.Trigger) { t.Version = 0 }, false},
		{"no changed_at", "asset:LT-0042", func(t *adlv1.Trigger) { t.ChangedAt = nil }, false},
		{"key mismatch", "asset:LT-0043", func(*adlv1.Trigger) {}, false},
		{"valid user", "user:U-0117", func(t *adlv1.Trigger) {
			t.EntityType, t.EntityId = adlv1.EntityType_ENTITY_TYPE_USER, "U-0117"
		}, true},
		{"valid group", "group:G-03", func(t *adlv1.Trigger) {
			t.EntityType, t.EntityId = adlv1.EntityType_ENTITY_TYPE_GROUP, "G-03"
		}, true},
	}
	for _, c := range cases {
		tr := proto.Clone(ok).(*adlv1.Trigger)
		c.mod(tr)
		err := ValidateTrigger(c.key, tr)
		if (err == nil) != c.valid {
			t.Errorf("%s: err = %v, want valid=%v", c.name, err, c.valid)
		}
		if err != nil && !errors.Is(err, ErrValidation) {
			t.Errorf("%s: error does not wrap ErrValidation", c.name)
		}
	}
}

func TestValidateAsset(t *testing.T) {
	cases := []struct {
		name  string
		mod   func(*adlv1.Asset)
		valid bool
	}{
		{"valid", func(*adlv1.Asset) {}, true},
		{"unassigned", func(a *adlv1.Asset) { a.OwnerUserId = "" }, true},
		{"bad id", func(a *adlv1.Asset) { a.Id = "XX-0042" }, false},
		{"empty hostname", func(a *adlv1.Asset) { a.Hostname = "" }, false},
		{"empty os", func(a *adlv1.Asset) { a.Os = "" }, false},
		{"empty site", func(a *adlv1.Asset) { a.Site = "" }, false},
		{"unspecified kind", func(a *adlv1.Asset) { a.Kind = adlv1.AssetKind_ASSET_KIND_UNSPECIFIED }, false},
		{"kind vs prefix", func(a *adlv1.Asset) { a.Kind = adlv1.AssetKind_ASSET_KIND_SERVER }, false},
		{"bad owner", func(a *adlv1.Asset) { a.OwnerUserId = "U-17" }, false},
		{"version zero", func(a *adlv1.Asset) { a.Version = 0 }, false},
		{"no changed_at", func(a *adlv1.Asset) { a.ChangedAt = nil }, false},
		{"server", func(a *adlv1.Asset) { a.Id, a.Kind = "SV-0001", adlv1.AssetKind_ASSET_KIND_SERVER }, true},
	}
	for _, c := range cases {
		a := validAsset()
		c.mod(a)
		if err := ValidateAsset(a); (err == nil) != c.valid {
			t.Errorf("%s: err = %v, want valid=%v", c.name, err, c.valid)
		}
	}
}

func TestValidateUserAndGroup(t *testing.T) {
	u := &adlv1.User{Id: "U-0117", DisplayName: "Ana Ruiz", Email: "u-0117@acme.example", GroupId: "G-03", Version: 1, ChangedAt: timestamppb.Now()}
	if err := ValidateUser(u); err != nil {
		t.Fatal(err)
	}
	for name, mod := range map[string]func(*adlv1.User){
		"bad id": func(u *adlv1.User) { u.Id = "U-1" }, "no name": func(u *adlv1.User) { u.DisplayName = "" },
		"no email": func(u *adlv1.User) { u.Email = "" }, "bad group": func(u *adlv1.User) { u.GroupId = "G-3" },
		"version": func(u *adlv1.User) { u.Version = 0 }, "time": func(u *adlv1.User) { u.ChangedAt = nil },
	} {
		c := proto.Clone(u).(*adlv1.User)
		mod(c)
		if ValidateUser(c) == nil {
			t.Errorf("user %s: expected invalid", name)
		}
	}
	g := &adlv1.Group{Id: "G-03", Name: "Ventas", Version: 1, ChangedAt: timestamppb.Now()}
	if err := ValidateGroup(g); err != nil {
		t.Fatal(err)
	}
	for name, mod := range map[string]func(*adlv1.Group){
		"bad id": func(g *adlv1.Group) { g.Id = "G-003" }, "no name": func(g *adlv1.Group) { g.Name = "" },
		"version": func(g *adlv1.Group) { g.Version = -1 }, "time": func(g *adlv1.Group) { g.ChangedAt = nil },
	} {
		c := proto.Clone(g).(*adlv1.Group)
		mod(c)
		if ValidateGroup(c) == nil {
			t.Errorf("group %s: expected invalid", name)
		}
	}
}

func TestDecodeEntity(t *testing.T) {
	b, _ := proto.Marshal(validAsset())
	e, err := DecodeEntity(TopicEntityAssets, []byte("LT-0042"), b)
	if err != nil || e.ID != "LT-0042" || e.Version != 7 || e.Type != "asset" || e.Asset() == nil {
		t.Fatalf("valid record: %+v, %v", e, err)
	}
	if _, err := DecodeEntity(TopicEntityAssets, []byte("LT-0043"), b); !errors.Is(err, ErrValidation) {
		t.Errorf("key mismatch: want ErrValidation, got %v", err)
	}
	if _, err := DecodeEntity(TopicEntityAssets, []byte("LT-0042"), []byte{0xff, 0xff, 0xff}); !errors.Is(err, ErrDecode) {
		t.Errorf("garbage: want ErrDecode, got %v", err)
	}
	// An asset decoded from the users topic does not validate as a user.
	if _, err := DecodeEntity(TopicEntityUsers, []byte("LT-0042"), b); err == nil {
		t.Error("asset bytes on the users topic must not validate")
	}
	if _, err := DecodeEntity("triggers", nil, b); err == nil {
		t.Error("non-entity topic must fail")
	}
}

func TestDecodeTrigger(t *testing.T) {
	tr := &adlv1.Trigger{EntityType: adlv1.EntityType_ENTITY_TYPE_GROUP, EntityId: "G-03", Version: 2, ChangedAt: timestamppb.Now()}
	b, _ := proto.Marshal(tr)
	got, err := DecodeTrigger([]byte("group:G-03"), b)
	if err != nil || !proto.Equal(got, tr) {
		t.Fatalf("got %v, %v", got, err)
	}
	if _, err := DecodeTrigger([]byte("x"), []byte{0x0a}); !errors.Is(err, ErrDecode) {
		t.Errorf("truncated bytes: want ErrDecode, got %v", err)
	}
}

func TestNamesAndKeys(t *testing.T) {
	if KindName(adlv1.AssetKind_ASSET_KIND_DESKTOP) != "DESKTOP" {
		t.Error("KindName")
	}
	if TriggerKey(adlv1.EntityType_ENTITY_TYPE_ASSET, "LT-0042") != "asset:LT-0042" {
		t.Error("TriggerKey")
	}
	if CacheKey("asset", "LT-0042", 7) != "entity:asset:LT-0042:v7" {
		t.Error("CacheKey")
	}
	if AssetKindForID("SV-0001") != adlv1.AssetKind_ASSET_KIND_SERVER || AssetKindForID("X") != adlv1.AssetKind_ASSET_KIND_UNSPECIFIED {
		t.Error("AssetKindForID")
	}
	req := NewAssetRequest("LT-9001", "Madrid", "")
	if req.GetHostname() != "lt-9001.acme.local" || req.GetKind() != adlv1.AssetKind_ASSET_KIND_LAPTOP || req.GetOs() == "" {
		t.Errorf("NewAssetRequest: %v", req)
	}
	if NewUserRequest("U-9001", "Test", "G-90").GetEmail() != "u-9001@acme.example" {
		t.Error("NewUserRequest email")
	}
}
