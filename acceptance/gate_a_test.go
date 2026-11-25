//go:build acceptance

package acceptance

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
	"github.com/cristianrisueo/asset-discovery-lab/labkit"
)

// Gate A: ingestion (datacore, Kafka, adapter, Dragonfly). Spec §16.4.

var gateAServices = []string{labkit.CompKafka, labkit.CompDatacore, labkit.CompAdapter}

func TestGateA_01_SeedPublished(t *testing.T) {
	h := newHarness(t, gateAServices...)
	h.pauseGenerator()
	tails := map[string]*labkit.Tail{}
	keys := map[string]map[string]bool{}
	for _, topic := range labkit.EntityTopics {
		tails[topic] = h.tailFromStart(topic)
		keys[topic] = map[string]bool{}
	}
	h.eventually(TReconcile, "distinct valid keys per entities.* topic equal datacore's stats", func(ctx context.Context) (bool, string, error) {
		for topic, tail := range tails {
			recs, err := tail.ReadToEnd(ctx)
			if err != nil {
				return false, "", err
			}
			for _, r := range recs {
				// Only records that pass §6.7, key equal to id (spec §16.3).
				if _, err := labkit.DecodeEntity(topic, r.Key, r.Value); err == nil {
					keys[topic][string(r.Key)] = true
				}
			}
		}
		st, err := h.lab.Datacore.Stats(ctx)
		if err != nil {
			return false, "", err
		}
		a, u, g := int64(len(keys[labkit.TopicEntityAssets])), int64(len(keys[labkit.TopicEntityUsers])), int64(len(keys[labkit.TopicEntityGroups]))
		state := fmt.Sprintf("distinct keys / datacore: assets %d/%d, users %d/%d, groups %d/%d",
			a, st.GetAssets(), u, st.GetUsers(), g, st.GetGroups())
		return a == st.GetAssets() && u == st.GetUsers() && g == st.GetGroups(), state, nil
	})
}

func TestGateA_02_AssetChangePublished(t *testing.T) {
	h := newHarness(t, gateAServices...)
	tail := h.tailFromEnd(labkit.TopicEntityAssets)
	a := h.upsertAsset(labkit.NewAssetRequest("LT-9001", "Madrid", ""))
	rec := h.waitEntityRecord(tail, "LT-9001", a.GetVersion(), TShort)

	got := &adlv1.Asset{}
	h.fatalIf(proto.Unmarshal(rec.Value, got), "decode the LT-9001 record")
	want, err := h.lab.Datacore.GetAsset(h.ctx, "LT-9001")
	h.fatalIf(err, "GetAsset(LT-9001)")
	if !proto.Equal(got, want) {
		t.Fatalf("entities.assets record %v differs from GetAsset %v", got, want)
	}
	h.requireHeader(rec, labkit.HeaderTriggerVer, strconv.FormatInt(a.GetVersion(), 10))
}

func TestGateA_03_TriggerContract(t *testing.T) {
	h := newHarness(t, gateAServices...)
	tail := h.tailFromEnd(labkit.TopicTriggers)
	a := h.upsertAsset(labkit.NewAssetRequest("LT-9002", "Valencia", ""))

	var trig *adlv1.Trigger
	h.eventually(TShort, "trigger asset:LT-9002 v"+strconv.FormatInt(a.GetVersion(), 10)+" on triggers", func(ctx context.Context) (bool, string, error) {
		if _, err := tail.ReadToEnd(ctx); err != nil {
			return false, "", err
		}
		for _, r := range tail.Records() {
			if string(r.Key) != "asset:LT-9002" {
				continue
			}
			tr := &adlv1.Trigger{}
			if err := proto.Unmarshal(r.Value, tr); err != nil {
				return false, "", labkit.Stop(fmt.Errorf("record with key asset:LT-9002 does not decode as a Trigger: %w", err))
			}
			if tr.GetVersion() == a.GetVersion() {
				trig = tr
				return true, "", nil
			}
		}
		return false, fmt.Sprintf("%d records read", len(tail.Records())), nil
	})
	if trig.GetEntityType() != adlv1.EntityType_ENTITY_TYPE_ASSET || trig.GetEntityId() != "LT-9002" {
		t.Fatalf("trigger %v: want ENTITY_TYPE_ASSET LT-9002", trig)
	}
	if !proto.Equal(trig.GetChangedAt(), a.GetChangedAt()) {
		t.Fatalf("trigger changed_at %v differs from the entity's %v", trig.GetChangedAt().AsTime(), a.GetChangedAt().AsTime())
	}
	if n := (&adlv1.Trigger{}).ProtoReflect().Descriptor().Fields().Len(); n != 4 {
		t.Fatalf("the Trigger descriptor has %d fields, want exactly 4", n)
	}
}

func TestGateA_04_UsersAndGroups(t *testing.T) {
	h := newHarness(t, gateAServices...)
	groups := h.tailFromEnd(labkit.TopicEntityGroups)
	users := h.tailFromEnd(labkit.TopicEntityUsers)
	g := h.upsertGroup(labkit.NewGroupRequest("G-90", "QA Gate A"))
	u := h.upsertUser(labkit.NewUserRequest("U-9001", "Gate A User", "G-90"))
	deadline := time.Now().Add(TShort)
	h.waitEntityRecord(groups, "G-90", g.GetVersion(), remaining(deadline))
	h.waitEntityRecord(users, "U-9001", u.GetVersion(), remaining(deadline))
}

func TestGateA_05_CacheHit(t *testing.T) {
	h := newHarness(t, append(append([]string(nil), gateAServices...), labkit.CompDragonfly)...)
	h.pauseGenerator()
	h.setChaos(labkit.ChaosOffConfig())
	triggers := h.tailFromEnd(labkit.TopicTriggers)
	entities := h.tailFromEnd(labkit.TopicEntityAssets)
	a := h.upsertAsset(labkit.NewAssetRequest("LT-9003", "Sevilla", ""))
	h.waitEntityRecord(entities, "LT-9003", a.GetVersion(), TShort)
	orig := h.waitRecord(triggers, TShort, "the original trigger of LT-9003", func(r labkit.Record) bool {
		if string(r.Key) != "asset:LT-9003" {
			return false
		}
		tr, err := labkit.DecodeTrigger(r.Key, r.Value)
		return err == nil && tr.GetVersion() == a.GetVersion()
	})
	h.waitQuiet(TPipe)

	st, err := h.lab.Datacore.Stats(h.ctx)
	h.fatalIf(err, "datacore stats")
	g0 := st.GetGetCalls()

	after := h.tailFromEnd(labkit.TopicEntityAssets)
	h.produce(labkit.TopicTriggers, orig.Key, orig.Value)
	h.produce(labkit.TopicTriggers, orig.Key, orig.Value)
	h.eventually(TShort, "two more LT-9003 records at the same version", func(ctx context.Context) (bool, string, error) {
		if _, err := after.ReadToEnd(ctx); err != nil {
			return false, "", err
		}
		n := len(entityRecords(after.Records(), "LT-9003", a.GetVersion()))
		return n >= 2, fmt.Sprintf("%d new records for LT-9003 v%d", n, a.GetVersion()), nil
	})

	st, err = h.lab.Datacore.Stats(h.ctx)
	h.fatalIf(err, "datacore stats")
	if st.GetGetCalls() != g0 {
		t.Fatalf("get_calls went from %d to %d: the duplicate triggers were not served from the cache", g0, st.GetGetCalls())
	}

	key := labkit.CacheKey("asset", "LT-9003", a.GetVersion())
	val, ok, err := h.lab.Dragonfly.Get(h.ctx, key)
	h.fatalIf(err, "GET %s", key)
	if !ok {
		t.Fatalf("Dragonfly key %s does not exist", key)
	}
	ttl, err := h.lab.Dragonfly.TTL(h.ctx, key)
	h.fatalIf(err, "TTL %s", key)
	if ttl <= 0 || ttl > 3600*time.Second {
		t.Fatalf("TTL of %s is %v, want within (0, 3600 s]", key, ttl)
	}
	cached := &adlv1.Asset{}
	h.fatalIf(proto.Unmarshal(val, cached), "decode the cached value")
	if !proto.Equal(cached, a) {
		t.Fatalf("cached asset %v differs from %v", cached, a)
	}
}

func TestGateA_06_DecodeErrorToDlq(t *testing.T) {
	h := newHarness(t, gateAServices...)
	dlq := h.tailFromEnd(labkit.TopicDLQAdapter)
	key := "test:garbage:" + labkit.NewUUID()
	value := labkit.UndecodableBytes(16, 64, &adlv1.Trigger{})
	src := h.produce(labkit.TopicTriggers, []byte(key), value)

	rec := h.waitRecord(dlq, TShort, "dead letter "+key, func(r labkit.Record) bool { return string(r.Key) == key })
	if !bytes.Equal(rec.Value, value) {
		t.Fatalf("dead letter value differs from the original bytes")
	}
	h.requireHeader(rec, labkit.HeaderErrorReason, labkit.ReasonDecodeError)
	h.requireHeader(rec, labkit.HeaderService, "adapter")
	h.requireHeader(rec, labkit.HeaderSourceTopic, labkit.TopicTriggers)
	h.requireHeader(rec, labkit.HeaderSourcePart, strconv.Itoa(int(src.Partition)))
	h.requireHeader(rec, labkit.HeaderSourceOffset, strconv.FormatInt(src.Offset, 10))

	entities := h.tailFromEnd(labkit.TopicEntityAssets)
	a := h.upsertAsset(labkit.NewAssetRequest("LT-9004", "Bilbao", ""))
	h.waitEntityRecord(entities, "LT-9004", a.GetVersion(), TShort)
}

func TestGateA_07_ValidationErrorToDlq(t *testing.T) {
	h := newHarness(t, gateAServices...)
	dlq := h.tailFromEnd(labkit.TopicDLQAdapter)
	key1 := "test:invalid:" + labkit.NewUUID()
	val1 := mustMarshal(t, &adlv1.Trigger{EntityType: adlv1.EntityType_ENTITY_TYPE_UNSPECIFIED, EntityId: "LT-9005", Version: 1, ChangedAt: timestamppb.Now()})
	key2 := "asset:LT-9005"
	val2 := mustMarshal(t, &adlv1.Trigger{EntityType: adlv1.EntityType_ENTITY_TYPE_ASSET, EntityId: "LT-9006", Version: 1, ChangedAt: timestamppb.Now()})
	h.produce(labkit.TopicTriggers, []byte(key1), val1)
	h.produce(labkit.TopicTriggers, []byte(key2), val2)

	deadline := time.Now().Add(TShort)
	r1 := h.waitRecord(dlq, remaining(deadline), "dead letter of the UNSPECIFIED trigger", keyAndValue(key1, val1))
	r2 := h.waitRecord(dlq, remaining(deadline), "dead letter of the trigger whose key mismatches its id", keyAndValue(key2, val2))
	h.requireHeader(r1, labkit.HeaderErrorReason, labkit.ReasonValidation)
	h.requireHeader(r2, labkit.HeaderErrorReason, labkit.ReasonValidation)
}

func TestGateA_08_NotFoundToDlq(t *testing.T) {
	h := newHarness(t, gateAServices...)
	dlq := h.tailFromEnd(labkit.TopicDLQAdapter)
	key := "asset:LT-8001"
	val := mustMarshal(t, &adlv1.Trigger{EntityType: adlv1.EntityType_ENTITY_TYPE_ASSET, EntityId: "LT-8001", Version: 1, ChangedAt: timestamppb.Now()})
	h.produce(labkit.TopicTriggers, []byte(key), val)
	rec := h.waitRecord(dlq, TShort, "dead letter of the trigger for LT-8001", keyAndValue(key, val))
	h.requireHeader(rec, labkit.HeaderErrorReason, labkit.ReasonNotFound)
}

func TestGateA_09_StaleReadRecovered(t *testing.T) {
	h := newHarness(t, gateAServices...)
	h.pauseGenerator()
	before := h.tailFromEnd(labkit.TopicEntityAssets)
	prev := h.upsertAsset(labkit.NewAssetRequest("LT-9007", "Alicante", ""))
	h.waitEntityRecord(before, "LT-9007", prev.GetVersion(), TShort)

	h.setChaos(&adlv1.ChaosConfig{StaleReadRate: 1.0, StaleWindowMs: 300})
	dlq := h.tailFromEnd(labkit.TopicDLQAdapter)
	after := h.tailFromEnd(labkit.TopicEntityAssets)
	a := h.changeSite(prev, "Madrid")
	h.waitEntityRecord(after, "LT-9007", a.GetVersion(), TShort)

	for _, r := range h.readAll(after) {
		if string(r.Key) != "LT-9007" {
			continue
		}
		if e, err := labkit.DecodeEntity(r.Topic, r.Key, r.Value); err == nil && e.Version < a.GetVersion() {
			t.Fatalf("a record of LT-9007 at v%d (lower than v%d) appeared after the upsert", e.Version, a.GetVersion())
		}
	}
	for _, r := range h.readAll(dlq) {
		if string(r.Key) == "asset:LT-9007" {
			t.Fatalf("dlq.adapter got a record for asset:LT-9007 with headers %v", r.Headers)
		}
	}
}

func TestGateA_10_TransientErrorsRetried(t *testing.T) {
	h := newHarness(t, gateAServices...)
	h.pauseGenerator()
	h.setChaos(&adlv1.ChaosConfig{UnavailableRate: 0.5})
	dlq := h.tailFromEnd(labkit.TopicDLQAdapter)
	entities := h.tailFromEnd(labkit.TopicEntityAssets)
	var last *adlv1.Asset
	for i := 0; i < 20; i++ {
		last = h.upsertAsset(labkit.NewAssetRequest("LT-9008", Sites[i%len(Sites)], ""))
	}
	h.waitEntityRecord(entities, "LT-9008", last.GetVersion(), TPipe)
	for _, r := range h.readAll(dlq) {
		if string(r.Key) == "asset:LT-9008" {
			t.Fatalf("dlq.adapter got a record for asset:LT-9008 with headers %v", r.Headers)
		}
	}
}
