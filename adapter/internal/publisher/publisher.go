// Package publisher is the adapter's Kafka publisher: full entities to the
// topic of their type (spec §9.5) and dead letters to dlq.adapter (spec §8.3,
// §9.6). Every produce returns an Ack that resolves when the broker has
// acknowledged the record.
package publisher

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/twmb/franz-go/pkg/kgo"
)

// Dead-letter reasons (spec §8.3).
const (
	ReasonDecode       = "DECODE_ERROR"
	ReasonValidation   = "VALIDATION_ERROR"
	ReasonNotFound     = "NOT_FOUND"
	ReasonStaleVersion = "STALE_VERSION"
)

// Header names (spec §6.8, §8.3).
const (
	HeaderTriggerVersion = "trigger.version"
	HeaderErrorReason    = "error.reason"
	HeaderErrorMessage   = "error.message"
	HeaderSourceTopic    = "source.topic"
	HeaderSourcePart     = "source.partition"
	HeaderSourceOffset   = "source.offset"
	HeaderFailedAt       = "failed.at"
	HeaderService        = "service"
)

// MaxErrorMessage is the size limit of error.message: 1 KB (spec §8.3).
const MaxErrorMessage = 1024

// Producer is the part of *kgo.Client the publisher uses.
type Producer interface {
	Produce(ctx context.Context, r *kgo.Record, promise func(*kgo.Record, error))
}

// Ack resolves once a record is acknowledged, or with an error when it never
// will be (only on shutdown).
type Ack struct {
	done chan struct{}
	once sync.Once
	err  error
}

func newAck() *Ack { return &Ack{done: make(chan struct{})} }

func (a *Ack) resolve(err error) {
	a.once.Do(func() { a.err = err; close(a.done) })
}

// Failed returns an Ack that has already failed, for records that were not
// handled at all (for example a fetch aborted by shutdown).
func Failed(err error) *Ack {
	a := newAck()
	a.resolve(err)
	return a
}

// Wait blocks until the Ack resolves or ctx ends, and returns its error.
func (a *Ack) Wait(ctx context.Context) error {
	select {
	case <-a.done:
		return a.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Source identifies the record a dead letter comes from.
type Source struct {
	Topic     string
	Partition int32
	Offset    int64
	Key       []byte
	Value     []byte
}

// SourceOf returns the coordinates and payload of a consumed record.
func SourceOf(r *kgo.Record) Source {
	return Source{Topic: r.Topic, Partition: r.Partition, Offset: r.Offset, Key: r.Key, Value: r.Value}
}

// Publisher produces entity records and dead letters.
type Publisher struct {
	prod        Producer
	entityTopic func(suffix string) string
	dlqTopic    string
	log         *slog.Logger
	now         func() time.Time

	// life ends only when the process stops for good. Failed produces are
	// retried until then, so a batch never commits past an unacked record.
	life       context.Context
	retryDelay time.Duration
}

// New returns a publisher. Entity records go to prefix+suffix (for example
// entities.assets), dead letters to dlqTopic.
func New(life context.Context, prod Producer, prefix, dlqTopic string, log *slog.Logger) *Publisher {
	return &Publisher{
		prod: prod, entityTopic: func(s string) string { return prefix + s }, dlqTopic: dlqTopic,
		log: log, now: time.Now, life: life, retryDelay: 500 * time.Millisecond,
	}
}

// Entity publishes the serialized entity with key = id and the header
// trigger.version (spec §6.8, §9.5).
func (p *Publisher) Entity(topicSuffix, id string, value []byte, triggerVersion int64) *Ack {
	return p.send(&kgo.Record{
		Topic: p.entityTopic(topicSuffix),
		Key:   []byte(id),
		Value: value,
		Headers: []kgo.RecordHeader{
			{Key: HeaderTriggerVersion, Value: []byte(strconv.FormatInt(triggerVersion, 10))},
		},
	})
}

// DeadLetter publishes src unchanged to the DLQ with the headers of spec §8.3.
func (p *Publisher) DeadLetter(src Source, reason, message string) *Ack {
	return p.send(DeadLetterRecord(p.dlqTopic, src, reason, message, p.now()))
}

// DeadLetterRecord builds a dead letter: the original key and bytes, and the
// headers of spec §8.3 with service=adapter.
func DeadLetterRecord(topic string, src Source, reason, message string, failedAt time.Time) *kgo.Record {
	return &kgo.Record{
		Topic: topic,
		Key:   src.Key,
		Value: src.Value,
		Headers: []kgo.RecordHeader{
			{Key: HeaderErrorReason, Value: []byte(reason)},
			{Key: HeaderErrorMessage, Value: []byte(Truncate(message, MaxErrorMessage))},
			{Key: HeaderSourceTopic, Value: []byte(src.Topic)},
			{Key: HeaderSourcePart, Value: []byte(strconv.FormatInt(int64(src.Partition), 10))},
			{Key: HeaderSourceOffset, Value: []byte(strconv.FormatInt(src.Offset, 10))},
			{Key: HeaderFailedAt, Value: []byte(failedAt.UTC().Format("2006-01-02T15:04:05.000Z07:00"))},
			{Key: HeaderService, Value: []byte("adapter")},
		},
	}
}

// Truncate cuts s to at most n bytes without splitting a UTF-8 character.
func Truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// send produces rec and retries failed produces until it is acknowledged or
// the process stops.
func (p *Publisher) send(rec *kgo.Record) *Ack {
	ack := newAck()
	var try func(attempt int)
	try = func(attempt int) {
		r := cloneRecord(rec) // a failed record is never reused by the client
		p.prod.Produce(p.life, r, func(_ *kgo.Record, err error) {
			if err == nil {
				ack.resolve(nil)
				return
			}
			if p.life.Err() != nil {
				ack.resolve(err)
				return
			}
			p.log.Warn("produce failed; retrying", "topic", rec.Topic, "key", string(rec.Key),
				"attempt", attempt, "error", err.Error())
			time.AfterFunc(p.retryDelay, func() { try(attempt + 1) })
		})
	}
	try(1)
	return ack
}

func cloneRecord(r *kgo.Record) *kgo.Record {
	return &kgo.Record{Topic: r.Topic, Key: r.Key, Value: r.Value, Headers: append([]kgo.RecordHeader(nil), r.Headers...)}
}
