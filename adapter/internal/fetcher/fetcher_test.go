package fetcher

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/cristianrisueo/asset-discovery-lab/adapter/internal/contract"
	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
)

func TestClassifyEveryOutcome(t *testing.T) {
	errs := []struct {
		name string
		err  error
		want Class
	}{
		{"NOT_FOUND", status.Error(codes.NotFound, "x"), NotFound},
		{"INVALID_ARGUMENT", status.Error(codes.InvalidArgument, "x"), InvalidArgument},
		{"UNAVAILABLE", status.Error(codes.Unavailable, "x"), Transient},
		{"DEADLINE_EXCEEDED", status.Error(codes.DeadlineExceeded, "x"), Transient},
		{"RESOURCE_EXHAUSTED", status.Error(codes.ResourceExhausted, "x"), Transient},
		{"ABORTED", status.Error(codes.Aborted, "x"), Transient},
		{"INTERNAL", status.Error(codes.Internal, "x"), Transient},
		{"UNKNOWN", status.Error(codes.Unknown, "x"), Transient},
		{"connection error", errors.New("dial tcp: connection refused"), Transient},
		{"context deadline", context.DeadlineExceeded, Transient},
		{"unlisted code", status.Error(codes.PermissionDenied, "x"), Transient},
	}
	for _, tt := range errs {
		if got := ClassifyError(tt.err); got != tt.want {
			t.Errorf("%s: %s, want %s", tt.name, got, tt.want)
		}
	}
	oks := []struct {
		name          string
		got, trigger  int64
		validationErr error
		want          Class
	}{
		{"same version", 7, 7, nil, Success},
		{"newer version", 9, 7, nil, Success},
		{"older version", 6, 7, nil, Stale},
		{"invalid entity", 7, 7, errors.New("bad"), InvalidEntity},
		{"invalid and older", 6, 7, errors.New("bad"), InvalidEntity},
	}
	for _, tt := range oks {
		if got := ClassifyOK(tt.got, tt.trigger, tt.validationErr); got != tt.want {
			t.Errorf("%s: %s, want %s", tt.name, got, tt.want)
		}
	}
	for c, want := range map[Class]bool{Success: false, Stale: false, Transient: false, Aborted: false,
		InvalidEntity: true, NotFound: true, InvalidArgument: true, StaleTimeout: true} {
		if c.Permanent() != want {
			t.Errorf("%s.Permanent() = %v", c, !want)
		}
	}
}

func TestBackoffTimings(t *testing.T) {
	b := NewBackoff(3, 100*time.Millisecond, 5*time.Second, rand.New(rand.NewPCG(1, 1)))
	nominal := []time.Duration{100, 200, 400, 800, 1600, 3200, 5000, 5000, 5000}
	for i, want := range nominal {
		n := i + 1
		if got := b.Nominal(n); got != want*time.Millisecond {
			t.Errorf("Nominal(%d) = %v, want %v", n, got, want*time.Millisecond)
		}
		for range 200 {
			d := b.Delay(n)
			w := want * time.Millisecond
			if n <= 3 {
				if d < w*8/10 || d > w*12/10 {
					t.Fatalf("fast retry %d: %v outside %v ±20%%", n, d, w)
				}
			} else if d != w {
				t.Fatalf("retry %d: %v, want exactly %v", n, d, w)
			}
		}
	}
	// The jitter really varies the fast retries.
	seen := map[time.Duration]bool{}
	for range 50 {
		seen[b.Delay(1)] = true
	}
	if len(seen) < 10 {
		t.Fatalf("fast retries barely vary: %d distinct delays", len(seen))
	}
}

// fakeClient answers GetAsset from a script of results.
type fakeClient struct {
	script []func() (*adlv1.Asset, error)
	calls  int
}

func (f *fakeClient) GetAsset(context.Context, *adlv1.GetAssetRequest, ...grpc.CallOption) (*adlv1.GetAssetResponse, error) {
	step := f.script[min(f.calls, len(f.script)-1)]
	f.calls++
	a, err := step()
	if err != nil {
		return nil, err
	}
	return &adlv1.GetAssetResponse{Asset: a}, nil
}

func (f *fakeClient) GetUser(context.Context, *adlv1.GetUserRequest, ...grpc.CallOption) (*adlv1.GetUserResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (f *fakeClient) GetGroup(context.Context, *adlv1.GetGroupRequest, ...grpc.CallOption) (*adlv1.GetGroupResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func assetAt(v int64) func() (*adlv1.Asset, error) {
	return func() (*adlv1.Asset, error) {
		return &adlv1.Asset{Id: "LT-9007", Hostname: "h", Kind: adlv1.AssetKind_ASSET_KIND_LAPTOP, Os: "o", Site: "s", Version: v, ChangedAt: timestamppb.Now()}, nil
	}
}

func failWith(c codes.Code) func() (*adlv1.Asset, error) {
	return func() (*adlv1.Asset, error) { return nil, status.Error(c, "boom") }
}

// newFetcher returns a fetcher on a virtual clock: sleeping advances it.
func newFetcher(c Client) (*Fetcher, *[]time.Duration) {
	clock := time.Unix(0, 0)
	var slept []time.Duration
	return &Fetcher{
		Client:   c,
		Timeout:  500 * time.Millisecond,
		Backoff:  NewBackoff(3, 100*time.Millisecond, 5*time.Second, rand.New(rand.NewPCG(2, 2))),
		StaleMax: 10 * time.Second,
		Tracker:  NewTracker(),
		Log:      slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Now:      func() time.Time { return clock },
		Sleep: func(ctx context.Context, d time.Duration) error {
			slept = append(slept, d)
			clock = clock.Add(d)
			return ctx.Err()
		},
	}, &slept
}

func TestFetchFlows(t *testing.T) {
	type step = func() (*adlv1.Asset, error)
	tests := []struct {
		name     string
		script   []step
		want     Class
		attempts int
	}{
		{"success", []step{assetAt(7)}, Success, 1},
		{"newer than trigger", []step{assetAt(9)}, Success, 1},
		{"stale then fresh", []step{assetAt(6), assetAt(6), assetAt(7)}, Success, 3},
		{"transient then fresh", []step{failWith(codes.Unavailable), failWith(codes.DeadlineExceeded), assetAt(7)}, Success, 3},
		{"many transient then fresh", append(repeat(failWith(codes.Unavailable), 12), assetAt(7)), Success, 13},
		{"not found", []step{failWith(codes.NotFound)}, NotFound, 1},
		{"invalid argument", []step{failWith(codes.InvalidArgument)}, InvalidArgument, 1},
		{"invalid entity", []step{func() (*adlv1.Asset, error) { a, _ := assetAt(7)(); a.Hostname = ""; return a, nil }}, InvalidEntity, 1},
		{"transient then not found", []step{failWith(codes.Unavailable), failWith(codes.NotFound)}, NotFound, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, _ := newFetcher(&fakeClient{script: tt.script})
			r := f.Fetch(context.Background(), contract.Asset, "LT-9007", 7, "asset:LT-9007", 0)
			if r.Class != tt.want || r.Attempts != tt.attempts {
				t.Fatalf("Fetch = %s after %d attempts, want %s after %d (%s)", r.Class, r.Attempts, tt.want, tt.attempts, r.Detail)
			}
			if r.Class == Success && r.Entity == nil {
				t.Fatal("success without an entity")
			}
		})
	}
}

func repeat(s func() (*adlv1.Asset, error), n int) []func() (*adlv1.Asset, error) {
	out := make([]func() (*adlv1.Asset, error), n)
	for i := range out {
		out[i] = s
	}
	return out
}

func TestStaleTimesOutAfterStaleMax(t *testing.T) {
	f, slept := newFetcher(&fakeClient{script: []func() (*adlv1.Asset, error){assetAt(6)}})
	r := f.Fetch(context.Background(), contract.Asset, "LT-9007", 7, "asset:LT-9007", 0)
	if r.Class != StaleTimeout {
		t.Fatalf("Fetch = %s, want stale_timeout", r.Class)
	}
	var total time.Duration
	for _, d := range *slept {
		total += d
	}
	if total != 10*time.Second {
		t.Fatalf("waited %v before STALE_VERSION, want exactly STALE_MAX_WAIT_MS (10 s)", total)
	}
}

func TestTransientNeverDeadLettersAndMarksTheTracker(t *testing.T) {
	f, _ := newFetcher(nil)
	calls := 0
	var stalledDuringRetries []int32
	f.Client = &fakeClient{script: []func() (*adlv1.Asset, error){func() (*adlv1.Asset, error) {
		calls++
		if calls == 40 {
			stalledDuringRetries = f.Tracker.Stalled(f.Now(), 30*time.Second)
			return assetAt(7)()
		}
		return failWith(codes.Unavailable)()
	}}}
	r := f.Fetch(context.Background(), contract.Asset, "LT-9007", 7, "asset:LT-9007", 2)
	if r.Class != Success || r.Attempts != 40 {
		t.Fatalf("Fetch = %s after %d attempts", r.Class, r.Attempts)
	}
	if len(stalledDuringRetries) != 1 || stalledDuringRetries[0] != 2 {
		t.Fatalf("tracker during a long transient retry: %v, want partition 2", stalledDuringRetries)
	}
	if s := f.Tracker.Stalled(f.Now(), 0); len(s) != 0 {
		t.Fatalf("tracker not cleared after success: %v", s)
	}
}

func TestFetchAbortsOnShutdown(t *testing.T) {
	f, _ := newFetcher(&fakeClient{script: []func() (*adlv1.Asset, error){failWith(codes.Unavailable)}})
	ctx, cancel := context.WithCancel(context.Background())
	n := 0
	f.Sleep = func(ctx context.Context, _ time.Duration) error {
		if n++; n == 5 {
			cancel()
		}
		return ctx.Err()
	}
	if r := f.Fetch(ctx, contract.Asset, "LT-9007", 7, "asset:LT-9007", 0); r.Class != Aborted {
		t.Fatalf("Fetch = %s, want aborted", r.Class)
	}
}
