package contract

import (
	"errors"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
)

func TestValidateTrigger(t *testing.T) {
	now := timestamppb.Now()
	trig := func(et adlv1.EntityType, id string, v int64) *adlv1.Trigger {
		return &adlv1.Trigger{EntityType: et, EntityId: id, Version: v, ChangedAt: now}
	}
	tests := []struct {
		name string
		key  string
		t    *adlv1.Trigger
		ok   bool
	}{
		{"asset", "asset:LT-0042", trig(adlv1.EntityType_ENTITY_TYPE_ASSET, "LT-0042", 7), true},
		{"desktop", "asset:DT-0001", trig(adlv1.EntityType_ENTITY_TYPE_ASSET, "DT-0001", 1), true},
		{"user", "user:U-0117", trig(adlv1.EntityType_ENTITY_TYPE_USER, "U-0117", 1), true},
		{"group", "group:G-03", trig(adlv1.EntityType_ENTITY_TYPE_GROUP, "G-03", 2), true},
		{"unspecified type", "asset:LT-0042", trig(adlv1.EntityType_ENTITY_TYPE_UNSPECIFIED, "LT-0042", 1), false},
		{"unknown enum", "asset:LT-0042", trig(adlv1.EntityType(99), "LT-0042", 1), false},
		{"id breaks asset pattern", "asset:U-0117", trig(adlv1.EntityType_ENTITY_TYPE_ASSET, "U-0117", 1), false},
		{"id breaks user pattern", "user:U-117", trig(adlv1.EntityType_ENTITY_TYPE_USER, "U-117", 1), false},
		{"id breaks group pattern", "group:G-3", trig(adlv1.EntityType_ENTITY_TYPE_GROUP, "G-3", 1), false},
		{"version zero", "asset:LT-0042", trig(adlv1.EntityType_ENTITY_TYPE_ASSET, "LT-0042", 0), false},
		{"negative version", "asset:LT-0042", trig(adlv1.EntityType_ENTITY_TYPE_ASSET, "LT-0042", -1), false},
		{"no changed_at", "asset:LT-0042", &adlv1.Trigger{EntityType: adlv1.EntityType_ENTITY_TYPE_ASSET, EntityId: "LT-0042", Version: 1}, false},
		{"key names another id", "asset:LT-9005", trig(adlv1.EntityType_ENTITY_TYPE_ASSET, "LT-9006", 1), false},
		{"key names another type", "user:LT-0042", trig(adlv1.EntityType_ENTITY_TYPE_ASSET, "LT-0042", 1), false},
		{"empty key", "", trig(adlv1.EntityType_ENTITY_TYPE_ASSET, "LT-0042", 1), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTrigger(tt.key, tt.t)
			if tt.ok != (err == nil) {
				t.Fatalf("ValidateTrigger = %v, want ok=%v", err, tt.ok)
			}
			if err != nil && !errors.Is(err, ErrValidation) {
				t.Fatalf("%v does not wrap ErrValidation", err)
			}
		})
	}
}

func TestValidateEntity(t *testing.T) {
	now := timestamppb.Now()
	good := &adlv1.Asset{Id: "LT-0042", Hostname: "h", Kind: adlv1.AssetKind_ASSET_KIND_LAPTOP, Os: "o", Site: "s", OwnerUserId: "U-0117", Version: 7, ChangedAt: now}
	with := func(f func(a *adlv1.Asset)) *adlv1.Asset { a := proto.Clone(good).(*adlv1.Asset); f(a); return a }
	tests := []struct {
		name string
		id   string
		m    proto.Message
		ok   bool
	}{
		{"asset", "LT-0042", good, true},
		{"unassigned asset", "LT-0042", with(func(a *adlv1.Asset) { a.OwnerUserId = "" }), true},
		{"empty hostname", "LT-0042", with(func(a *adlv1.Asset) { a.Hostname = "" }), false},
		{"kind mismatch", "LT-0042", with(func(a *adlv1.Asset) { a.Kind = adlv1.AssetKind_ASSET_KIND_SERVER }), false},
		{"bad owner", "LT-0042", with(func(a *adlv1.Asset) { a.OwnerUserId = "x" }), false},
		{"version 0", "LT-0042", with(func(a *adlv1.Asset) { a.Version = 0 }), false},
		{"no changed_at", "LT-0042", with(func(a *adlv1.Asset) { a.ChangedAt = nil }), false},
		{"other id", "LT-0043", good, false},
		{"user", "U-0117", &adlv1.User{Id: "U-0117", DisplayName: "Ana", Email: "e", GroupId: "G-03", Version: 1, ChangedAt: now}, true},
		{"user without email", "U-0117", &adlv1.User{Id: "U-0117", DisplayName: "Ana", Version: 1, ChangedAt: now}, false},
		{"user bad group", "U-0117", &adlv1.User{Id: "U-0117", DisplayName: "Ana", Email: "e", GroupId: "G3", Version: 1, ChangedAt: now}, false},
		{"group", "G-03", &adlv1.Group{Id: "G-03", Name: "Ventas", Version: 2, ChangedAt: now}, true},
		{"group without name", "G-03", &adlv1.Group{Id: "G-03", Version: 2, ChangedAt: now}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := ValidateEntity(tt.id, tt.m)
			if tt.ok != (err == nil) {
				t.Fatalf("ValidateEntity = %d, %v; want ok=%v", v, err, tt.ok)
			}
		})
	}
}

func TestTypes(t *testing.T) {
	for e, want := range map[adlv1.EntityType]string{
		adlv1.EntityType_ENTITY_TYPE_ASSET: "asset", adlv1.EntityType_ENTITY_TYPE_USER: "user", adlv1.EntityType_ENTITY_TYPE_GROUP: "group",
	} {
		et, ok := TypeOf(e)
		if !ok || et.Name != want || et.TriggerKey("X") != want+":X" {
			t.Errorf("TypeOf(%s) = %+v", e, et)
		}
	}
	if _, ok := TypeOf(adlv1.EntityType_ENTITY_TYPE_UNSPECIFIED); ok {
		t.Error("UNSPECIFIED has a type")
	}
	if _, ok := Asset.New().(*adlv1.Asset); !ok {
		t.Error("Asset.New is not an Asset")
	}
}
