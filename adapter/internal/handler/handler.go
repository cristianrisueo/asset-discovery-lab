// Package handler is the adapter's service layer. It turns one trigger into
// a full entity: decode and validate, cache, fetch from datacore, cache,
// publish (spec §4.3 steps 2 to 5, §9). What can never succeed is
// dead-lettered (spec §9.6).
package handler

import (
	"context"
	"errors"
	"log/slog"

	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"

	"github.com/cristianrisueo/asset-discovery-lab/adapter/internal/cache"
	"github.com/cristianrisueo/asset-discovery-lab/adapter/internal/contract"
	"github.com/cristianrisueo/asset-discovery-lab/adapter/internal/fetcher"
	"github.com/cristianrisueo/asset-discovery-lab/adapter/internal/publisher"
	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
)

// Cache is the entity cache (cache.Cache). Errors never surface: they are misses.
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, bool)
	Set(ctx context.Context, key string, value []byte)
}

// Fetcher gets an entity from datacore with the retry policy (fetcher.Fetcher).
type Fetcher interface {
	Fetch(ctx context.Context, et contract.EntityType, id string, triggerVersion int64, key string, partition int32) fetcher.Result
}

// Publisher produces entity records and dead letters (publisher.Publisher).
type Publisher interface {
	Entity(topicSuffix, id string, value []byte, triggerVersion int64) *publisher.Ack
	DeadLetter(src publisher.Source, reason, message string) *publisher.Ack
}

// ErrAborted is the Ack error of a record not handled because the adapter is stopping.
var ErrAborted = errors.New("handling aborted by shutdown")

var marshal = proto.MarshalOptions{Deterministic: true}

// Handler handles one trigger record.
type Handler struct {
	Cache     Cache
	Fetcher   Fetcher
	Publisher Publisher
	Log       *slog.Logger
}

// Handle handles rec and returns the Ack of the record it produced: the
// entity or the dead letter. The cache SET, when there is one, has finished
// before Handle returns, so the next record of the partition sees it (§9.3).
func (h *Handler) Handle(ctx context.Context, rec *kgo.Record) *publisher.Ack {
	src := publisher.SourceOf(rec)
	key := string(rec.Key)

	trig := &adlv1.Trigger{}
	if err := proto.Unmarshal(rec.Value, trig); err != nil {
		return h.deadLetter(src, key, 0, publisher.ReasonDecode, err.Error(), 0)
	}
	if err := contract.ValidateTrigger(key, trig); err != nil {
		return h.deadLetter(src, key, trig.GetVersion(), publisher.ReasonValidation, err.Error(), 0)
	}
	et, _ := contract.TypeOf(trig.GetEntityType())
	id, version := trig.GetEntityId(), trig.GetVersion()

	// The lookup uses the trigger's version. A hit is still a full publish.
	if b, ok := h.Cache.Get(ctx, cache.Key(et.Name, id, version)); ok {
		m := et.New()
		if err := proto.Unmarshal(b, m); err == nil {
			if v, err := contract.ValidateEntity(id, m); err == nil && v == version {
				h.Log.Debug("published from cache", "key", key, "version", version, "outcome", "cache_hit", "attempt", 0)
				return h.Publisher.Entity(et.TopicSuffix, id, b, version)
			}
		}
		h.Log.Warn("cached entity is unusable; treated as a miss", "key", key, "version", version)
	}

	res := h.Fetcher.Fetch(ctx, et, id, version, key, rec.Partition)
	switch res.Class {
	case fetcher.Success:
		b, err := marshal.Marshal(res.Entity)
		if err != nil {
			return h.deadLetter(src, key, version, publisher.ReasonValidation, err.Error(), res.Attempts)
		}
		// Stored under the entity's own version, which can be newer (§9.3).
		h.Cache.Set(ctx, cache.Key(et.Name, id, res.Version), b)
		h.Log.Debug("published", "key", key, "version", version, "entity_version", res.Version,
			"outcome", "success", "attempt", res.Attempts)
		return h.Publisher.Entity(et.TopicSuffix, id, b, version)
	case fetcher.NotFound:
		return h.deadLetter(src, key, version, publisher.ReasonNotFound, res.Detail, res.Attempts)
	case fetcher.InvalidEntity, fetcher.InvalidArgument:
		return h.deadLetter(src, key, version, publisher.ReasonValidation, res.Detail, res.Attempts)
	case fetcher.StaleTimeout:
		return h.deadLetter(src, key, version, publisher.ReasonStaleVersion, res.Detail, res.Attempts)
	default: // Aborted: nothing is published, so the offset is never committed.
		return publisher.Failed(ErrAborted)
	}
}

// deadLetter logs one line per dead letter (spec §9.8) and produces it.
func (h *Handler) deadLetter(src publisher.Source, key string, version int64, reason, detail string, attempt int) *publisher.Ack {
	h.Log.Warn("dead letter", "key", key, "version", version, "outcome", reason, "attempt", attempt,
		"source_partition", src.Partition, "source_offset", src.Offset, "detail", publisher.Truncate(detail, 300))
	return h.Publisher.DeadLetter(src, reason, detail)
}
