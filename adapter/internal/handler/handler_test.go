package handler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/cristianrisueo/asset-discovery-lab/adapter/internal/contract"
	"github.com/cristianrisueo/asset-discovery-lab/adapter/internal/fetcher"
	"github.com/cristianrisueo/asset-discovery-lab/adapter/internal/publisher"
	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
)

type fakeCache struct {
	data map[string][]byte
	sets []string
}

func (c *fakeCache) Get(_ context.Context, k string) ([]byte, bool) { b, ok := c.data[k]; return b, ok }
func (c *fakeCache) Set(_ context.Context, k string, v []byte) {
	c.data[k] = v
	c.sets = append(c.sets, k)
}

type fakeFetcher struct {
	result fetcher.Result
	calls  int
}

func (f *fakeFetcher) Fetch(context.Context, contract.EntityType, string, int64, string, int32) fetcher.Result {
	f.calls++
	return f.result
}

type published struct {
	topicSuffix, id string
	value           []byte
	triggerVersion  int64
	reason          string // set for dead letters
	src             publisher.Source
}

type fakePublisher struct{ out []published }

func (p *fakePublisher) Entity(s, id string, v []byte, tv int64) *publisher.Ack {
	p.out = append(p.out, published{topicSuffix: s, id: id, value: v, triggerVersion: tv})
	return publisher.Failed(nil)
}

func (p *fakePublisher) DeadLetter(src publisher.Source, reason, _ string) *publisher.Ack {
	p.out = append(p.out, published{reason: reason, src: src})
	return publisher.Failed(nil)
}

func asset(v int64) *adlv1.Asset {
	return &adlv1.Asset{Id: "LT-0042", Hostname: "lt-0042.acme.local", Kind: adlv1.AssetKind_ASSET_KIND_LAPTOP,
		Os: "Windows 11", Site: "Madrid", OwnerUserId: "U-0117", Version: v, ChangedAt: timestamppb.Now()}
}

func triggerRecord(t *testing.T, key string, tr *adlv1.Trigger) *kgo.Record {
	t.Helper()
	b, err := proto.Marshal(tr)
	if err != nil {
		t.Fatal(err)
	}
	return &kgo.Record{Topic: "triggers", Partition: 1, Offset: 42, Key: []byte(key), Value: b}
}

func validTrigger(t *testing.T, v int64) *kgo.Record {
	return triggerRecord(t, "asset:LT-0042", &adlv1.Trigger{EntityType: adlv1.EntityType_ENTITY_TYPE_ASSET, EntityId: "LT-0042", Version: v, ChangedAt: timestamppb.Now()})
}

type env struct {
	h     *Handler
	cache *fakeCache
	fetch *fakeFetcher
	pub   *fakePublisher
}

func newEnv() env {
	e := env{cache: &fakeCache{data: map[string][]byte{}}, fetch: &fakeFetcher{}, pub: &fakePublisher{}}
	e.h = &Handler{Cache: e.cache, Fetcher: e.fetch, Publisher: e.pub, Log: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	return e
}

func TestCacheHitPublishesWithoutFetching(t *testing.T) {
	e := newEnv()
	b, _ := proto.Marshal(asset(7))
	e.cache.data["entity:asset:LT-0042:v7"] = b
	if err := e.h.Handle(context.Background(), validTrigger(t, 7)).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if e.fetch.calls != 0 {
		t.Fatalf("a cache hit called datacore %d times", e.fetch.calls)
	}
	if len(e.pub.out) != 1 || e.pub.out[0].topicSuffix != "assets" || e.pub.out[0].id != "LT-0042" ||
		string(e.pub.out[0].value) != string(b) || e.pub.out[0].triggerVersion != 7 {
		t.Fatalf("published %+v", e.pub.out)
	}
}

func TestMissFetchesCachesUnderOwnVersionAndPublishes(t *testing.T) {
	e := newEnv()
	e.fetch.result = fetcher.Result{Class: fetcher.Success, Entity: asset(9), Version: 9, Attempts: 1}
	e.h.Handle(context.Background(), validTrigger(t, 7))
	if e.fetch.calls != 1 {
		t.Fatalf("fetch calls %d, want 1", e.fetch.calls)
	}
	if len(e.cache.sets) != 1 || e.cache.sets[0] != "entity:asset:LT-0042:v9" {
		t.Fatalf("cache sets %v, want the entity's own version v9", e.cache.sets)
	}
	got := &adlv1.Asset{}
	if err := proto.Unmarshal(e.pub.out[0].value, got); err != nil || got.GetVersion() != 9 || e.pub.out[0].triggerVersion != 7 {
		t.Fatalf("published %v (trigger.version %d), %v", got, e.pub.out[0].triggerVersion, err)
	}
	if string(e.cache.data["entity:asset:LT-0042:v9"]) != string(e.pub.out[0].value) {
		t.Fatal("the cached bytes differ from the published bytes")
	}
}

func TestUnusableCacheValueIsAMiss(t *testing.T) {
	e := newEnv()
	e.cache.data["entity:asset:LT-0042:v7"] = []byte{0xff, 0xff, 0xff}
	e.fetch.result = fetcher.Result{Class: fetcher.Success, Entity: asset(7), Version: 7}
	e.h.Handle(context.Background(), validTrigger(t, 7))
	if e.fetch.calls != 1 {
		t.Fatalf("a corrupt cache value was published instead of fetching (calls %d)", e.fetch.calls)
	}
}

func TestDeadLetterReasons(t *testing.T) {
	unspecified := triggerRecord(t, "test:invalid:1", &adlv1.Trigger{EntityId: "LT-0042", Version: 1, ChangedAt: timestamppb.Now()})
	mismatch := triggerRecord(t, "asset:LT-9005", &adlv1.Trigger{EntityType: adlv1.EntityType_ENTITY_TYPE_ASSET, EntityId: "LT-9006", Version: 1, ChangedAt: timestamppb.Now()})
	garbage := &kgo.Record{Topic: "triggers", Partition: 2, Offset: 7, Key: []byte("test:garbage:1"), Value: []byte{0x0a, 0xff, 0xff, 0xff, 0xff}}
	tests := []struct {
		name   string
		rec    *kgo.Record
		result fetcher.Result
		reason string
	}{
		{"undecodable", garbage, fetcher.Result{}, publisher.ReasonDecode},
		{"unspecified type", unspecified, fetcher.Result{}, publisher.ReasonValidation},
		{"key mismatch", mismatch, fetcher.Result{}, publisher.ReasonValidation},
		{"not found", validTrigger(t, 1), fetcher.Result{Class: fetcher.NotFound}, publisher.ReasonNotFound},
		{"invalid argument", validTrigger(t, 1), fetcher.Result{Class: fetcher.InvalidArgument}, publisher.ReasonValidation},
		{"invalid entity", validTrigger(t, 1), fetcher.Result{Class: fetcher.InvalidEntity}, publisher.ReasonValidation},
		{"stale timeout", validTrigger(t, 1), fetcher.Result{Class: fetcher.StaleTimeout}, publisher.ReasonStaleVersion},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv()
			e.fetch.result = tt.result
			e.h.Handle(context.Background(), tt.rec)
			if len(e.pub.out) != 1 || e.pub.out[0].reason != tt.reason {
				t.Fatalf("published %+v, want one dead letter %s", e.pub.out, tt.reason)
			}
			src := e.pub.out[0].src
			if src.Topic != "triggers" || src.Partition != tt.rec.Partition || src.Offset != tt.rec.Offset ||
				string(src.Key) != string(tt.rec.Key) || string(src.Value) != string(tt.rec.Value) {
				t.Fatalf("dead letter source %+v does not match the record", src)
			}
		})
	}
}

func TestAbortedPublishesNothing(t *testing.T) {
	e := newEnv()
	e.fetch.result = fetcher.Result{Class: fetcher.Aborted}
	err := e.h.Handle(context.Background(), validTrigger(t, 3)).Wait(context.Background())
	if !errors.Is(err, ErrAborted) || len(e.pub.out) != 0 {
		t.Fatalf("aborted fetch: ack %v, published %+v", err, e.pub.out)
	}
}
