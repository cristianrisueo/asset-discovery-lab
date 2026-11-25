// Package outbox is datacore's publisher: the outbox relay of spec §7.5. It
// produces pending triggers to Kafka in seq order and deletes them only after
// every record of the batch is acknowledged.
package outbox

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/cristianrisueo/asset-discovery-lab/datacore/internal/chaos"
	"github.com/cristianrisueo/asset-discovery-lab/datacore/internal/store"
)

// Producer is the part of *kgo.Client the relay uses.
type Producer interface {
	ProduceSync(ctx context.Context, rs ...*kgo.Record) kgo.ProduceResults
	Ping(ctx context.Context) error
}

// Outbox is the part of *store.Store the relay uses.
type Outbox interface {
	OutboxBatch(ctx context.Context, limit int) ([]store.OutboxRow, error)
	DeleteOutboxThrough(ctx context.Context, seq int64) error
}

// Relay publishes outbox rows.
type Relay struct {
	out       Outbox
	prod      Producer
	chaos     *chaos.Chaos
	log       *slog.Logger
	batch     int
	poll      time.Duration
	now       func() time.Time
	published atomic.Int64
	connected atomic.Bool

	// produceTimeout bounds one batch. The produce is detached from
	// cancellation, so a batch in progress finishes on shutdown (spec §7.11).
	produceTimeout time.Duration
	backoffMin     time.Duration
	backoffMax     time.Duration
}

// New returns a relay that reads up to batch rows at a time and polls every
// poll when the outbox is empty (spec §7.10).
func New(out Outbox, prod Producer, ch *chaos.Chaos, log *slog.Logger, batch int, poll time.Duration) *Relay {
	return &Relay{
		out: out, prod: prod, chaos: ch, log: log, batch: batch, poll: poll, now: time.Now,
		produceTimeout: 60 * time.Second, backoffMin: 200 * time.Millisecond, backoffMax: 5 * time.Second,
	}
}

// Published counts the real triggers acknowledged by Kafka since start
// (chaos extras are not counted).
func (r *Relay) Published() int64 { return r.published.Load() }

// Connected reports whether the relay has reached Kafka (spec §7.11 readyz).
func (r *Relay) Connected() bool { return r.connected.Load() }

// Records builds the Kafka records of a batch: every real trigger in seq
// order, each followed by the chaos extras it draws (spec §7.9). Chaos only
// adds records; it never replaces or drops a real trigger.
func (r *Relay) Records(rows []store.OutboxRow) []*kgo.Record {
	recs := make([]*kgo.Record, 0, len(rows))
	now := r.now().UTC().Truncate(time.Millisecond)
	for _, row := range rows {
		recs = append(recs, &kgo.Record{Topic: row.Topic, Key: []byte(row.Key), Value: row.Value})
		if r.chaos.Duplicate() {
			recs = append(recs, &kgo.Record{Topic: row.Topic, Key: []byte(row.Key), Value: row.Value})
		}
		if r.chaos.Malformed() {
			recs = append(recs, &kgo.Record{Topic: row.Topic, Key: []byte(chaos.MalformedKey(row.Seq)), Value: r.chaos.MalformedValue(row.Seq, now)})
		}
		if r.chaos.Phantom() {
			k, v := r.chaos.PhantomTrigger(now)
			recs = append(recs, &kgo.Record{Topic: row.Topic, Key: []byte(k), Value: v})
		}
	}
	return recs
}

// Run publishes until ctx ends. It stops only between batches.
func (r *Relay) Run(ctx context.Context) {
	backoff := r.backoffMin
	fail := func(msg string, err error) {
		r.log.Warn(msg, "error", err.Error(), "retry_in", backoff.String())
		sleep(ctx, backoff)
		backoff = min(backoff*2, r.backoffMax)
	}
	for ctx.Err() == nil {
		if !r.connected.Load() {
			pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := r.prod.Ping(pctx)
			cancel()
			if err != nil {
				fail("relay cannot reach Kafka", err)
				continue
			}
			r.connected.Store(true)
			r.log.Info("relay connected to Kafka")
		}
		published, err := r.Once(ctx)
		if err != nil {
			fail("relay batch failed; rows stay in the outbox", err)
			continue
		}
		backoff = r.backoffMin
		if published == 0 {
			sleep(ctx, r.poll)
		}
	}
}

// Once publishes one batch and returns how many outbox rows it published.
// On any error the rows stay in the outbox and are retried later; a partial
// success republishes some records, which is a harmless duplicate (§7.5).
func (r *Relay) Once(ctx context.Context) (int, error) {
	rows, err := r.out.OutboxBatch(ctx, r.batch)
	if err != nil || len(rows) == 0 {
		return 0, err
	}
	pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.produceTimeout)
	defer cancel()
	if err := r.prod.ProduceSync(pctx, r.Records(rows)...).FirstErr(); err != nil {
		return 0, err
	}
	if err := r.out.DeleteOutboxThrough(ctx, rows[len(rows)-1].Seq); err != nil {
		return 0, err
	}
	r.published.Add(int64(len(rows)))
	return len(rows), nil
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
