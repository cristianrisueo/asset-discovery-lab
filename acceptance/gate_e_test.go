//go:build acceptance

package acceptance

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"strings"
	"testing"
	"time"

	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
	"github.com/cristianrisueo/asset-discovery-lab/labkit"
)

// Gate E: failures. Spec §16.4.

var gateEServices = gateCServices

// phantomKey matches a trigger key naming a phantom id (spec §5.4).
var phantomKey = regexp.MustCompile(`^asset:LT-8\d{3}$`)

func TestGateE_01_ChaosSoak(t *testing.T) {
	h := newHarness(t, gateEServices...)
	dlqAdapter := h.tailFromEnd(labkit.TopicDLQAdapter)
	dlqProcessor := h.tailFromEnd(labkit.TopicDLQProcessor)

	h.setChaos(labkit.ChaosOnConfig())
	h.setGenerator(20, false)
	t.Log("chaos soak: 180 s at 20 changes per second")
	h.sleep(180 * time.Second)
	_, err := h.lab.Datacore.PauseGenerator(h.ctx)
	h.fatalIf(err, "pause the generator")
	_, err = h.lab.Datacore.SetChaos(h.ctx, labkit.ChaosOffConfig())
	h.fatalIf(err, "set chaos off")

	h.reconcile("Doris and lake reconcile after the soak", labkit.ReconcileOptions{Timeout: TReconcile, NoPause: true})
	dups, err := h.lab.Doris.LakeDuplicates(h.ctx)
	h.fatalIf(err, "query lake duplicates")
	if len(dups) > 0 {
		t.Fatalf("%d duplicated (id, version) pairs in the lake, for example %+v", len(dups), dups[0])
	}

	reasons := map[string]int{}
	for _, r := range h.readAll(dlqAdapter) {
		reason, _ := r.Header(labkit.HeaderErrorReason)
		reasons[reason]++
		key := string(r.Key)
		if !strings.HasPrefix(key, "malformed:") && !phantomKey.MatchString(key) {
			t.Errorf("dlq.adapter record with key %q (reason %s): neither malformed nor a phantom id", key, reason)
		}
	}
	for _, want := range []string{labkit.ReasonDecodeError, labkit.ReasonValidation, labkit.ReasonNotFound} {
		if reasons[want] == 0 {
			t.Errorf("dlq.adapter got no %s record during the soak (reasons %v)", want, reasons)
		}
	}
	if reasons[labkit.ReasonStaleVersion] > 0 {
		t.Errorf("dlq.adapter got %d STALE_VERSION records during the soak", reasons[labkit.ReasonStaleVersion])
	}
	if n := len(h.readAll(dlqProcessor)); n > 0 {
		t.Errorf("dlq.processor got %d records during the soak, want none", n)
	}
}

// burstInBackground starts AdminService.Burst(count) and returns a channel
// with its outcome.
func (h *harness) burstInBackground(count int32) <-chan error {
	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(h.ctx, TRecover)
		defer cancel()
		_, err := h.lab.Datacore.Burst(ctx, count)
		done <- err
	}()
	return done
}

// startOnCleanup makes sure a service is started again even if the test fails.
func (h *harness) startOnCleanup(c *labkit.Compose, service string) {
	h.t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := c.Start(ctx, service); err != nil {
			h.t.Logf("start %s during cleanup: %v", service, err)
		}
	})
}

func TestGateE_02_KillAdapterMidBurst(t *testing.T) {
	h := newHarness(t, gateBServices...)
	h.pauseGenerator()
	c := h.compose()
	h.startOnCleanup(c, labkit.CompAdapter)

	burst := h.burstInBackground(1000)
	h.sleep(time.Second)
	h.fatalIf(c.Kill(h.ctx, labkit.CompAdapter), "kill the adapter")
	h.sleep(10 * time.Second)
	h.fatalIf(c.Start(h.ctx, labkit.CompAdapter), "start the adapter")
	h.fatalIf(<-burst, "Burst(1000)")

	deadline := time.Now().Add(TRecover)
	h.eventually(remaining(deadline), "adl-adapter lag on triggers reaches 0", func(ctx context.Context) (bool, string, error) {
		lag, _, err := h.lab.Kafka.GroupLag(ctx, "adl-adapter", labkit.TopicTriggers)
		if err != nil {
			return false, "", err
		}
		return lag == 0, fmt.Sprintf("lag %d", lag), nil
	})
	h.reconcile("Doris reconcile", labkit.ReconcileOptions{Timeout: remaining(deadline), NoLake: true, NoPause: true})
}

func TestGateE_03_KillTaskManagerMidBurst(t *testing.T) {
	h := newHarness(t, gateEServices...)
	h.pauseGenerator()
	c := h.compose()
	h.startOnCleanup(c, labkit.CompFlinkTaskManager)

	burst := h.burstInBackground(1000)
	h.sleep(5 * time.Second)
	h.fatalIf(c.Kill(h.ctx, labkit.CompFlinkTaskManager), "kill the TaskManager")
	h.sleep(15 * time.Second)
	h.fatalIf(c.Start(h.ctx, labkit.CompFlinkTaskManager), "start the TaskManager")
	h.fatalIf(<-burst, "Burst(1000)")

	deadline := time.Now().Add(TRecover)
	h.runningJob(remaining(deadline))
	h.reconcile("Doris and lake reconcile", labkit.ReconcileOptions{Timeout: remaining(deadline), NoPause: true})
	rows, err := h.lab.Doris.ChangeRateRows(h.ctx)
	h.fatalIf(err, "read change_rate_1m")
	if d := labkit.DuplicateWindows(rows); len(d) > 0 {
		t.Errorf("duplicate (window_start, entity_type) pairs in change_rate_1m: %v", d)
	}
	dups, err := h.lab.Doris.LakeDuplicates(h.ctx)
	h.fatalIf(err, "query lake duplicates")
	if len(dups) > 0 {
		t.Errorf("%d duplicated (id, version) pairs in the lake, for example %+v", len(dups), dups[0])
	}
}

// savepoints lists the entries of /flink-data/savepoints in the JobManager.
func (h *harness) savepoints(c *labkit.Compose) map[string]bool {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(h.ctx, TShort)
	defer cancel()
	out, err := c.Exec(ctx, labkit.CompFlinkJobManager, "sh", "-c", "ls -1 /flink-data/savepoints 2>/dev/null || true")
	h.fatalIf(err, "list /flink-data/savepoints")
	names := map[string]bool{}
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			names[l] = true
		}
	}
	return names
}

func TestGateE_04_GracefulRestart(t *testing.T) {
	h := newHarness(t, gateEServices...)
	c := h.compose()
	before := h.runningJob(TShort)
	existing := h.savepoints(c)

	downCtx, cancelDown := context.WithTimeout(h.ctx, 15*time.Minute)
	out, err := h.lab.Make(downCtx, "down")
	cancelDown()
	h.fatalIf(err, "make down\n%s", out)
	upCtx, cancelUp := context.WithTimeout(h.ctx, 30*time.Minute)
	out, err = h.lab.Make(upCtx, "up")
	cancelUp()
	h.fatalIf(err, "make up\n%s", out)

	var after labkit.JobSummary
	h.eventually(TRecover, "a new RUNNING job after make up", func(ctx context.Context) (bool, string, error) {
		j, err := h.lab.Flink.JobByName(ctx, labkit.JobName)
		if err != nil {
			return false, "", err
		}
		if j == nil {
			return false, "no job", nil
		}
		after = *j
		return j.ID != before.ID && j.State == "RUNNING", "job " + j.ID + " " + j.State, nil
	})
	restored, err := h.lab.Flink.LatestRestored(h.ctx, after.ID)
	h.fatalIf(err, "read the checkpoints of job %s", after.ID)
	if restored == nil || !restored.IsSavepoint {
		t.Fatalf("job %s did not restore from a savepoint: %+v", after.ID, restored)
	}
	name := path.Base(strings.TrimRight(restored.ExternalPath, "/"))
	if !strings.Contains(restored.ExternalPath, "/flink-data/savepoints/") || existing[name] {
		t.Fatalf("job %s restored from %s, which is not a savepoint written by make down (savepoints before: %v)",
			after.ID, restored.ExternalPath, existing)
	}

	a := h.upsertAsset(labkit.NewAssetRequest("LT-9503", "Valencia", ""))
	h.waitDorisAsset("LT-9503", a.GetVersion(), TPipe)
	h.reconcile("Doris reconcile", labkit.ReconcileOptions{Timeout: TReconcile, NoLake: true})
}

func TestGateE_05_CacheDown(t *testing.T) {
	h := newHarness(t, append(append([]string(nil), gateBServices...), labkit.CompDragonfly)...)
	c := h.compose()
	h.startOnCleanup(c, labkit.CompDragonfly)
	h.fatalIf(c.Stop(h.ctx, labkit.CompDragonfly), "stop Dragonfly")
	a := h.upsertAsset(labkit.NewAssetRequest("LT-9501", "Sevilla", ""))
	h.waitDorisAsset("LT-9501", a.GetVersion(), TPipe)
	h.fatalIf(c.Start(h.ctx, labkit.CompDragonfly), "start Dragonfly")
}

func TestGateE_06_DatacoreRestart(t *testing.T) {
	h := newHarness(t, gateBServices...)
	c := h.compose()
	a := h.upsertAsset(labkit.NewAssetRequest("LT-9502", "Bilbao", ""))
	v := a.GetVersion()
	h.fatalIf(c.Restart(h.ctx, labkit.CompDatacore), "restart datacore")
	readyz := h.cfg.DatacoreHTTP + "/readyz"
	h.eventually(TPipe, "datacore /readyz returns 200", func(ctx context.Context) (bool, string, error) {
		r, err := labkit.HTTPGet(ctx, readyz)
		if err != nil {
			return false, "", err
		}
		return r.Status == http.StatusOK, fmt.Sprintf("%d", r.Status), nil
	})

	var got *adlv1.Asset
	h.eventually(TShort, "GetAsset(LT-9502) after the restart", func(ctx context.Context) (bool, string, error) {
		g, err := h.lab.Datacore.GetAsset(ctx, "LT-9502")
		if err != nil {
			return false, "", err
		}
		got = g
		return true, "", nil
	})
	if got.GetVersion() != v {
		t.Fatalf("after the restart GetAsset(LT-9502) returned v%d, want v%d", got.GetVersion(), v)
	}
	next := h.upsertAsset(labkit.AssetRequestFrom(got))
	if next.GetVersion() != v+1 {
		t.Fatalf("the upsert after the restart gave v%d, want v%d", next.GetVersion(), v+1)
	}
	h.waitDorisAsset("LT-9502", v+1, TPipe)
	h.reconcile("reconcile", labkit.ReconcileOptions{Timeout: TReconcile})
}
