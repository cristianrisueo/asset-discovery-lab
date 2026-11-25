// Package fetcher is the adapter's repository for datacore: it calls
// DataCoreService.Get* with a deadline per attempt and applies the retry
// policy of spec §9.4.
package fetcher

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"github.com/cristianrisueo/asset-discovery-lab/adapter/internal/contract"
	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
)

// Client is the part of the generated DataCoreService client the fetcher uses.
type Client interface {
	GetAsset(ctx context.Context, in *adlv1.GetAssetRequest, opts ...grpc.CallOption) (*adlv1.GetAssetResponse, error)
	GetUser(ctx context.Context, in *adlv1.GetUserRequest, opts ...grpc.CallOption) (*adlv1.GetUserResponse, error)
	GetGroup(ctx context.Context, in *adlv1.GetGroupRequest, opts ...grpc.CallOption) (*adlv1.GetGroupResponse, error)
}

// Result is the final outcome of a fetch.
type Result struct {
	Class    Class
	Entity   proto.Message // set on Success
	Version  int64         // the entity version on Success
	Attempts int
	Detail   string // why it failed, for the dead letter's error.message
}

// Fetcher fetches entities with retries.
type Fetcher struct {
	Client   Client
	Timeout  time.Duration // GRPC_TIMEOUT_MS, per attempt
	Backoff  *Backoff
	StaleMax time.Duration // STALE_MAX_WAIT_MS
	Tracker  *Tracker
	Log      *slog.Logger

	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error
}

func (f *Fetcher) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

func (f *Fetcher) sleep(ctx context.Context, d time.Duration) error {
	if f.Sleep != nil {
		return f.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// get runs one attempt with its own deadline.
func (f *Fetcher) get(ctx context.Context, et contract.EntityType, id string) (proto.Message, error) {
	ctx, cancel := context.WithTimeout(ctx, f.Timeout)
	defer cancel()
	switch et.Enum {
	case adlv1.EntityType_ENTITY_TYPE_ASSET:
		r, err := f.Client.GetAsset(ctx, &adlv1.GetAssetRequest{Id: id})
		return r.GetAsset(), err
	case adlv1.EntityType_ENTITY_TYPE_USER:
		r, err := f.Client.GetUser(ctx, &adlv1.GetUserRequest{Id: id})
		return r.GetUser(), err
	default:
		r, err := f.Client.GetGroup(ctx, &adlv1.GetGroupRequest{Id: id})
		return r.GetGroup(), err
	}
}

// Fetch gets the entity a trigger announced, retrying as spec §9.4 says. It
// returns when the outcome is final, or Aborted when ctx ends. key and
// partition identify the trigger for logs and for the readiness tracker.
func (f *Fetcher) Fetch(ctx context.Context, et contract.EntityType, id string, triggerVersion int64, key string, partition int32) Result {
	var firstStale, firstTransient time.Time
	defer f.Tracker.Clear(partition)
	for attempt := 1; ; attempt++ {
		m, err := f.get(ctx, et, id)
		if ctx.Err() != nil {
			return Result{Class: Aborted, Attempts: attempt, Detail: ctx.Err().Error()}
		}
		var (
			class   Class
			version int64
			detail  string
		)
		if err != nil {
			class, detail = ClassifyError(err), err.Error()
		} else {
			var verr error
			version, verr = contract.ValidateEntity(id, m)
			class = ClassifyOK(version, triggerVersion, verr)
			if verr != nil {
				detail = verr.Error()
			}
		}

		delay := f.Backoff.Delay(attempt)
		switch class {
		case Success:
			return Result{Class: Success, Entity: m, Version: version, Attempts: attempt}
		case Stale:
			now := f.now()
			if firstStale.IsZero() {
				firstStale = now
			}
			waited := now.Sub(firstStale)
			if waited >= f.StaleMax {
				return Result{Class: StaleTimeout, Attempts: attempt,
					Detail: staleDetail(version, triggerVersion, waited)}
			}
			delay = min(delay, f.StaleMax-waited)
		case Transient:
			if firstTransient.IsZero() {
				firstTransient = f.now()
			}
			f.Tracker.Mark(partition, firstTransient)
		default:
			return Result{Class: class, Attempts: attempt, Detail: detail}
		}

		if attempt == f.Backoff.Fast {
			// The fast retries are used up: from now on the partition waits
			// longer between attempts (one log line per escalation, §9.8).
			f.Log.Warn("retry escalation", "key", key, "version", triggerVersion,
				"outcome", class.String(), "attempt", attempt, "next_delay_ms", f.Backoff.Nominal(attempt+1).Milliseconds(), "detail", detail)
		} else {
			f.Log.Debug("retry", "key", key, "version", triggerVersion, "outcome", class.String(),
				"attempt", attempt, "delay_ms", delay.Milliseconds(), "detail", detail)
		}
		if err := f.sleep(ctx, delay); err != nil {
			return Result{Class: Aborted, Attempts: attempt, Detail: err.Error()}
		}
	}
}

func staleDetail(got, want int64, waited time.Duration) string {
	return fmt.Sprintf("datacore still returned version %d < trigger version %d after %s",
		got, want, waited.Round(time.Millisecond))
}

// Tracker records, per partition, since when it has been in transient retry.
// The adapter's /readyz reports 503 once any partition passes 30 s (§9.4).
type Tracker struct {
	mu    sync.Mutex
	since map[int32]time.Time
}

// NewTracker returns an empty tracker.
func NewTracker() *Tracker { return &Tracker{since: map[int32]time.Time{}} }

// Mark records that partition has been retrying transient errors since since.
func (t *Tracker) Mark(partition int32, since time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.since[partition] = since
}

// Clear records that partition is no longer in transient retry.
func (t *Tracker) Clear(partition int32) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.since, partition)
}

// Stalled returns the partitions in transient retry for longer than limit.
func (t *Tracker) Stalled(now time.Time, limit time.Duration) []int32 {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []int32
	for p, s := range t.since {
		if now.Sub(s) > limit {
			out = append(out, p)
		}
	}
	return out
}
