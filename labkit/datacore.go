package labkit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"

	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
)

// ChaosOnConfig is the "make chaos-on" column of spec §7.9.
func ChaosOnConfig() *adlv1.ChaosConfig {
	return &adlv1.ChaosConfig{
		DuplicateRate:   0.05,
		MalformedRate:   0.01,
		PhantomRate:     0.01,
		StaleReadRate:   0.05,
		StaleWindowMs:   300,
		UnavailableRate: 0.05,
	}
}

// ChaosOffConfig sets every knob to zero (adlctl chaos off, spec §15.10).
func ChaosOffConfig() *adlv1.ChaosConfig {
	return &adlv1.ChaosConfig{}
}

// DefaultGeneratorRate is the default load: 10 changes per second (spec §1.5).
const DefaultGeneratorRate = 10

// Datacore is a client for both datacore gRPC services.
type Datacore struct {
	addr  string
	conn  *grpc.ClientConn
	Admin adlv1.AdminServiceClient
	Data  adlv1.DataCoreServiceClient
}

// NewDatacore builds a client. gRPC connects lazily, on the first call.
func NewDatacore(addr string) (*Datacore, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("datacore at %s: %w", addr, err)
	}
	return &Datacore{
		addr:  addr,
		conn:  conn,
		Admin: adlv1.NewAdminServiceClient(conn),
		Data:  adlv1.NewDataCoreServiceClient(conn),
	}, nil
}

// Close releases the connection.
func (d *Datacore) Close() error { return d.conn.Close() }

// Addr is the gRPC address.
func (d *Datacore) Addr() string { return d.addr }

func (d *Datacore) wrap(what string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("datacore %s at %s: %w", what, d.addr, err)
}

func callCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 10*time.Second)
}

// Ping calls GetStats.
func (d *Datacore) Ping(ctx context.Context) error {
	_, err := d.Stats(ctx)
	return err
}

// Stats returns the datacore counters.
func (d *Datacore) Stats(ctx context.Context) (*adlv1.GetStatsResponse, error) {
	ctx, cancel := callCtx(ctx)
	defer cancel()
	r, err := d.Admin.GetStats(ctx, &adlv1.GetStatsRequest{})
	return r, d.wrap("admin API", err)
}

// UpsertAsset creates or changes an asset and returns it.
func (d *Datacore) UpsertAsset(ctx context.Context, req *adlv1.UpsertAssetRequest) (*adlv1.Asset, error) {
	ctx, cancel := callCtx(ctx)
	defer cancel()
	r, err := d.Admin.UpsertAsset(ctx, req)
	if err != nil {
		return nil, d.wrap("admin API UpsertAsset("+req.GetId()+")", err)
	}
	return r.GetAsset(), nil
}

// UpsertUser creates or changes a user and returns it.
func (d *Datacore) UpsertUser(ctx context.Context, req *adlv1.UpsertUserRequest) (*adlv1.User, error) {
	ctx, cancel := callCtx(ctx)
	defer cancel()
	r, err := d.Admin.UpsertUser(ctx, req)
	if err != nil {
		return nil, d.wrap("admin API UpsertUser("+req.GetId()+")", err)
	}
	return r.GetUser(), nil
}

// UpsertGroup creates or changes a group and returns it.
func (d *Datacore) UpsertGroup(ctx context.Context, req *adlv1.UpsertGroupRequest) (*adlv1.Group, error) {
	ctx, cancel := callCtx(ctx)
	defer cancel()
	r, err := d.Admin.UpsertGroup(ctx, req)
	if err != nil {
		return nil, d.wrap("admin API UpsertGroup("+req.GetId()+")", err)
	}
	return r.GetGroup(), nil
}

// GetAsset calls DataCoreService.GetAsset. Chaos applies to it (spec §7.7).
func (d *Datacore) GetAsset(ctx context.Context, id string) (*adlv1.Asset, error) {
	ctx, cancel := callCtx(ctx)
	defer cancel()
	r, err := d.Data.GetAsset(ctx, &adlv1.GetAssetRequest{Id: id})
	if err != nil {
		return nil, d.wrap("GetAsset("+id+")", err)
	}
	return r.GetAsset(), nil
}

func listCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 60*time.Second)
}

// ListAssets streams every asset, ordered by id.
func (d *Datacore) ListAssets(ctx context.Context) ([]*adlv1.Asset, error) {
	ctx, cancel := listCtx(ctx)
	defer cancel()
	s, err := d.Admin.ListAssets(ctx, &adlv1.ListAssetsRequest{})
	if err != nil {
		return nil, d.wrap("admin API ListAssets", err)
	}
	var out []*adlv1.Asset
	for {
		m, err := s.Recv()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, d.wrap("admin API ListAssets", err)
		}
		out = append(out, m.GetAsset())
	}
}

// ListUsers streams every user, ordered by id.
func (d *Datacore) ListUsers(ctx context.Context) ([]*adlv1.User, error) {
	ctx, cancel := listCtx(ctx)
	defer cancel()
	s, err := d.Admin.ListUsers(ctx, &adlv1.ListUsersRequest{})
	if err != nil {
		return nil, d.wrap("admin API ListUsers", err)
	}
	var out []*adlv1.User
	for {
		m, err := s.Recv()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, d.wrap("admin API ListUsers", err)
		}
		out = append(out, m.GetUser())
	}
}

// ListGroups streams every group, ordered by id.
func (d *Datacore) ListGroups(ctx context.Context) ([]*adlv1.Group, error) {
	ctx, cancel := listCtx(ctx)
	defer cancel()
	s, err := d.Admin.ListGroups(ctx, &adlv1.ListGroupsRequest{})
	if err != nil {
		return nil, d.wrap("admin API ListGroups", err)
	}
	var out []*adlv1.Group
	for {
		m, err := s.Recv()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, d.wrap("admin API ListGroups", err)
		}
		out = append(out, m.GetGroup())
	}
}

// FindAsset returns the asset with id from AdminService.ListAssets, which
// chaos never touches, or nil when it does not exist.
func (d *Datacore) FindAsset(ctx context.Context, id string) (*adlv1.Asset, error) {
	all, err := d.ListAssets(ctx)
	if err != nil {
		return nil, err
	}
	for _, a := range all {
		if a.GetId() == id {
			return a, nil
		}
	}
	return nil, nil
}

// Generator returns the generator configuration.
func (d *Datacore) Generator(ctx context.Context) (*adlv1.GeneratorConfig, error) {
	ctx, cancel := callCtx(ctx)
	defer cancel()
	r, err := d.Admin.GetGenerator(ctx, &adlv1.GetGeneratorRequest{})
	if err != nil {
		return nil, d.wrap("admin API GetGenerator", err)
	}
	return r.GetConfig(), nil
}

// SetGenerator applies cfg and returns a function that restores the previous
// configuration.
func (d *Datacore) SetGenerator(ctx context.Context, cfg *adlv1.GeneratorConfig) (restore func(context.Context) error, err error) {
	prev, err := d.Generator(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := d.applyGenerator(ctx, cfg); err != nil {
		return nil, err
	}
	prev = proto.Clone(prev).(*adlv1.GeneratorConfig)
	return func(ctx context.Context) error {
		_, err := d.applyGenerator(ctx, prev)
		return err
	}, nil
}

func (d *Datacore) applyGenerator(ctx context.Context, cfg *adlv1.GeneratorConfig) (*adlv1.GeneratorConfig, error) {
	ctx, cancel := callCtx(ctx)
	defer cancel()
	r, err := d.Admin.SetGenerator(ctx, &adlv1.SetGeneratorRequest{Config: cfg})
	if err != nil {
		return nil, d.wrap("admin API SetGenerator", err)
	}
	return r.GetConfig(), nil
}

// PauseGenerator pauses the generator, keeping its rate, and returns a
// function that restores the previous configuration.
func (d *Datacore) PauseGenerator(ctx context.Context) (restore func(context.Context) error, err error) {
	cur, err := d.Generator(ctx)
	if err != nil {
		return nil, err
	}
	next := proto.Clone(cur).(*adlv1.GeneratorConfig)
	next.Paused = true
	return d.SetGenerator(ctx, next)
}

// Chaos returns the chaos configuration.
func (d *Datacore) Chaos(ctx context.Context) (*adlv1.ChaosConfig, error) {
	ctx, cancel := callCtx(ctx)
	defer cancel()
	r, err := d.Admin.GetChaos(ctx, &adlv1.GetChaosRequest{})
	if err != nil {
		return nil, d.wrap("admin API GetChaos", err)
	}
	return r.GetConfig(), nil
}

// SetChaos applies cfg and returns a function that restores the previous
// configuration.
func (d *Datacore) SetChaos(ctx context.Context, cfg *adlv1.ChaosConfig) (restore func(context.Context) error, err error) {
	prev, err := d.Chaos(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := d.applyChaos(ctx, cfg); err != nil {
		return nil, err
	}
	prev = proto.Clone(prev).(*adlv1.ChaosConfig)
	return func(ctx context.Context) error {
		_, err := d.applyChaos(ctx, prev)
		return err
	}, nil
}

func (d *Datacore) applyChaos(ctx context.Context, cfg *adlv1.ChaosConfig) (*adlv1.ChaosConfig, error) {
	ctx, cancel := callCtx(ctx)
	defer cancel()
	r, err := d.Admin.SetChaos(ctx, &adlv1.SetChaosRequest{Config: cfg})
	if err != nil {
		return nil, d.wrap("admin API SetChaos", err)
	}
	return r.GetConfig(), nil
}

// Burst applies count generator changes synchronously. Large bursts can take
// a while, so it uses ctx as is.
func (d *Datacore) Burst(ctx context.Context, count int32) (int32, error) {
	r, err := d.Admin.Burst(ctx, &adlv1.BurstRequest{Count: count})
	if err != nil {
		return 0, d.wrap("admin API Burst", err)
	}
	return r.GetApplied(), nil
}

// NewAssetRequest builds a valid upsert for a reserved test asset: hostname
// lower(id)+".acme.local", the kind of the prefix, a pool OS, and the site.
func NewAssetRequest(id, site, ownerUserID string) *adlv1.UpsertAssetRequest {
	osName := "Windows 11"
	if strings.HasPrefix(id, "SV-") {
		osName = "Ubuntu 24.04"
	}
	return &adlv1.UpsertAssetRequest{
		Id:          id,
		Hostname:    strings.ToLower(id) + ".acme.local",
		Kind:        AssetKindForID(id),
		Os:          osName,
		Site:        site,
		OwnerUserId: ownerUserID,
	}
}

// AssetRequestFrom builds an upsert that keeps every field of a.
func AssetRequestFrom(a *adlv1.Asset) *adlv1.UpsertAssetRequest {
	return &adlv1.UpsertAssetRequest{
		Id: a.GetId(), Hostname: a.GetHostname(), Kind: a.GetKind(),
		Os: a.GetOs(), Site: a.GetSite(), OwnerUserId: a.GetOwnerUserId(),
	}
}

// NewUserRequest builds a valid upsert for a reserved test user.
func NewUserRequest(id, displayName, groupID string) *adlv1.UpsertUserRequest {
	return &adlv1.UpsertUserRequest{
		Id:          id,
		DisplayName: displayName,
		Email:       strings.ToLower(id) + "@acme.example",
		GroupId:     groupID,
	}
}

// NewGroupRequest builds a valid upsert for a reserved test group.
func NewGroupRequest(id, name string) *adlv1.UpsertGroupRequest {
	return &adlv1.UpsertGroupRequest{Id: id, Name: name}
}
