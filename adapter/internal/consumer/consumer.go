// Package consumer is the adapter's transport: the franz-go consumer of the
// triggers topic in group adl-adapter (spec §9.2). Records of one partition
// are handled sequentially, partitions in parallel, and offsets are committed
// only after every record of the batch is published or dead-lettered and
// acknowledged (spec §9.5).
package consumer

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

// Pending is the acknowledgement of the record a trigger produced.
type Pending interface {
	Wait(ctx context.Context) error
}

// HandleFunc handles one record and returns the acknowledgement of what it
// produced. It returns only once the record is handled up to the produce.
type HandleFunc func(ctx context.Context, rec *kgo.Record) Pending

// Client is the part of *kgo.Client the consumer uses. The client is built
// with DisableAutoCommit and BlockRebalanceOnPoll.
type Client interface {
	PollFetches(ctx context.Context) kgo.Fetches
	CommitRecords(ctx context.Context, rs ...*kgo.Record) error
	AllowRebalance()
}

// Consumer polls, handles and commits.
type Consumer struct {
	Client Client
	Handle HandleFunc
	Log    *slog.Logger
	// Grace is how long a batch in progress may run after shutdown starts.
	// Records not handled by then are not committed and are handled again
	// after the restart (spec §9.5).
	Grace time.Duration
}

// ErrIncomplete reports a batch with a record that was not acknowledged
// while the adapter was not stopping. Consuming further could commit offsets
// past that record (spec §9.2), so Run stops and the process restarts from the
// last committed offset, handling the records again in order.
var ErrIncomplete = errors.New("a record of the batch was not acknowledged")

// Run consumes until ctx ends: then it stops polling, finishes the current
// batch (within Grace), commits it, and returns nil (spec §9.8 SIGTERM). It
// returns ErrIncomplete when a record fails outside shutdown. Rebalances are
// always allowed again on return, so closing the client never hangs.
func (c *Consumer) Run(ctx context.Context) error {
	work, stopWork := context.WithCancel(context.WithoutCancel(ctx))
	defer stopWork()
	defer c.Client.AllowRebalance()
	go func() {
		select {
		case <-ctx.Done():
			t := time.NewTimer(c.Grace)
			defer t.Stop()
			select {
			case <-t.C:
				stopWork()
			case <-work.Done():
			}
		case <-work.Done():
		}
	}()

	for {
		fetches := c.Client.PollFetches(ctx)
		if ctx.Err() != nil || fetches.IsClientClosed() {
			// Records returned together with the shutdown are left
			// uncommitted: they are handled again after the restart.
			return nil
		}
		fetches.EachError(func(topic string, partition int32, err error) {
			c.Log.Warn("fetch error", "topic", topic, "partition", partition, "error", err.Error())
		})
		complete := c.processBatch(work, fetches)
		if !complete && ctx.Err() == nil {
			return ErrIncomplete
		}
		c.Client.AllowRebalance()
	}
}

// processBatch handles one polled batch, commits what is safe to commit, and
// reports whether every record was acknowledged.
func (c *Consumer) processBatch(work context.Context, fetches kgo.Fetches) bool {
	var parts [][]*kgo.Record
	fetches.EachPartition(func(p kgo.FetchTopicPartition) {
		if len(p.Records) > 0 {
			parts = append(parts, p.Records)
		}
	})
	if len(parts) == 0 {
		return true
	}
	commit := make([]*kgo.Record, len(parts))
	var wg sync.WaitGroup
	for i, recs := range parts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			commit[i] = c.handlePartition(work, recs)
		}()
	}
	wg.Wait()

	var toCommit []*kgo.Record
	complete := true
	for i, r := range commit {
		if r != parts[i][len(parts[i])-1] {
			complete = false
		}
		if r != nil {
			toCommit = append(toCommit, r)
		}
	}
	if len(toCommit) == 0 {
		return complete
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(work), 10*time.Second)
	defer cancel()
	if err := c.Client.CommitRecords(cctx, toCommit...); err != nil {
		// The records are handled; they are handled again only if the
		// adapter restarts before a later commit covers them.
		c.Log.Warn("offset commit failed", "error", err.Error())
	}
	return complete
}

// handlePartition handles the records of one partition in offset order and
// returns the last record that may be committed: the end of the prefix whose
// produces were all acknowledged. It returns nil when nothing may be committed.
func (c *Consumer) handlePartition(work context.Context, recs []*kgo.Record) *kgo.Record {
	acks := make([]Pending, 0, len(recs))
	for _, r := range recs {
		if work.Err() != nil {
			break
		}
		acks = append(acks, c.Handle(work, r))
	}
	var last *kgo.Record
	for i, a := range acks {
		if err := a.Wait(work); err != nil {
			c.Log.Warn("record not acknowledged; its offset is not committed",
				"topic", recs[i].Topic, "partition", recs[i].Partition, "offset", recs[i].Offset, "error", err.Error())
			break
		}
		last = recs[i]
	}
	return last
}
