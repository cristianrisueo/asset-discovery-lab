// Package model holds the datacore domain rules: entity types, id patterns,
// reserved ranges (spec §5.1, §5.4) and the validation of admin requests
// (spec §7.8).
package model

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
)

// Type is an entity type as it appears in trigger keys: asset, user or group.
type Type string

// The three entity types (spec §6.8).
const (
	TypeAsset Type = "asset"
	TypeUser  Type = "user"
	TypeGroup Type = "group"
)

// EntityType returns the Protobuf enum of t.
func (t Type) EntityType() adlv1.EntityType {
	switch t {
	case TypeAsset:
		return adlv1.EntityType_ENTITY_TYPE_ASSET
	case TypeUser:
		return adlv1.EntityType_ENTITY_TYPE_USER
	case TypeGroup:
		return adlv1.EntityType_ENTITY_TYPE_GROUP
	}
	return adlv1.EntityType_ENTITY_TYPE_UNSPECIFIED
}

// TriggerKey is the key of a trigger record: "<type>:<id>" (spec §6.8).
func TriggerKey(t Type, id string) string { return string(t) + ":" + id }

// Id patterns (spec §5.1).
var (
	AssetIDPattern = regexp.MustCompile(`^(LT|DT|SV)-\d{4}$`)
	UserIDPattern  = regexp.MustCompile(`^U-\d{4}$`)
	GroupIDPattern = regexp.MustCompile(`^G-\d{2}$`)
)

// ValidID reports whether id matches the pattern of t.
func ValidID(t Type, id string) bool {
	switch t {
	case TypeAsset:
		return AssetIDPattern.MatchString(id)
	case TypeUser:
		return UserIDPattern.MatchString(id)
	case TypeGroup:
		return GroupIDPattern.MatchString(id)
	}
	return false
}

// KindForID returns the asset kind that matches the id prefix.
func KindForID(id string) adlv1.AssetKind {
	switch {
	case strings.HasPrefix(id, "LT-"):
		return adlv1.AssetKind_ASSET_KIND_LAPTOP
	case strings.HasPrefix(id, "DT-"):
		return adlv1.AssetKind_ASSET_KIND_DESKTOP
	case strings.HasPrefix(id, "SV-"):
		return adlv1.AssetKind_ASSET_KIND_SERVER
	}
	return adlv1.AssetKind_ASSET_KIND_UNSPECIFIED
}

// NotFoundAssetID is never created by anyone: tests use it as the known
// "not found" id (spec §5.4).
const NotFoundAssetID = "LT-9999"

// Phantom asset ids for chaos: LT-8000 to LT-8999, never created (spec §5.4).
const (
	PhantomMin = 8000
	PhantomMax = 8999
)

// NeverCreated reports whether an asset id belongs to a range that nobody may
// create: LT-9999 and the phantom range LT-8000 to LT-8999 (spec §5.4).
func NeverCreated(id string) bool {
	if id == NotFoundAssetID {
		return true
	}
	if !strings.HasPrefix(id, "LT-") || !AssetIDPattern.MatchString(id) {
		return false
	}
	n, err := strconv.Atoi(id[3:])
	return err == nil && n >= PhantomMin && n <= PhantomMax
}

// ErrInvalid marks a malformed request (gRPC INVALID_ARGUMENT).
var ErrInvalid = errors.New("invalid argument")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// ValidateAssetRequest checks an UpsertAssetRequest without its references:
// id pattern, never-created ranges, kind consistent with the prefix, required
// fields, and the owner id pattern (spec §7.8).
func ValidateAssetRequest(r *adlv1.UpsertAssetRequest) error {
	id := r.GetId()
	if !AssetIDPattern.MatchString(id) {
		return invalid("asset id %q does not match %s", id, AssetIDPattern)
	}
	if NeverCreated(id) {
		return invalid("asset id %s is reserved and is never created", id)
	}
	if r.GetKind() == adlv1.AssetKind_ASSET_KIND_UNSPECIFIED {
		return invalid("asset %s: kind is required", id)
	}
	if r.GetKind() != KindForID(id) {
		return invalid("asset %s: kind %s does not match the id prefix", id, r.GetKind())
	}
	if strings.TrimSpace(r.GetHostname()) == "" {
		return invalid("asset %s: hostname is required", id)
	}
	if strings.TrimSpace(r.GetOs()) == "" {
		return invalid("asset %s: os is required", id)
	}
	if strings.TrimSpace(r.GetSite()) == "" {
		return invalid("asset %s: site is required", id)
	}
	if o := r.GetOwnerUserId(); o != "" && !UserIDPattern.MatchString(o) {
		return invalid("asset %s: owner_user_id %q does not match %s", id, o, UserIDPattern)
	}
	return nil
}

// ValidateUserRequest checks an UpsertUserRequest without its references.
func ValidateUserRequest(r *adlv1.UpsertUserRequest) error {
	id := r.GetId()
	if !UserIDPattern.MatchString(id) {
		return invalid("user id %q does not match %s", id, UserIDPattern)
	}
	if strings.TrimSpace(r.GetDisplayName()) == "" {
		return invalid("user %s: display_name is required", id)
	}
	if strings.TrimSpace(r.GetEmail()) == "" {
		return invalid("user %s: email is required", id)
	}
	if g := r.GetGroupId(); g != "" && !GroupIDPattern.MatchString(g) {
		return invalid("user %s: group_id %q does not match %s", id, g, GroupIDPattern)
	}
	return nil
}

// ValidateGroupRequest checks an UpsertGroupRequest.
func ValidateGroupRequest(r *adlv1.UpsertGroupRequest) error {
	id := r.GetId()
	if !GroupIDPattern.MatchString(id) {
		return invalid("group id %q does not match %s", id, GroupIDPattern)
	}
	if strings.TrimSpace(r.GetName()) == "" {
		return invalid("group %s: name is required", id)
	}
	return nil
}

// AssetFromRequest builds the new state of an asset from an upsert. Version
// and changed_at are set by the store when the change is applied.
func AssetFromRequest(r *adlv1.UpsertAssetRequest) *adlv1.Asset {
	return &adlv1.Asset{
		Id: r.GetId(), Hostname: r.GetHostname(), Kind: r.GetKind(),
		Os: r.GetOs(), Site: r.GetSite(), OwnerUserId: r.GetOwnerUserId(),
	}
}

// UserFromRequest builds the new state of a user from an upsert.
func UserFromRequest(r *adlv1.UpsertUserRequest) *adlv1.User {
	return &adlv1.User{Id: r.GetId(), DisplayName: r.GetDisplayName(), Email: r.GetEmail(), GroupId: r.GetGroupId()}
}

// GroupFromRequest builds the new state of a group from an upsert.
func GroupFromRequest(r *adlv1.UpsertGroupRequest) *adlv1.Group {
	return &adlv1.Group{Id: r.GetId(), Name: r.GetName()}
}

// ValidateGeneratorConfig checks the generator ranges (spec §6.5, §7.8).
func ValidateGeneratorConfig(c *adlv1.GeneratorConfig) error {
	if c == nil {
		return invalid("generator config is required")
	}
	r := c.GetRatePerSecond()
	if r != r || r < 0 || r > 500 { // r != r is NaN
		return invalid("rate_per_second %v is outside [0, 500]", r)
	}
	return nil
}

// MaxBurst is the largest Burst count (spec §6.5).
const MaxBurst = 10000

// ValidateBurst checks a Burst count.
func ValidateBurst(n int32) error {
	if n < 1 || n > MaxBurst {
		return invalid("count %d is outside [1, %d]", n, MaxBurst)
	}
	return nil
}
