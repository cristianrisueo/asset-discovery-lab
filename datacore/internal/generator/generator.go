// Package generator applies random changes to the seeded entities at a
// configurable rate (spec §7.6). It never touches reserved ids and only
// assigns seeded users and groups.
package generator

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/cristianrisueo/asset-discovery-lab/datacore/internal/model"
	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
)

// Applier applies one change in a transaction (store.Store.Apply).
type Applier interface {
	Apply(ctx context.Context, t model.Type, id string, mutate func(cur proto.Message) (proto.Message, error)) (proto.Message, error)
}

// ApplyFunc adapts a function to Applier.
type ApplyFunc func(ctx context.Context, t model.Type, id string, mutate func(cur proto.Message) (proto.Message, error)) (proto.Message, error)

// Apply calls f.
func (f ApplyFunc) Apply(ctx context.Context, t model.Type, id string, mutate func(cur proto.Message) (proto.Message, error)) (proto.Message, error) {
	return f(ctx, t, id, mutate)
}

// Kind is one kind of generated change.
type Kind int

// The change mix of spec §7.6.
const (
	AssetMove Kind = iota
	AssetReassign
	AssetOS
	UserGroup
	UserName
	GroupRename
)

func (k Kind) String() string {
	return [...]string{"asset_move", "asset_reassign", "asset_os", "user_group", "user_name", "group_rename"}[k]
}

// Mix holds the weight of every kind, in percent (spec §7.6).
var Mix = []struct {
	Kind   Kind
	Weight int
}{
	{AssetMove, 35}, {AssetReassign, 20}, {AssetOS, 15}, {UserGroup, 15}, {UserName, 10}, {GroupRename, 5},
}

// Generator applies random changes to seeded entities.
type Generator struct {
	store  Applier
	log    *slog.Logger
	assets []string
	users  []string
	groups []string

	rmu sync.Mutex
	rnd *rand.Rand

	cmu sync.Mutex
	cfg *adlv1.GeneratorConfig
}

// New returns a generator over the seeded ids of c.
func New(store Applier, c model.SeedCounts, cfg *adlv1.GeneratorConfig, rnd *rand.Rand, log *slog.Logger) (*Generator, error) {
	if err := model.ValidateGeneratorConfig(cfg); err != nil {
		return nil, err
	}
	return &Generator{
		store: store, log: log, rnd: rnd,
		assets: c.AssetIDs(), users: c.UserIDs(), groups: c.GroupIDs(),
		cfg: proto.Clone(cfg).(*adlv1.GeneratorConfig),
	}, nil
}

// Config returns the effective configuration.
func (g *Generator) Config() *adlv1.GeneratorConfig {
	g.cmu.Lock()
	defer g.cmu.Unlock()
	return proto.Clone(g.cfg).(*adlv1.GeneratorConfig)
}

// SetConfig validates and replaces the configuration (spec §7.8).
func (g *Generator) SetConfig(cfg *adlv1.GeneratorConfig) (*adlv1.GeneratorConfig, error) {
	if err := model.ValidateGeneratorConfig(cfg); err != nil {
		return nil, err
	}
	g.cmu.Lock()
	defer g.cmu.Unlock()
	g.cfg = proto.Clone(cfg).(*adlv1.GeneratorConfig)
	return proto.Clone(g.cfg).(*adlv1.GeneratorConfig), nil
}

func (g *Generator) intN(n int) int {
	g.rmu.Lock()
	defer g.rmu.Unlock()
	return g.rnd.IntN(n)
}

func (g *Generator) pickKind() Kind {
	n := g.intN(100)
	for _, m := range Mix {
		if n < m.Weight {
			return m.Kind
		}
		n -= m.Weight
	}
	return Mix[len(Mix)-1].Kind
}

// pickOther returns a random element of pool that differs from cur.
func (g *Generator) pickOther(pool []string, cur string) string {
	for range 64 {
		if v := pool[g.intN(len(pool))]; v != cur {
			return v
		}
	}
	for _, v := range pool {
		if v != cur {
			return v
		}
	}
	return cur
}

// ApplyOne applies one random change and returns its kind. A change that has
// started always finishes (the store detaches it from cancellation).
func (g *Generator) ApplyOne(ctx context.Context) (Kind, error) {
	kind := g.pickKind()
	var (
		t      model.Type
		id     string
		mutate func(proto.Message) (proto.Message, error)
	)
	switch kind {
	case AssetMove, AssetReassign, AssetOS:
		t, id = model.TypeAsset, g.assets[g.intN(len(g.assets))]
		mutate = func(cur proto.Message) (proto.Message, error) {
			a, ok := cur.(*adlv1.Asset)
			if !ok {
				return nil, fmt.Errorf("seeded asset %s does not exist", id)
			}
			a = proto.Clone(a).(*adlv1.Asset)
			switch kind {
			case AssetMove:
				a.Site = g.pickOther(model.Sites, a.GetSite())
			case AssetReassign:
				// Another seeded user, or unassigned.
				a.OwnerUserId = g.pickOther(append([]string{""}, g.users...), a.GetOwnerUserId())
			default:
				pool := model.OSPool(id)
				values := make([]string, len(pool))
				for i, v := range pool {
					values[i] = v.Value
				}
				a.Os = g.pickOther(values, a.GetOs())
			}
			return a, nil
		}
	case UserGroup, UserName:
		t, id = model.TypeUser, g.users[g.intN(len(g.users))]
		mutate = func(cur proto.Message) (proto.Message, error) {
			u, ok := cur.(*adlv1.User)
			if !ok {
				return nil, fmt.Errorf("seeded user %s does not exist", id)
			}
			u = proto.Clone(u).(*adlv1.User)
			if kind == UserGroup {
				u.GroupId = g.pickOther(g.groups, u.GetGroupId())
				return u, nil
			}
			for range 16 {
				g.rmu.Lock()
				name := model.RandomName(g.rnd)
				g.rmu.Unlock()
				if name != u.GetDisplayName() {
					u.DisplayName = name
					break
				}
			}
			return u, nil
		}
	default:
		t, id = model.TypeGroup, g.groups[g.intN(len(g.groups))]
		mutate = func(cur proto.Message) (proto.Message, error) {
			gr, ok := cur.(*adlv1.Group)
			if !ok {
				return nil, fmt.Errorf("seeded group %s does not exist", id)
			}
			gr = proto.Clone(gr).(*adlv1.Group)
			gr.Name = g.pickOther(model.GroupRenamePool, gr.GetName())
			return gr, nil
		}
	}
	if _, err := g.store.Apply(ctx, t, id, mutate); err != nil {
		return kind, fmt.Errorf("generator %s on %s: %w", kind, id, err)
	}
	return kind, nil
}

// Burst applies n changes synchronously, even when paused (spec §7.8). It
// stops early only when ctx ends, and returns how many it applied.
func (g *Generator) Burst(ctx context.Context, n int) (int, error) {
	for i := range n {
		if ctx.Err() != nil {
			return i, ctx.Err()
		}
		if _, err := g.ApplyOne(ctx); err != nil {
			return i, err
		}
	}
	return n, nil
}

// tick is how often Run checks how many changes it owes.
const tick = 20 * time.Millisecond

// Run applies changes at rate_per_second until ctx ends (spec §7.6). It
// returns after the change in progress, never in the middle of one.
func (g *Generator) Run(ctx context.Context) {
	t := time.NewTicker(tick)
	defer t.Stop()
	last := time.Now()
	owed := 0.0
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			cfg := g.Config()
			elapsed := now.Sub(last)
			last = now
			if cfg.GetPaused() || cfg.GetRatePerSecond() <= 0 {
				owed = 0
				continue
			}
			// Never owe more than one second of changes: a slow moment must
			// not turn into a flood afterwards.
			owed = min(owed+cfg.GetRatePerSecond()*elapsed.Seconds(), max(cfg.GetRatePerSecond(), 1))
			for owed >= 1 {
				if ctx.Err() != nil {
					return
				}
				owed--
				if _, err := g.ApplyOne(ctx); err != nil {
					g.log.Error("generator change failed", "error", err.Error())
				}
			}
		}
	}
}
