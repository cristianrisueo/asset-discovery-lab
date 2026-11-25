//go:build acceptance

package acceptance

import (
	"context"
	"fmt"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
	"github.com/cristianrisueo/asset-discovery-lab/labkit"
)

// Gate B: processing (Flink, Doris). Spec §16.4.

var gateBServices = []string{
	labkit.CompKafka, labkit.CompDatacore, labkit.CompAdapter,
	labkit.CompFlinkJobManager, labkit.CompFlinkTaskManager, labkit.CompDorisFE, labkit.CompDorisBE,
}

func TestGateB_01_SeedInDoris(t *testing.T) {
	h := newHarness(t, gateBServices...)
	h.reconcile("Doris reconcile", labkit.ReconcileOptions{Timeout: TReconcile, NoLake: true})
}

func TestGateB_02_ChangeVisible(t *testing.T) {
	h := newHarness(t, gateBServices...)
	a := h.upsertAsset(labkit.NewAssetRequest("LT-9101", "Madrid", ""))
	row := h.waitDorisAsset("LT-9101", a.GetVersion(), TPipe)
	if row.ID != "LT-9101" {
		t.Fatalf("row id %q", row.ID)
	}
	if d := labkit.AssetFieldDiff(a, row); d != "" {
		t.Fatalf("inventory.assets row of LT-9101 v%d differs from datacore: %s", a.GetVersion(), d)
	}
}

func TestGateB_03_OlderVersionIgnored(t *testing.T) {
	h := newHarness(t, gateBServices...)
	a := h.upsertAsset(labkit.NewAssetRequest("LT-9102", "Madrid", ""))
	if a.GetVersion() < 2 {
		a = h.upsertAsset(labkit.NewAssetRequest("LT-9102", "Madrid", ""))
	}
	row := h.waitDorisAsset("LT-9102", a.GetVersion(), TPipe)
	if row.Site != "Madrid" {
		t.Fatalf("precondition: LT-9102 v%d in Doris says %q, want Madrid", a.GetVersion(), row.Site)
	}

	older := proto.Clone(a).(*adlv1.Asset)
	older.Version = a.GetVersion() - 1
	older.Site = "Bilbao"
	same := proto.Clone(a).(*adlv1.Asset)
	same.Site = "Sevilla"
	h.produce(labkit.TopicEntityAssets, []byte("LT-9102"), mustMarshal(t, older))
	h.produce(labkit.TopicEntityAssets, []byte("LT-9102"), mustMarshal(t, same))

	h.sleep(NotHappeningWait)
	r, err := h.lab.Doris.AssetByID(h.ctx, "LT-9102")
	h.fatalIf(err, "read LT-9102")
	if r == nil || r.Site != "Madrid" || r.Version != a.GetVersion() {
		t.Fatalf("after the stale records, LT-9102 in Doris is %+v; want Madrid at v%d", r, a.GetVersion())
	}
}

func TestGateB_04_DuplicatesDropped(t *testing.T) {
	h := newHarness(t, gateBServices...)
	a := h.upsertAsset(labkit.NewAssetRequest("LT-9110", "Valencia", ""))
	h.waitDorisAsset("LT-9110", a.GetVersion(), TPipe)
	job := h.runningJob(TShort)

	var d0 float64
	h.eventually(TShort, "read versionGateDropped", func(ctx context.Context) (bool, string, error) {
		v, found, err := h.lab.Flink.SumOperatorMetric(ctx, job.ID, "VersionGate", "versionGateDropped")
		if err != nil {
			return false, "", err
		}
		d0 = v
		return found, "metric VersionGate.versionGateDropped not exposed yet", nil
	})

	cur, err := h.lab.Datacore.GetAsset(h.ctx, "LT-9110")
	h.fatalIf(err, "GetAsset(LT-9110)")
	if cur.GetVersion() != a.GetVersion() {
		t.Fatalf("GetAsset(LT-9110) returned v%d, want v%d", cur.GetVersion(), a.GetVersion())
	}
	value := mustMarshal(t, cur)
	for i := 0; i < 3; i++ {
		h.produce(labkit.TopicEntityAssets, []byte("LT-9110"), value)
	}
	h.eventually(TShort, fmt.Sprintf("versionGateDropped >= %g + 3", d0), func(ctx context.Context) (bool, string, error) {
		v, _, err := h.lab.Flink.SumOperatorMetric(ctx, job.ID, "VersionGate", "versionGateDropped")
		if err != nil {
			return false, "", err
		}
		return v >= d0+3, fmt.Sprintf("versionGateDropped = %g", v), nil
	})
}

func TestGateB_05_ProcessorDlq(t *testing.T) {
	h := newHarness(t, gateBServices...)
	dlq := h.tailFromEnd(labkit.TopicDLQProcessor)
	garbage := labkit.UndecodableBytes(16, 64, &adlv1.Asset{})
	noHostname := mustMarshal(t, &adlv1.Asset{Id: "LT-9104", Hostname: "", Kind: adlv1.AssetKind_ASSET_KIND_LAPTOP,
		Os: "Windows 11", Site: "Madrid", Version: 1, ChangedAt: timestamppb.Now()})
	wrongKey := mustMarshal(t, &adlv1.Asset{Id: "LT-9105", Hostname: "lt-9105.acme.local", Kind: adlv1.AssetKind_ASSET_KIND_LAPTOP,
		Os: "Windows 11", Site: "Madrid", Version: 1, ChangedAt: timestamppb.Now()})
	h.produce(labkit.TopicEntityAssets, []byte("LT-9103"), garbage)
	h.produce(labkit.TopicEntityAssets, []byte("LT-9104"), noHostname)
	h.produce(labkit.TopicEntityAssets, []byte("LT-9106"), wrongKey)

	cases := []struct {
		key    string
		value  []byte
		reason string
	}{
		{"LT-9103", garbage, labkit.ReasonDecodeError},
		{"LT-9104", noHostname, labkit.ReasonValidation},
		{"LT-9106", wrongKey, labkit.ReasonValidation},
	}
	deadline := time.Now().Add(TPipe)
	for _, c := range cases {
		r := h.waitRecord(dlq, remaining(deadline), "dlq.processor record with key "+c.key, keyAndValue(c.key, c.value))
		h.requireHeader(r, labkit.HeaderErrorReason, c.reason)
		h.requireHeader(r, labkit.HeaderService, "processor")
		h.requireHeader(r, labkit.HeaderSourceTopic, labkit.TopicEntityAssets)
	}

	h.requireJobRunning()
	a := h.upsertAsset(labkit.NewAssetRequest("LT-9107", "Barcelona", ""))
	h.waitDorisAsset("LT-9107", a.GetVersion(), TPipe)
	h.requireJobRunning()
}

// requireJobRunning fails unless the Flink job is RUNNING now.
func (h *harness) requireJobRunning() {
	h.t.Helper()
	j, err := h.lab.Flink.JobByName(h.ctx, labkit.JobName)
	h.fatalIf(err, "read the Flink job")
	if j == nil || j.State != "RUNNING" {
		h.t.Fatalf("Flink job %s is %+v, want RUNNING", labkit.JobName, j)
	}
}

func TestGateB_06_EnrichedView(t *testing.T) {
	h := newHarness(t, gateBServices...)
	h.upsertGroup(labkit.NewGroupRequest("G-91", "QA Doris"))
	h.upsertUser(labkit.NewUserRequest("U-9101", "Test User", "G-91"))
	a := h.upsertAsset(labkit.NewAssetRequest("LT-9108", "Madrid", "U-9101"))
	h.eventually(TPipe, "LT-9108 enriched with owner and group in v_assets_enriched", func(ctx context.Context) (bool, string, error) {
		r, err := h.lab.Doris.EnrichedByID(ctx, "LT-9108")
		if err != nil {
			return false, "", err
		}
		if r == nil {
			return false, "no row", nil
		}
		ok := r.Version == a.GetVersion() && r.OwnerName.Valid && r.OwnerName.String == "Test User" &&
			r.GroupName.Valid && r.GroupName.String == "QA Doris"
		return ok, fmt.Sprintf("v%d owner_name %v group_name %v", r.Version, r.OwnerName, r.GroupName), nil
	})
}

func TestGateB_07_MaterializedViewConsistent(t *testing.T) {
	h := newHarness(t, gateBServices...)
	h.pauseGenerator()
	h.reconcile("Doris reconcile", labkit.ReconcileOptions{Timeout: TReconcile, NoLake: true, NoPause: true})
	h.waitMVConsistent()
}

// ensureDefaultLoad puts the generator at its default rate, unpaused, and
// returns the time from which that load is guaranteed.
func (h *harness) ensureDefaultLoad(since time.Time) time.Time {
	h.t.Helper()
	cur, err := h.lab.Datacore.Generator(h.ctx)
	h.fatalIf(err, "read the generator")
	if cur.GetPaused() || cur.GetRatePerSecond() != labkit.DefaultGeneratorRate {
		h.setGenerator(labkit.DefaultGeneratorRate, false)
		return time.Now()
	}
	return since
}

func TestGateB_08_ChangeRateRows(t *testing.T) {
	h := newHarness(t, gateBServices...)
	job := h.runningJob(TShort)
	activeSince := h.ensureDefaultLoad(job.Started())
	if wait := time.Until(activeSince.Add(3 * time.Minute)); wait > 0 {
		t.Logf("waiting %s for 3 minutes of activity", wait.Round(time.Second))
		h.sleep(wait)
	}

	var rows []labkit.ChangeRateRow
	h.eventually(TPipe, "a closed asset row in change_rate_1m", func(ctx context.Context) (bool, string, error) {
		rs, err := h.lab.Doris.ChangeRateRows(ctx)
		if err != nil {
			return false, "", err
		}
		rows = rs
		now := time.Now().UTC()
		for _, r := range rs {
			if r.EntityType == "asset" && !r.WindowEnd.After(now) {
				return true, "", nil
			}
		}
		return false, fmt.Sprintf("%d rows, no closed asset row", len(rs)), nil
	})
	for _, r := range rows {
		if r.WindowEnd.Sub(r.WindowStart) != time.Minute {
			t.Errorf("row %+v: window_end - window_start = %s, want 60 s", r, r.WindowEnd.Sub(r.WindowStart))
		}
		if r.Changes <= 0 {
			t.Errorf("row %+v: changes must be > 0", r)
		}
	}
	if d := labkit.DuplicateWindows(rows); len(d) > 0 {
		t.Fatalf("duplicate (window_start, entity_type) pairs in change_rate_1m: %v", d)
	}
}

func TestGateB_09_Freshness(t *testing.T) {
	h := newHarness(t, gateBServices...)
	h.ensureDefaultLoad(time.Now())
	var latencies []time.Duration
	for i := 0; i < 20; i++ {
		a := h.upsertAsset(labkit.NewAssetRequest("LT-9109", Sites[i%len(Sites)], ""))
		start := time.Now()
		h.waitDorisAsset("LT-9109", a.GetVersion(), TPipe)
		latencies = append(latencies, time.Since(start))
	}
	p95, maxL := labkit.Percentile(latencies, 95), labkit.MaxDuration(latencies)
	t.Logf("freshness over 20 upserts: p95 %s, max %s, all %v", p95, maxL, latencies)
	if p95 > 30*time.Second {
		t.Errorf("p95 latency %s exceeds 30 s", p95)
	}
	if maxL > 60*time.Second {
		t.Errorf("maximum latency %s exceeds 60 s", maxL)
	}
}
