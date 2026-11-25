package labkit

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"google.golang.org/protobuf/proto"

	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
)

// Topic names (spec §8.2).
const (
	TopicTriggers       = "triggers"
	TopicEntityAssets   = "entities.assets"
	TopicEntityUsers    = "entities.users"
	TopicEntityGroups   = "entities.groups"
	TopicDLQAdapter     = "dlq.adapter"
	TopicDLQProcessor   = "dlq.processor"
	HeaderTriggerVer    = "trigger.version"
	HeaderErrorReason   = "error.reason"
	HeaderErrorMessage  = "error.message"
	HeaderSourceTopic   = "source.topic"
	HeaderSourcePart    = "source.partition"
	HeaderSourceOffset  = "source.offset"
	HeaderFailedAt      = "failed.at"
	HeaderService       = "service"
	ReasonDecodeError   = "DECODE_ERROR"
	ReasonValidation    = "VALIDATION_ERROR"
	ReasonNotFound      = "NOT_FOUND"
	ReasonStaleVersion  = "STALE_VERSION"
	EntityTypeNameAsset = "asset"
	EntityTypeNameUser  = "user"
	EntityTypeNameGroup = "group"
)

// EntityTopics lists the three entities.* topics.
var EntityTopics = []string{TopicEntityAssets, TopicEntityUsers, TopicEntityGroups}

// Id patterns (spec §5.1).
var (
	AssetIDPattern = regexp.MustCompile(`^(LT|DT|SV)-\d{4}$`)
	UserIDPattern  = regexp.MustCompile(`^U-\d{4}$`)
	GroupIDPattern = regexp.MustCompile(`^G-\d{2}$`)
)

// ErrDecode and ErrValidation classify record failures like the DLQ reasons.
var (
	ErrDecode     = errors.New(ReasonDecodeError)
	ErrValidation = errors.New(ReasonValidation)
)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, args...))
}

// AssetKindForID returns the kind that matches the id prefix (spec §5.1).
func AssetKindForID(id string) adlv1.AssetKind {
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

// KindName returns LAPTOP, DESKTOP or SERVER: the enum name without its prefix,
// which is how Doris, the lake and the explorer spell it.
func KindName(k adlv1.AssetKind) string {
	return strings.TrimPrefix(k.String(), "ASSET_KIND_")
}

// EntityTypeName returns asset, user or group, as used in trigger keys.
func EntityTypeName(t adlv1.EntityType) string {
	switch t {
	case adlv1.EntityType_ENTITY_TYPE_ASSET:
		return EntityTypeNameAsset
	case adlv1.EntityType_ENTITY_TYPE_USER:
		return EntityTypeNameUser
	case adlv1.EntityType_ENTITY_TYPE_GROUP:
		return EntityTypeNameGroup
	}
	return ""
}

// TriggerKey is the triggers topic key: "<type>:<id>" (spec §6.8).
func TriggerKey(t adlv1.EntityType, id string) string {
	return EntityTypeName(t) + ":" + id
}

// ValidateTrigger applies spec §6.7 to a trigger and its Kafka key.
func ValidateTrigger(key string, t *adlv1.Trigger) error {
	var pattern *regexp.Regexp
	switch t.GetEntityType() {
	case adlv1.EntityType_ENTITY_TYPE_ASSET:
		pattern = AssetIDPattern
	case adlv1.EntityType_ENTITY_TYPE_USER:
		pattern = UserIDPattern
	case adlv1.EntityType_ENTITY_TYPE_GROUP:
		pattern = GroupIDPattern
	default:
		return invalid("entity_type is %s", t.GetEntityType())
	}
	if !pattern.MatchString(t.GetEntityId()) {
		return invalid("entity_id %q does not match %s", t.GetEntityId(), pattern)
	}
	if t.GetVersion() < 1 {
		return invalid("version %d < 1", t.GetVersion())
	}
	if t.GetChangedAt() == nil {
		return invalid("changed_at is not set")
	}
	if want := TriggerKey(t.GetEntityType(), t.GetEntityId()); key != want {
		return invalid("key %q differs from %q", key, want)
	}
	return nil
}

// ValidateAsset applies spec §6.7 to an asset, without the Kafka key rule.
func ValidateAsset(a *adlv1.Asset) error {
	if !AssetIDPattern.MatchString(a.GetId()) {
		return invalid("asset id %q does not match %s", a.GetId(), AssetIDPattern)
	}
	if a.GetHostname() == "" || a.GetOs() == "" || a.GetSite() == "" {
		return invalid("asset %s: hostname, os and site must not be empty", a.GetId())
	}
	if a.GetKind() == adlv1.AssetKind_ASSET_KIND_UNSPECIFIED || a.GetKind() != AssetKindForID(a.GetId()) {
		return invalid("asset %s: kind %s does not match the id prefix", a.GetId(), a.GetKind())
	}
	if a.GetOwnerUserId() != "" && !UserIDPattern.MatchString(a.GetOwnerUserId()) {
		return invalid("asset %s: owner_user_id %q does not match %s", a.GetId(), a.GetOwnerUserId(), UserIDPattern)
	}
	if a.GetVersion() < 1 {
		return invalid("asset %s: version %d < 1", a.GetId(), a.GetVersion())
	}
	if a.GetChangedAt() == nil {
		return invalid("asset %s: changed_at is not set", a.GetId())
	}
	return nil
}

// ValidateUser applies spec §6.7 to a user, without the Kafka key rule.
func ValidateUser(u *adlv1.User) error {
	if !UserIDPattern.MatchString(u.GetId()) {
		return invalid("user id %q does not match %s", u.GetId(), UserIDPattern)
	}
	if u.GetDisplayName() == "" || u.GetEmail() == "" {
		return invalid("user %s: display_name and email must not be empty", u.GetId())
	}
	if u.GetGroupId() != "" && !GroupIDPattern.MatchString(u.GetGroupId()) {
		return invalid("user %s: group_id %q does not match %s", u.GetId(), u.GetGroupId(), GroupIDPattern)
	}
	if u.GetVersion() < 1 {
		return invalid("user %s: version %d < 1", u.GetId(), u.GetVersion())
	}
	if u.GetChangedAt() == nil {
		return invalid("user %s: changed_at is not set", u.GetId())
	}
	return nil
}

// ValidateGroup applies spec §6.7 to a group, without the Kafka key rule.
func ValidateGroup(g *adlv1.Group) error {
	if !GroupIDPattern.MatchString(g.GetId()) {
		return invalid("group id %q does not match %s", g.GetId(), GroupIDPattern)
	}
	if g.GetName() == "" {
		return invalid("group %s: name must not be empty", g.GetId())
	}
	if g.GetVersion() < 1 {
		return invalid("group %s: version %d < 1", g.GetId(), g.GetVersion())
	}
	if g.GetChangedAt() == nil {
		return invalid("group %s: changed_at is not set", g.GetId())
	}
	return nil
}

// Entity is a decoded and validated record of an entities.* topic.
type Entity struct {
	Type    string // asset, user or group
	ID      string
	Version int64
	Msg     proto.Message // *adlv1.Asset, *adlv1.User or *adlv1.Group
}

// Asset returns the payload as an Asset, or nil.
func (e Entity) Asset() *adlv1.Asset { a, _ := e.Msg.(*adlv1.Asset); return a }

// User returns the payload as a User, or nil.
func (e Entity) User() *adlv1.User { u, _ := e.Msg.(*adlv1.User); return u }

// Group returns the payload as a Group, or nil.
func (e Entity) Group() *adlv1.Group { g, _ := e.Msg.(*adlv1.Group); return g }

// DecodeEntity decodes and validates a record of an entities.* topic: the
// topic decides the type, the value must decode and pass spec §6.7, and the key
// must equal the id. The error wraps ErrDecode or ErrValidation.
func DecodeEntity(topic string, key, value []byte) (Entity, error) {
	var (
		e   Entity
		err error
	)
	switch topic {
	case TopicEntityAssets:
		a := &adlv1.Asset{}
		if uerr := proto.Unmarshal(value, a); uerr != nil {
			return e, fmt.Errorf("%w: %w", ErrDecode, uerr)
		}
		e, err = Entity{EntityTypeNameAsset, a.GetId(), a.GetVersion(), a}, ValidateAsset(a)
	case TopicEntityUsers:
		u := &adlv1.User{}
		if uerr := proto.Unmarshal(value, u); uerr != nil {
			return e, fmt.Errorf("%w: %w", ErrDecode, uerr)
		}
		e, err = Entity{EntityTypeNameUser, u.GetId(), u.GetVersion(), u}, ValidateUser(u)
	case TopicEntityGroups:
		g := &adlv1.Group{}
		if uerr := proto.Unmarshal(value, g); uerr != nil {
			return e, fmt.Errorf("%w: %w", ErrDecode, uerr)
		}
		e, err = Entity{EntityTypeNameGroup, g.GetId(), g.GetVersion(), g}, ValidateGroup(g)
	default:
		return e, fmt.Errorf("%w: %q is not an entities topic", ErrValidation, topic)
	}
	if err != nil {
		return e, err
	}
	if string(key) != e.ID {
		return e, invalid("key %q differs from id %q", key, e.ID)
	}
	return e, nil
}

// DecodeTrigger decodes and validates a record of the triggers topic.
func DecodeTrigger(key, value []byte) (*adlv1.Trigger, error) {
	t := &adlv1.Trigger{}
	if err := proto.Unmarshal(value, t); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDecode, err)
	}
	if err := ValidateTrigger(string(key), t); err != nil {
		return t, err
	}
	return t, nil
}
