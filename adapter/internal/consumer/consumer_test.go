package consumer

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

// ack is a Pending resolved by the test.
type ack struct {
	done chan struct{}
	err  error
}

func newAck() *ack { return &ack{done: make(chan struct{})} }

func (a *ack) resolve(err error) { a.err = err; close(a.done) }

func (a *ack) Wait(ctx context.Context) error {
	select {
	case <-a.done:
		return a.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// fakeClient serves one batch, then blocks until the context ends.
type fakeClient struct {
	mu        sync.Mutex
	batch     kgo.Fetches
	served    bool
	committed []*kgo.Record
	commitCh  chan struct{}
	allowed   int
}

func (f *fakeClient) PollFetches(ctx context.Context) kgo.Fetches {
	f.mu.Lock()
	if !f.served {
		f.served = true
		f.mu.Unlock()
		return f.batch
	}
	f.mu.Unlock()
	<-ctx.Done()
	return kgo.NewErrFetch(ctx.Err())
}

func (f *fakeClient) CommitRecords(_ context.Context, rs ...*kgo.Record) error {
	f.mu.Lock()
	f.committed = append(f.committed, rs...)
	f.mu.Unlock()
	close(f.commitCh)
	return nil
}

func (f *fakeClient) AllowRebalance() { f.mu.Lock(); f.allowed++; f.mu.Unlock() }

func batch(perPartition map[int32]int) (kgo.Fetches, map[int32][]*kgo.Record) {
	byPart := map[int32][]*kgo.Record{}
	var parts []kgo.FetchPartition
	for p, n := range perPartition {
		var recs []*kgo.Record
		for o := range n {
			recs = append(recs, &kgo.Record{Topic: "triggers", Partition: p, Offset: int64(o)})
		}
		byPart[p] = recs
		parts = append(parts, kgo.FetchPartition{Partition: p, Records: recs})
	}
	return kgo.Fetches{{Topics: []kgo.FetchTopic{{Topic: "triggers", Partitions: parts}}}}, byPart
}

func quiet() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

func TestCommitOnlyAfterEveryPublishIsAcknowledged(t *testing.T) {
	fetches, byPart := batch(map[int32]int{0: 3, 1: 2})
	client := &fakeClient{batch: fetches, commitCh: make(chan struct{})}
	var mu sync.Mutex
	acks := map[*kgo.Record]*ack{}
	order := map[int32][]int64{}
	handled := make(chan struct{}, 10)
	c := &Consumer{Client: client, Log: quiet(), Grace: time.Second, Handle: func(_ context.Context, r *kgo.Record) Pending {
		a := newAck()
		mu.Lock()
		acks[r] = a
		order[r.Partition] = append(order[r.Partition], r.Offset)
		mu.Unlock()
		handled <- struct{}{}
		return a
	}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()

	for range 5 {
		<-handled
	}
	// Every record is handled but no produce is acknowledged: no commit.
	select {
	case <-client.commitCh:
		t.Fatal("offsets committed before the publishes were acknowledged")
	case <-time.After(100 * time.Millisecond):
	}
	mu.Lock()
	for p, offs := range order {
		for i, o := range offs {
			if o != int64(i) {
				t.Fatalf("partition %d handled out of order: %v", p, offs)
			}
		}
	}
	// Acknowledge all but the last record of partition 0.
	for r, a := range acks {
		if r.Partition != 0 || r.Offset != 2 {
			a.resolve(nil)
		}
	}
	mu.Unlock()
	select {
	case <-client.commitCh:
		t.Fatal("offsets committed while one publish was still unacknowledged")
	case <-time.After(100 * time.Millisecond):
	}
	mu.Lock()
	acks[byPart[0][2]].resolve(nil)
	mu.Unlock()
	select {
	case <-client.commitCh:
	case <-time.After(2 * time.Second):
		t.Fatal("no commit after every publish was acknowledged")
	}
	client.mu.Lock()
	got := map[int32]int64{}
	for _, r := range client.committed {
		got[r.Partition] = r.Offset
	}
	client.mu.Unlock()
	if got[0] != 2 || got[1] != 1 || len(got) != 2 {
		t.Fatalf("committed %v, want the last record of each partition", got)
	}
	cancel()
	<-done
	if client.allowed < 1 {
		t.Fatal("rebalances were never allowed again after the batch")
	}
}

func TestFailedAckCommitsOnlyThePrefix(t *testing.T) {
	fetches, _ := batch(map[int32]int{0: 4})
	client := &fakeClient{batch: fetches, commitCh: make(chan struct{})}
	c := &Consumer{Client: client, Log: quiet(), Grace: time.Second, Handle: func(_ context.Context, r *kgo.Record) Pending {
		a := newAck()
		if r.Offset == 2 {
			a.resolve(errors.New("aborted"))
		} else {
			a.resolve(nil)
		}
		return a
	}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	<-client.commitCh
	cancel()
	<-done
	if len(client.committed) != 1 || client.committed[0].Offset != 1 {
		t.Fatalf("committed %v, want only through offset 1", client.committed)
	}
}

func TestShutdownWithUnacknowledgedRecordsCommitsNothing(t *testing.T) {
	fetches, _ := batch(map[int32]int{0: 2})
	client := &fakeClient{batch: fetches, commitCh: make(chan struct{})}
	started := make(chan struct{}, 2)
	c := &Consumer{Client: client, Log: quiet(), Grace: 50 * time.Millisecond, Handle: func(context.Context, *kgo.Record) Pending {
		started <- struct{}{}
		return newAck() // never acknowledged
	}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	<-started
	<-started
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after the grace period")
	}
	if len(client.committed) != 0 {
		t.Fatalf("committed %v on shutdown, want nothing", client.committed)
	}
}
