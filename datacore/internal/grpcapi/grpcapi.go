// Package grpcapi is datacore's transport: the DataCoreService the adapter
// calls (spec §7.7) and the lab AdminService (spec §7.8).
package grpcapi

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/cristianrisueo/asset-discovery-lab/datacore/internal/chaos"
	"github.com/cristianrisueo/asset-discovery-lab/datacore/internal/generator"
	"github.com/cristianrisueo/asset-discovery-lab/datacore/internal/model"
	"github.com/cristianrisueo/asset-discovery-lab/datacore/internal/store"
	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
)

// Store is the part of *store.Store the services use.
type Store interface {
	Apply(ctx context.Context, t model.Type, id string, mutate store.Mutator) (proto.Message, error)
	Get(ctx context.Context, t model.Type, id string) (store.Snapshot, error)
	Exists(ctx context.Context, t model.Type, id string) (bool, error)
	List(ctx context.Context, t model.Type) ([]proto.Message, error)
	Counts(ctx context.Context) (assets, users, groups int64, err error)
	OutboxPending(ctx context.Context) (int64, error)
	ChangesApplied() int64
}

// Deps are the collaborators of both services.
type Deps struct {
	Store     Store
	Chaos     *chaos.Chaos
	Generator *generator.Generator
	Log       *slog.Logger
	// Published reports the triggers the relay published since start.
	Published func() int64
	Now       func() time.Time
}

// Services implements DataCoreService and AdminService.
type Services struct {
	adlv1.UnimplementedDataCoreServiceServer
	adlv1.UnimplementedAdminServiceServer

	d        Deps
	getCalls atomic.Int64
}

// New returns both services.
func New(d Deps) *Services {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Published == nil {
		d.Published = func() int64 { return 0 }
	}
	return &Services{d: d}
}

// internal logs an unexpected error and maps it to INTERNAL.
func (s *Services) internal(what string, err error) error {
	s.d.Log.Error(what, "error", err.Error())
	return status.Errorf(codes.Internal, "%s: %v", what, err)
}

// --- DataCoreService (spec §7.7) ---------------------------------------------

// get serves one Get* call: count it, apply unavailable chaos before any
// lookup, validate the id, look it up, and apply stale-read chaos.
func (s *Services) get(ctx context.Context, t model.Type, id string) (proto.Message, error) {
	s.getCalls.Add(1)
	if s.d.Chaos.Unavailable() {
		return nil, status.Error(codes.Unavailable, "chaos: datacore unavailable")
	}
	if !model.ValidID(t, id) {
		return nil, status.Errorf(codes.InvalidArgument, "%s id %q is malformed", t, id)
	}
	snap, err := s.d.Store.Get(ctx, t, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, status.Errorf(codes.NotFound, "%s %s not found", t, id)
	}
	if err != nil {
		return nil, s.internal("get "+string(t), err)
	}
	if s.d.Chaos.StaleRead(snap.ChangedAt, s.d.Now(), snap.Previous != nil) {
		return snap.Previous, nil
	}
	return snap.Current, nil
}

// GetAsset implements DataCoreService.
func (s *Services) GetAsset(ctx context.Context, r *adlv1.GetAssetRequest) (*adlv1.GetAssetResponse, error) {
	m, err := s.get(ctx, model.TypeAsset, r.GetId())
	if err != nil {
		return nil, err
	}
	return &adlv1.GetAssetResponse{Asset: m.(*adlv1.Asset)}, nil
}

// GetUser implements DataCoreService.
func (s *Services) GetUser(ctx context.Context, r *adlv1.GetUserRequest) (*adlv1.GetUserResponse, error) {
	m, err := s.get(ctx, model.TypeUser, r.GetId())
	if err != nil {
		return nil, err
	}
	return &adlv1.GetUserResponse{User: m.(*adlv1.User)}, nil
}

// GetGroup implements DataCoreService.
func (s *Services) GetGroup(ctx context.Context, r *adlv1.GetGroupRequest) (*adlv1.GetGroupResponse, error) {
	m, err := s.get(ctx, model.TypeGroup, r.GetId())
	if err != nil {
		return nil, err
	}
	return &adlv1.GetGroupResponse{Group: m.(*adlv1.Group)}, nil
}

// --- AdminService: upserts (spec §7.8) ---------------------------------------

// requireRef returns FAILED_PRECONDITION when a non-empty reference does not exist.
func (s *Services) requireRef(ctx context.Context, t model.Type, id string) error {
	if id == "" {
		return nil
	}
	ok, err := s.d.Store.Exists(ctx, t, id)
	if err != nil {
		return s.internal("look up "+string(t), err)
	}
	if !ok {
		return status.Errorf(codes.FailedPrecondition, "%s %s does not exist", t, id)
	}
	return nil
}

// upsert applies a validated upsert: every call creates a new version.
func (s *Services) upsert(ctx context.Context, t model.Type, id string, next proto.Message) (proto.Message, error) {
	m, err := s.d.Store.Apply(ctx, t, id, func(proto.Message) (proto.Message, error) { return next, nil })
	if err != nil {
		return nil, s.internal("upsert "+string(t), err)
	}
	return m, nil
}

func invalidArgument(err error) error { return status.Error(codes.InvalidArgument, err.Error()) }

// UpsertAsset implements AdminService.
func (s *Services) UpsertAsset(ctx context.Context, r *adlv1.UpsertAssetRequest) (*adlv1.UpsertAssetResponse, error) {
	if err := model.ValidateAssetRequest(r); err != nil {
		return nil, invalidArgument(err)
	}
	if err := s.requireRef(ctx, model.TypeUser, r.GetOwnerUserId()); err != nil {
		return nil, err
	}
	m, err := s.upsert(ctx, model.TypeAsset, r.GetId(), model.AssetFromRequest(r))
	if err != nil {
		return nil, err
	}
	return &adlv1.UpsertAssetResponse{Asset: m.(*adlv1.Asset)}, nil
}

// UpsertUser implements AdminService.
func (s *Services) UpsertUser(ctx context.Context, r *adlv1.UpsertUserRequest) (*adlv1.UpsertUserResponse, error) {
	if err := model.ValidateUserRequest(r); err != nil {
		return nil, invalidArgument(err)
	}
	if err := s.requireRef(ctx, model.TypeGroup, r.GetGroupId()); err != nil {
		return nil, err
	}
	m, err := s.upsert(ctx, model.TypeUser, r.GetId(), model.UserFromRequest(r))
	if err != nil {
		return nil, err
	}
	return &adlv1.UpsertUserResponse{User: m.(*adlv1.User)}, nil
}

// UpsertGroup implements AdminService.
func (s *Services) UpsertGroup(ctx context.Context, r *adlv1.UpsertGroupRequest) (*adlv1.UpsertGroupResponse, error) {
	if err := model.ValidateGroupRequest(r); err != nil {
		return nil, invalidArgument(err)
	}
	m, err := s.upsert(ctx, model.TypeGroup, r.GetId(), model.GroupFromRequest(r))
	if err != nil {
		return nil, err
	}
	return &adlv1.UpsertGroupResponse{Group: m.(*adlv1.Group)}, nil
}

// --- AdminService: listings ---------------------------------------------------

func (s *Services) list(ctx context.Context, t model.Type, send func(proto.Message) error) error {
	all, err := s.d.Store.List(ctx, t)
	if err != nil {
		return s.internal("list "+string(t), err)
	}
	for _, m := range all {
		if err := send(m); err != nil {
			return err
		}
	}
	return nil
}

// ListAssets implements AdminService.
func (s *Services) ListAssets(_ *adlv1.ListAssetsRequest, st adlv1.AdminService_ListAssetsServer) error {
	return s.list(st.Context(), model.TypeAsset, func(m proto.Message) error {
		return st.Send(&adlv1.ListAssetsResponse{Asset: m.(*adlv1.Asset)})
	})
}

// ListUsers implements AdminService.
func (s *Services) ListUsers(_ *adlv1.ListUsersRequest, st adlv1.AdminService_ListUsersServer) error {
	return s.list(st.Context(), model.TypeUser, func(m proto.Message) error {
		return st.Send(&adlv1.ListUsersResponse{User: m.(*adlv1.User)})
	})
}

// ListGroups implements AdminService.
func (s *Services) ListGroups(_ *adlv1.ListGroupsRequest, st adlv1.AdminService_ListGroupsServer) error {
	return s.list(st.Context(), model.TypeGroup, func(m proto.Message) error {
		return st.Send(&adlv1.ListGroupsResponse{Group: m.(*adlv1.Group)})
	})
}

// --- AdminService: generator, chaos, burst, stats ---------------------------

// GetGenerator implements AdminService.
func (s *Services) GetGenerator(context.Context, *adlv1.GetGeneratorRequest) (*adlv1.GetGeneratorResponse, error) {
	return &adlv1.GetGeneratorResponse{Config: s.d.Generator.Config()}, nil
}

// SetGenerator implements AdminService.
func (s *Services) SetGenerator(_ context.Context, r *adlv1.SetGeneratorRequest) (*adlv1.SetGeneratorResponse, error) {
	cfg, err := s.d.Generator.SetConfig(r.GetConfig())
	if err != nil {
		return nil, invalidArgument(err)
	}
	s.d.Log.Info("generator configured", "rate_per_second", cfg.GetRatePerSecond(), "paused", cfg.GetPaused())
	return &adlv1.SetGeneratorResponse{Config: cfg}, nil
}

// GetChaos implements AdminService.
func (s *Services) GetChaos(context.Context, *adlv1.GetChaosRequest) (*adlv1.GetChaosResponse, error) {
	return &adlv1.GetChaosResponse{Config: s.d.Chaos.Config()}, nil
}

// SetChaos implements AdminService.
func (s *Services) SetChaos(_ context.Context, r *adlv1.SetChaosRequest) (*adlv1.SetChaosResponse, error) {
	cfg, err := s.d.Chaos.Set(r.GetConfig())
	if err != nil {
		return nil, invalidArgument(err)
	}
	s.d.Log.Info("chaos configured",
		"duplicate_rate", cfg.GetDuplicateRate(), "malformed_rate", cfg.GetMalformedRate(),
		"phantom_rate", cfg.GetPhantomRate(), "stale_read_rate", cfg.GetStaleReadRate(),
		"stale_window_ms", cfg.GetStaleWindowMs(), "unavailable_rate", cfg.GetUnavailableRate())
	return &adlv1.SetChaosResponse{Config: cfg}, nil
}

// Burst implements AdminService: count generator changes, synchronously,
// even when the generator is paused.
func (s *Services) Burst(ctx context.Context, r *adlv1.BurstRequest) (*adlv1.BurstResponse, error) {
	if err := model.ValidateBurst(r.GetCount()); err != nil {
		return nil, invalidArgument(err)
	}
	n, err := s.d.Generator.Burst(ctx, int(r.GetCount()))
	if err != nil && ctx.Err() == nil {
		return nil, s.internal("burst", err)
	}
	return &adlv1.BurstResponse{Applied: int32(n)}, nil // n <= MaxBurst
}

// GetStats implements AdminService.
func (s *Services) GetStats(ctx context.Context, _ *adlv1.GetStatsRequest) (*adlv1.GetStatsResponse, error) {
	pending, err := s.d.Store.OutboxPending(ctx)
	if err != nil {
		return nil, s.internal("stats", err)
	}
	a, u, g, err := s.d.Store.Counts(ctx)
	if err != nil {
		return nil, s.internal("stats", err)
	}
	return &adlv1.GetStatsResponse{
		ChangesApplied:    s.d.Store.ChangesApplied(),
		TriggersPublished: s.d.Published(),
		OutboxPending:     pending,
		GetCalls:          s.getCalls.Load(),
		Assets:            a,
		Users:             u,
		Groups:            g,
	}, nil
}
