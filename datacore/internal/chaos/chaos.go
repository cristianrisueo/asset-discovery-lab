// Package chaos holds datacore's chaos configuration and its random decisions
// (spec §7.9). The configuration lives in memory and starts from the
// environment. Every decision draws from one injected random source, so tests
// can seed it.
package chaos

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/cristianrisueo/asset-discovery-lab/datacore/internal/model"
	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
)

// MaxStaleWindowMs is the upper bound of stale_window_ms (spec §6.5).
const MaxStaleWindowMs = 10000

// ErrInvalid marks a configuration outside its ranges.
var ErrInvalid = errors.New("invalid chaos config")

// Validate checks the ranges of spec §7.8: rates from 0 to 1 and
// stale_window_ms from 0 to 10000.
func Validate(c *adlv1.ChaosConfig) error {
	if c == nil {
		return fmt.Errorf("%w: config is required", ErrInvalid)
	}
	for name, r := range map[string]float64{
		"duplicate_rate":   c.GetDuplicateRate(),
		"malformed_rate":   c.GetMalformedRate(),
		"phantom_rate":     c.GetPhantomRate(),
		"stale_read_rate":  c.GetStaleReadRate(),
		"unavailable_rate": c.GetUnavailableRate(),
	} {
		if r != r || r < 0 || r > 1 { // r != r is NaN
			return fmt.Errorf("%w: %s %v is outside [0, 1]", ErrInvalid, name, r)
		}
	}
	if w := c.GetStaleWindowMs(); w < 0 || w > MaxStaleWindowMs {
		return fmt.Errorf("%w: stale_window_ms %d is outside [0, %d]", ErrInvalid, w, MaxStaleWindowMs)
	}
	return nil
}

// Chaos is the live chaos configuration with its random source.
type Chaos struct {
	mu  sync.Mutex
	cfg *adlv1.ChaosConfig
	rnd *rand.Rand
}

// New returns a Chaos with an initial configuration, which must be valid.
func New(cfg *adlv1.ChaosConfig, rnd *rand.Rand) (*Chaos, error) {
	if err := Validate(cfg); err != nil {
		return nil, err
	}
	return &Chaos{cfg: proto.Clone(cfg).(*adlv1.ChaosConfig), rnd: rnd}, nil
}

// Config returns a copy of the effective configuration.
func (c *Chaos) Config() *adlv1.ChaosConfig {
	c.mu.Lock()
	defer c.mu.Unlock()
	return proto.Clone(c.cfg).(*adlv1.ChaosConfig)
}

// Set replaces the configuration and returns the effective one.
func (c *Chaos) Set(cfg *adlv1.ChaosConfig) (*adlv1.ChaosConfig, error) {
	if err := Validate(cfg); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cfg = proto.Clone(cfg).(*adlv1.ChaosConfig)
	return proto.Clone(c.cfg).(*adlv1.ChaosConfig), nil
}

// hit draws once and reports whether it falls under rate. It must be called
// with mu held.
func (c *Chaos) hit(rate float64) bool {
	return rate > 0 && c.rnd.Float64() < rate
}

func (c *Chaos) decide(rate func(*adlv1.ChaosConfig) float64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hit(rate(c.cfg))
}

// Unavailable decides whether a Get* call returns UNAVAILABLE (spec §7.7).
func (c *Chaos) Unavailable() bool {
	return c.decide((*adlv1.ChaosConfig).GetUnavailableRate)
}

// Duplicate decides whether the relay publishes a trigger twice.
func (c *Chaos) Duplicate() bool {
	return c.decide((*adlv1.ChaosConfig).GetDuplicateRate)
}

// Malformed decides whether the relay also publishes a malformed record.
func (c *Chaos) Malformed() bool {
	return c.decide((*adlv1.ChaosConfig).GetMalformedRate)
}

// Phantom decides whether the relay also publishes a phantom trigger.
func (c *Chaos) Phantom() bool {
	return c.decide((*adlv1.ChaosConfig).GetPhantomRate)
}

// StaleRead decides whether a Get* call returns the previous state: only when
// the entity has one and changed less than stale_window_ms ago, and then with
// probability stale_read_rate (spec §7.7).
func (c *Chaos) StaleRead(changedAt, now time.Time, hasPrevious bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	window := time.Duration(c.cfg.GetStaleWindowMs()) * time.Millisecond
	if !hasPrevious || window <= 0 || now.Sub(changedAt) >= window {
		return false
	}
	return c.hit(c.cfg.GetStaleReadRate())
}

// MalformedKey is the key of a malformed record (spec §7.9).
func MalformedKey(seq int64) string { return fmt.Sprintf("malformed:%d", seq) }

// MalformedValue builds the malformed record for an outbox seq. Odd seq gives
// 16 to 64 random bytes; even seq gives a Trigger with ENTITY_TYPE_UNSPECIFIED
// (spec §7.9).
func (c *Chaos) MalformedValue(seq int64, now time.Time) []byte {
	if seq%2 != 0 {
		c.mu.Lock()
		defer c.mu.Unlock()
		b := make([]byte, 16+c.rnd.IntN(49))
		for i := range b {
			b[i] = byte(c.rnd.UintN(256))
		}
		return b
	}
	v, _ := proto.Marshal(&adlv1.Trigger{
		EntityType: adlv1.EntityType_ENTITY_TYPE_UNSPECIFIED,
		EntityId:   fmt.Sprintf("LT-%04d", seq%10000),
		Version:    1,
		ChangedAt:  timestamppb.New(now),
	})
	return v
}

// PhantomTrigger builds a valid trigger, version 1, for a random phantom id
// LT-8000 to LT-8999 (spec §7.9). It returns the record key and value.
func (c *Chaos) PhantomTrigger(now time.Time) (key string, value []byte) {
	c.mu.Lock()
	n := model.PhantomMin + c.rnd.IntN(model.PhantomMax-model.PhantomMin+1)
	c.mu.Unlock()
	id := fmt.Sprintf("LT-%04d", n)
	v, _ := proto.Marshal(&adlv1.Trigger{
		EntityType: adlv1.EntityType_ENTITY_TYPE_ASSET,
		EntityId:   id,
		Version:    1,
		ChangedAt:  timestamppb.New(now),
	})
	return model.TriggerKey(model.TypeAsset, id), v
}

// FromEnv builds the initial configuration from the CHAOS_* values (spec §7.10).
func FromEnv(duplicate, malformed, phantom, staleRead, unavailable float64, staleWindowMs int32) *adlv1.ChaosConfig {
	return &adlv1.ChaosConfig{
		DuplicateRate:   duplicate,
		MalformedRate:   malformed,
		PhantomRate:     phantom,
		StaleReadRate:   staleRead,
		StaleWindowMs:   staleWindowMs,
		UnavailableRate: unavailable,
	}
}
