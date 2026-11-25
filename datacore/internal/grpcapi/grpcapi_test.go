package grpcapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	"github.com/cristianrisueo/asset-discovery-lab/datacore/internal/chaos"
	"github.com/cristianrisueo/asset-discovery-lab/datacore/internal/generator"
	"github.com/cristianrisueo/asset-discovery-lab/datacore/internal/model"
	"github.com/cristianrisueo/asset-discovery-lab/datacore/internal/store"
	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
)

var smallSeed = model.SeedCounts{Groups: 8, Users: 150, Assets: 100}

type env struct {
	svc   *Services
	store *store.Store
	chaos *chaos.Chaos
}

func newEnv(t *testing.T) env {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "datacore.db"), "triggers")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.Seed(ctx, 42, smallSeed); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	ch, _ := chaos.New(&adlv1.ChaosConfig{StaleWindowMs: 300}, rand.New(rand.NewPCG(5, 6)))
	gen, err := generator.New(st, smallSeed, &adlv1.GeneratorConfig{RatePerSecond: 10, Paused: true}, rand.New(rand.NewPCG(7, 8)), log)
	if err != nil {
		t.Fatal(err)
	}
	return env{svc: New(Deps{Store: st, Chaos: ch, Generator: gen, Log: log}), store: st, chaos: ch}
}

func code(err error) codes.Code { return status.Code(err) }

func asset(id, owner string) *adlv1.UpsertAssetRequest {
	return &adlv1.UpsertAssetRequest{Id: id, Hostname: "h", Kind: model.KindForID(id), Os: "Windows 11", Site: "Madrid", OwnerUserId: owner}
}

func TestUpsertValidationAndReferences(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	cases := []struct {
		name string
		call func() error
		want codes.Code
	}{
		{"valid asset", func() error { _, err := e.svc.UpsertAsset(ctx, asset("LT-9001", "U-0117")); return err }, codes.OK},
		{"bad asset id", func() error { _, err := e.svc.UpsertAsset(ctx, asset("LT-91", "")); return err }, codes.InvalidArgument},
		{"kind mismatches prefix", func() error {
			r := asset("LT-9002", "")
			r.Kind = adlv1.AssetKind_ASSET_KIND_SERVER
			_, err := e.svc.UpsertAsset(ctx, r)
			return err
		}, codes.InvalidArgument},
		{"empty hostname", func() error {
			r := asset("LT-9003", "")
			r.Hostname = ""
			_, err := e.svc.UpsertAsset(ctx, r)
			return err
		}, codes.InvalidArgument},
		{"missing owner", func() error { _, err := e.svc.UpsertAsset(ctx, asset("LT-9004", "U-9999")); return err }, codes.FailedPrecondition},
		{"never-created id", func() error { _, err := e.svc.UpsertAsset(ctx, asset("LT-9999", "")); return err }, codes.InvalidArgument},
		{"user with missing group", func() error {
			_, err := e.svc.UpsertUser(ctx, &adlv1.UpsertUserRequest{Id: "U-9001", DisplayName: "T", Email: "t@x", GroupId: "G-99"})
			return err
		}, codes.FailedPrecondition},
		{"user without name", func() error {
			_, err := e.svc.UpsertUser(ctx, &adlv1.UpsertUserRequest{Id: "U-9001", Email: "t@x"})
			return err
		}, codes.InvalidArgument},
		{"group", func() error {
			_, err := e.svc.UpsertGroup(ctx, &adlv1.UpsertGroupRequest{Id: "G-90", Name: "QA"})
			return err
		}, codes.OK},
		{"group without name", func() error { _, err := e.svc.UpsertGroup(ctx, &adlv1.UpsertGroupRequest{Id: "G-91"}); return err }, codes.InvalidArgument},
		{"user in new group", func() error {
			_, err := e.svc.UpsertUser(ctx, &adlv1.UpsertUserRequest{Id: "U-9001", DisplayName: "T", Email: "t@x", GroupId: "G-90"})
			return err
		}, codes.OK},
	}
	for _, c := range cases {
		if got := code(c.call()); got != c.want {
			t.Errorf("%s: code %s, want %s", c.name, got, c.want)
		}
	}
}

func TestEveryUpsertCreatesANewVersion(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	var last int64
	for i := range 3 {
		r, err := e.svc.UpsertAsset(ctx, asset("LT-9001", ""))
		if err != nil {
			t.Fatal(err)
		}
		if v := r.GetAsset().GetVersion(); v != int64(i+1) || v <= last {
			t.Fatalf("upsert %d gave version %d", i, v)
		}
		last = r.GetAsset().GetVersion()
	}
	got, err := e.svc.GetAsset(ctx, &adlv1.GetAssetRequest{Id: "LT-9001"})
	if err != nil {
		t.Fatal(err)
	}
	if got.GetAsset().GetVersion() != last {
		t.Fatalf("GetAsset version %d, want %d", got.GetAsset().GetVersion(), last)
	}
}

func TestGetCodesAndCalls(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.svc.GetAsset(ctx, &adlv1.GetAssetRequest{Id: "bad"}); code(err) != codes.InvalidArgument {
		t.Fatalf("bad id: %v", err)
	}
	if _, err := e.svc.GetAsset(ctx, &adlv1.GetAssetRequest{Id: "LT-9999"}); code(err) != codes.NotFound {
		t.Fatalf("unknown id: %v", err)
	}
	if _, err := e.svc.GetUser(ctx, &adlv1.GetUserRequest{Id: "U-0117"}); err != nil {
		t.Fatal(err)
	}
	g, err := e.svc.GetGroup(ctx, &adlv1.GetGroupRequest{Id: "G-03"})
	if err != nil || g.GetGroup().GetName() != "Ventas" {
		t.Fatalf("G-03 = %v, %v", g, err)
	}
	if _, err := e.svc.GetGroup(ctx, &adlv1.GetGroupRequest{Id: "G-3"}); code(err) != codes.InvalidArgument {
		t.Fatalf("bad group id: %v", err)
	}
	st, err := e.svc.GetStats(ctx, &adlv1.GetStatsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if st.GetGetCalls() != 5 {
		t.Fatalf("get_calls %d, want 5 (failed calls count too)", st.GetGetCalls())
	}
}

func TestChaosAppliesOnlyToDataCoreService(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.chaos.Set(&adlv1.ChaosConfig{UnavailableRate: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.GetAsset(ctx, &adlv1.GetAssetRequest{Id: "bad"}); code(err) != codes.Unavailable {
		t.Fatalf("unavailable chaos must win before validation: %v", err)
	}
	if _, err := e.svc.UpsertAsset(ctx, asset("LT-9001", "")); err != nil {
		t.Fatalf("chaos reached AdminService: %v", err)
	}

	// Stale read: the previous state within the window.
	if _, err := e.chaos.Set(&adlv1.ChaosConfig{StaleReadRate: 1, StaleWindowMs: 10000}); err != nil {
		t.Fatal(err)
	}
	r2, err := e.svc.UpsertAsset(ctx, asset("LT-9001", ""))
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.svc.GetAsset(ctx, &adlv1.GetAssetRequest{Id: "LT-9001"})
	if err != nil {
		t.Fatal(err)
	}
	if got.GetAsset().GetVersion() != r2.GetAsset().GetVersion()-1 {
		t.Fatalf("stale read returned v%d, want v%d", got.GetAsset().GetVersion(), r2.GetAsset().GetVersion()-1)
	}
	// A seeded entity has no previous state: never stale.
	if a, err := e.svc.GetAsset(ctx, &adlv1.GetAssetRequest{Id: "LT-0042"}); err != nil || a.GetAsset().GetVersion() != 1 {
		t.Fatalf("LT-0042 = %v, %v", a, err)
	}
}

func TestSetRangesBurstAndStats(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.svc.SetGenerator(ctx, &adlv1.SetGeneratorRequest{Config: &adlv1.GeneratorConfig{RatePerSecond: 600}}); code(err) != codes.InvalidArgument {
		t.Fatalf("rate 600: %v", err)
	}
	if _, err := e.svc.SetGenerator(ctx, &adlv1.SetGeneratorRequest{}); code(err) != codes.InvalidArgument {
		t.Fatalf("missing generator config: %v", err)
	}
	r, err := e.svc.SetGenerator(ctx, &adlv1.SetGeneratorRequest{Config: &adlv1.GeneratorConfig{RatePerSecond: 20, Paused: true}})
	if err != nil || r.GetConfig().GetRatePerSecond() != 20 || !r.GetConfig().GetPaused() {
		t.Fatalf("SetGenerator = %v, %v", r, err)
	}
	if _, err := e.svc.SetChaos(ctx, &adlv1.SetChaosRequest{Config: &adlv1.ChaosConfig{StaleWindowMs: 20000}}); code(err) != codes.InvalidArgument {
		t.Fatalf("window 20000: %v", err)
	}
	if _, err := e.svc.Burst(ctx, &adlv1.BurstRequest{Count: 0}); code(err) != codes.InvalidArgument {
		t.Fatalf("burst 0: %v", err)
	}
	b, err := e.svc.Burst(ctx, &adlv1.BurstRequest{Count: 25})
	if err != nil || b.GetApplied() != 25 {
		t.Fatalf("Burst = %v, %v", b, err)
	}
	st, err := e.svc.GetStats(ctx, &adlv1.GetStatsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	want := &adlv1.GetStatsResponse{ChangesApplied: 25, OutboxPending: 258 + 25, Assets: 100, Users: 150, Groups: 8}
	if !proto.Equal(st, want) {
		t.Fatalf("stats %v, want %v", st, want)
	}
}

func TestListStreamsOrderedByIDOverGRPC(t *testing.T) {
	e := newEnv(t)
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	adlv1.RegisterAdminServiceServer(srv, e.svc)
	adlv1.RegisterDataCoreServiceServer(srv, e.svc)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream, err := adlv1.NewAdminServiceClient(conn).ListAssets(ctx, &adlv1.ListAssetsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for {
		r, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, r.GetAsset().GetId())
	}
	if len(ids) != 100 {
		t.Fatalf("%d assets streamed, want 100", len(ids))
	}
	for i := 1; i < len(ids); i++ {
		if ids[i-1] >= ids[i] {
			t.Fatalf("not ordered by id: %s then %s", ids[i-1], ids[i])
		}
	}
	if _, err := adlv1.NewDataCoreServiceClient(conn).GetAsset(ctx, &adlv1.GetAssetRequest{Id: "LT-9999"}); status.Code(err) != codes.NotFound {
		t.Fatalf("GetAsset LT-9999 over gRPC: %v", err)
	}
}
