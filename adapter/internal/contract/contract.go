// Package contract holds the message rules the adapter applies: the entity
// types, the Kafka keys (spec §6.8) and the validation rules of spec §6.7.
package contract

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"google.golang.org/protobuf/proto"

	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
)

// EntityType describes one entity type.
type EntityType struct {
	Name        string // asset, user or group: the trigger key prefix and cache key part
	Enum        adlv1.EntityType
	TopicSuffix string // assets, users or groups: entities.<suffix>
	IDPattern   *regexp.Regexp
}

// The three entity types.
var (
	Asset = EntityType{"asset", adlv1.EntityType_ENTITY_TYPE_ASSET, "assets", regexp.MustCompile(`^(LT|DT|SV)-\d{4}$`)}
	User  = EntityType{"user", adlv1.EntityType_ENTITY_TYPE_USER, "users", regexp.MustCompile(`^U-\d{4}$`)}
	Group = EntityType{"group", adlv1.EntityType_ENTITY_TYPE_GROUP, "groups", regexp.MustCompile(`^G-\d{2}$`)}
)

// TypeOf returns the entity type of an enum value.
func TypeOf(e adlv1.EntityType) (EntityType, bool) {
	switch e {
	case adlv1.EntityType_ENTITY_TYPE_ASSET:
		return Asset, true
	case adlv1.EntityType_ENTITY_TYPE_USER:
		return User, true
	case adlv1.EntityType_ENTITY_TYPE_GROUP:
		return Group, true
	}
	return EntityType{}, false
}

// TriggerKey is the triggers key: "<type>:<id>" (spec §6.8).
func (t EntityType) TriggerKey(id string) string { return t.Name + ":" + id }

// New returns an empty message of the type.
func (t EntityType) New() proto.Message {
	switch t.Enum {
	case adlv1.EntityType_ENTITY_TYPE_ASSET:
		return &adlv1.Asset{}
	case adlv1.EntityType_ENTITY_TYPE_USER:
		return &adlv1.User{}
	default:
		return &adlv1.Group{}
	}
}

// ErrValidation marks a message that breaks spec §6.7.
var ErrValidation = errors.New("VALIDATION_ERROR")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, args...))
}

// ValidateTrigger applies spec §6.7 to a trigger and its Kafka key.
func ValidateTrigger(key string, t *adlv1.Trigger) error {
	et, ok := TypeOf(t.GetEntityType())
	if !ok {
		return invalid("entity_type is %s", t.GetEntityType())
	}
	if !et.IDPattern.MatchString(t.GetEntityId()) {
		return invalid("entity_id %q does not match %s", t.GetEntityId(), et.IDPattern)
	}
	if t.GetVersion() < 1 {
		return invalid("version %d < 1", t.GetVersion())
	}
	if t.GetChangedAt() == nil {
		return invalid("changed_at is not set")
	}
	if want := et.TriggerKey(t.GetEntityId()); key != want {
		return invalid("key %q differs from %q", key, want)
	}
	return nil
}

func kindForID(id string) adlv1.AssetKind {
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

// ValidateEntity applies spec §6.7 to an entity datacore returned for the
// requested id. It returns the entity version.
func ValidateEntity(id string, m proto.Message) (int64, error) {
	switch e := m.(type) {
	case *adlv1.Asset:
		switch {
		case !Asset.IDPattern.MatchString(e.GetId()):
			return 0, invalid("asset id %q does not match %s", e.GetId(), Asset.IDPattern)
		case e.GetHostname() == "" || e.GetOs() == "" || e.GetSite() == "":
			return 0, invalid("asset %s: hostname, os and site must not be empty", e.GetId())
		case e.GetKind() == adlv1.AssetKind_ASSET_KIND_UNSPECIFIED || e.GetKind() != kindForID(e.GetId()):
			return 0, invalid("asset %s: kind %s does not match the id prefix", e.GetId(), e.GetKind())
		case e.GetOwnerUserId() != "" && !User.IDPattern.MatchString(e.GetOwnerUserId()):
			return 0, invalid("asset %s: owner_user_id %q is malformed", e.GetId(), e.GetOwnerUserId())
		}
		return checkCommon(id, e.GetId(), e.GetVersion(), e.GetChangedAt() != nil)
	case *adlv1.User:
		switch {
		case !User.IDPattern.MatchString(e.GetId()):
			return 0, invalid("user id %q does not match %s", e.GetId(), User.IDPattern)
		case e.GetDisplayName() == "" || e.GetEmail() == "":
			return 0, invalid("user %s: display_name and email must not be empty", e.GetId())
		case e.GetGroupId() != "" && !Group.IDPattern.MatchString(e.GetGroupId()):
			return 0, invalid("user %s: group_id %q is malformed", e.GetId(), e.GetGroupId())
		}
		return checkCommon(id, e.GetId(), e.GetVersion(), e.GetChangedAt() != nil)
	case *adlv1.Group:
		switch {
		case !Group.IDPattern.MatchString(e.GetId()):
			return 0, invalid("group id %q does not match %s", e.GetId(), Group.IDPattern)
		case e.GetName() == "":
			return 0, invalid("group %s: name must not be empty", e.GetId())
		}
		return checkCommon(id, e.GetId(), e.GetVersion(), e.GetChangedAt() != nil)
	}
	return 0, invalid("unexpected message %T", m)
}

// checkCommon applies the rules shared by every entity: version >= 1,
// changed_at set, and the id equal to the requested one (on a Kafka record the
// key equals the id, spec §6.7).
func checkCommon(want, got string, version int64, hasChangedAt bool) (int64, error) {
	switch {
	case got != want:
		return 0, invalid("entity id %q differs from the requested %q", got, want)
	case version < 1:
		return 0, invalid("entity %s: version %d < 1", got, version)
	case !hasChangedAt:
		return 0, invalid("entity %s: changed_at is not set", got)
	}
	return version, nil
}
