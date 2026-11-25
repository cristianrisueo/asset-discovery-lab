//go:build acceptance

package acceptance

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
	"github.com/cristianrisueo/asset-discovery-lab/labkit"
)

// Timeouts, defined once (spec §16.3).
const (
	TShort     = 30 * time.Second
	TPipe      = 90 * time.Second
	TLake      = 120 * time.Second
	TReconcile = 180 * time.Second
	TRecover   = 300 * time.Second
)

// NotHappeningWait is how long a test waits before asserting that something
// did not happen: at least three checkpoint intervals (spec §16.3).
const NotHappeningWait = 30 * time.Second

// MVRefreshWait is the MV consistency window of tests B07 and D05.
const MVRefreshWait = 150 * time.Second

// Sites is the site pool (spec §5.3).
var Sites = []string{"Alicante", "Madrid", "Barcelona", "Valencia", "Sevilla", "Bilbao"}

// harness carries the clients of one test and its failure diagnostics.
type harness struct {
	t        *testing.T
	ctx      context.Context
	cfg      labkit.Config
	lab      *labkit.Lab
	services []string
}

// newHarness builds the clients, registers the failure dump for the services
// involved, and fails at once, naming the endpoint, if any of them is not
// reachable from the host.
func newHarness(t *testing.T, services ...string) *harness {
	t.Helper()
	cfg := labkit.ConfigFromEnv()
	lab, err := labkit.NewLab(cfg)
	if err != nil {
		t.Fatalf("build labkit clients: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &harness{t: t, ctx: ctx, cfg: cfg, lab: lab, services: services}
	t.Cleanup(func() {
		cancel()
		lab.Close()
	})
	t.Cleanup(h.dumpOnFailure)
	if err := cfg.CheckReachable(ctx, services...); err != nil {
		t.Fatalf("the stack is not running or not reachable (start it with make up):\n%v", err)
	}
	return h
}

// dumpOnFailure prints docker compose ps and the last 200 log lines of the
// services involved (spec §16.3).
func (h *harness) dumpOnFailure() {
	if !h.t.Failed() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	compose, err := h.lab.Compose()
	if err != nil {
		h.t.Logf("failure diagnostics unavailable: %v", err)
		return
	}
	ps, err := compose.PS(ctx)
	h.t.Logf("docker compose ps:\n%s%s", ps, errSuffix(err))
	for _, s := range h.services {
		logs, err := compose.Logs(ctx, 200, s)
		h.t.Logf("last 200 log lines of %s:\n%s%s", s, logs, errSuffix(err))
	}
}

func errSuffix(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("\n(error: %v)", err)
}

// fatalIf fails the test when err is not nil.
func (h *harness) fatalIf(err error, format string, args ...any) {
	h.t.Helper()
	if err != nil {
		h.t.Fatalf("%s: %v", fmt.Sprintf(format, args...), err)
	}
}

// eventually polls fn every second until it reports done, and fails with the
// last observed state on timeout (spec §16.3).
func (h *harness) eventually(timeout time.Duration, what string, fn labkit.PollFunc) {
	h.t.Helper()
	if err := labkit.Poll(h.ctx, timeout, labkit.PollInterval, fn); err != nil {
		h.t.Fatalf("%s: %v", what, err)
	}
}

// sleep waits d, or fails if the test context ends first.
func (h *harness) sleep(d time.Duration) {
	h.t.Helper()
	select {
	case <-h.ctx.Done():
		h.t.Fatalf("interrupted while waiting %s", d)
	case <-time.After(d):
	}
}

func (h *harness) restoreOnCleanup(what string, restore func(context.Context) error) {
	h.t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := restore(ctx); err != nil {
			h.t.Errorf("restore %s: %v", what, err)
		}
	})
}

// pauseGenerator pauses the generator and restores it in t.Cleanup.
func (h *harness) pauseGenerator() {
	h.t.Helper()
	restore, err := h.lab.Datacore.PauseGenerator(h.ctx)
	h.fatalIf(err, "pause the generator")
	h.restoreOnCleanup("the generator", restore)
}

// setGenerator applies a generator configuration and restores the previous
// one in t.Cleanup.
func (h *harness) setGenerator(rate float64, paused bool) {
	h.t.Helper()
	restore, err := h.lab.Datacore.SetGenerator(h.ctx, &adlv1.GeneratorConfig{RatePerSecond: rate, Paused: paused})
	h.fatalIf(err, "set the generator to %g/s paused=%v", rate, paused)
	h.restoreOnCleanup("the generator", restore)
}

// setChaos applies a chaos configuration and resets it in t.Cleanup.
func (h *harness) setChaos(cfg *adlv1.ChaosConfig) {
	h.t.Helper()
	restore, err := h.lab.Datacore.SetChaos(h.ctx, cfg)
	h.fatalIf(err, "set chaos")
	h.restoreOnCleanup("chaos", restore)
}

func (h *harness) upsertAsset(req *adlv1.UpsertAssetRequest) *adlv1.Asset {
	h.t.Helper()
	a, err := h.lab.Datacore.UpsertAsset(h.ctx, req)
	h.fatalIf(err, "upsert asset %s", req.GetId())
	return a
}

func (h *harness) upsertUser(req *adlv1.UpsertUserRequest) *adlv1.User {
	h.t.Helper()
	u, err := h.lab.Datacore.UpsertUser(h.ctx, req)
	h.fatalIf(err, "upsert user %s", req.GetId())
	return u
}

func (h *harness) upsertGroup(req *adlv1.UpsertGroupRequest) *adlv1.Group {
	h.t.Helper()
	g, err := h.lab.Datacore.UpsertGroup(h.ctx, req)
	h.fatalIf(err, "upsert group %s", req.GetId())
	return g
}

// changeSite upserts a with another site and returns the new version.
func (h *harness) changeSite(a *adlv1.Asset, site string) *adlv1.Asset {
	h.t.Helper()
	req := labkit.AssetRequestFrom(a)
	req.Site = site
	return h.upsertAsset(req)
}

// tailFromEnd starts reading topic from its current end.
func (h *harness) tailFromEnd(topic string) *labkit.Tail {
	h.t.Helper()
	tail, err := h.lab.Kafka.TailFromEnd(h.ctx, topic)
	h.fatalIf(err, "start reading %s", topic)
	h.t.Cleanup(tail.Close)
	return tail
}

// tailFromStart starts reading topic from the beginning of its log.
func (h *harness) tailFromStart(topic string) *labkit.Tail {
	h.t.Helper()
	tail, err := h.lab.Kafka.NewTail(h.ctx, topic, nil)
	h.fatalIf(err, "start reading %s", topic)
	h.t.Cleanup(tail.Close)
	return tail
}

func (h *harness) produce(topic string, key, value []byte) labkit.Record {
	h.t.Helper()
	rec, err := h.lab.Kafka.Produce(h.ctx, topic, key, value, nil)
	h.fatalIf(err, "produce to %s", topic)
	return rec
}

func mustMarshal(t *testing.T, m proto.Message) []byte {
	t.Helper()
	b, err := proto.Marshal(m)
	if err != nil {
		t.Fatalf("marshal %T: %v", m, err)
	}
	return b
}

// entityRecords returns the records with key that pass spec §6.7 and carry version.
func entityRecords(recs []labkit.Record, key string, version int64) []labkit.Record {
	var out []labkit.Record
	for _, r := range recs {
		if string(r.Key) != key {
			continue
		}
		if e, err := labkit.DecodeEntity(r.Topic, r.Key, r.Value); err == nil && e.Version == version {
			out = append(out, r)
		}
	}
	return out
}

// waitEntityRecord waits until tail holds a valid record with key and version.
func (h *harness) waitEntityRecord(tail *labkit.Tail, key string, version int64, timeout time.Duration) labkit.Record {
	h.t.Helper()
	var found labkit.Record
	h.eventually(timeout, fmt.Sprintf("record %s v%d on the entities topic", key, version), func(ctx context.Context) (bool, string, error) {
		if _, err := tail.ReadToEnd(ctx); err != nil {
			return false, "", err
		}
		if m := entityRecords(tail.Records(), key, version); len(m) > 0 {
			found = m[0]
			return true, "", nil
		}
		return false, fmt.Sprintf("%d records read, none with key %s at v%d (latest seen: %s)",
			len(tail.Records()), key, version, latestVersion(tail.Records(), key)), nil
	})
	return found
}

func latestVersion(recs []labkit.Record, key string) string {
	best := int64(-1)
	for _, r := range recs {
		if string(r.Key) != key {
			continue
		}
		if e, err := labkit.DecodeEntity(r.Topic, r.Key, r.Value); err == nil && e.Version > best {
			best = e.Version
		}
	}
	if best < 0 {
		return "none"
	}
	return "v" + strconv.FormatInt(best, 10)
}

// waitRecord waits until tail holds a record matching match.
func (h *harness) waitRecord(tail *labkit.Tail, timeout time.Duration, what string, match func(labkit.Record) bool) labkit.Record {
	h.t.Helper()
	var found labkit.Record
	h.eventually(timeout, what, func(ctx context.Context) (bool, string, error) {
		if _, err := tail.ReadToEnd(ctx); err != nil {
			return false, "", err
		}
		for _, r := range tail.Records() {
			if match(r) {
				found = r
				return true, "", nil
			}
		}
		return false, fmt.Sprintf("%d records read, none matching", len(tail.Records())), nil
	})
	return found
}

// keyAndValue matches a record by exact key and bytes.
func keyAndValue(key string, value []byte) func(labkit.Record) bool {
	return func(r labkit.Record) bool { return string(r.Key) == key && string(r.Value) == string(value) }
}

// readAll reads tail to its current end and returns every record read so far.
func (h *harness) readAll(tail *labkit.Tail) []labkit.Record {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(h.ctx, TShort)
	defer cancel()
	_, err := tail.ReadToEnd(ctx)
	h.fatalIf(err, "read to the end of the topic")
	return tail.Records()
}

// requireHeader fails unless the record carries header name with value want.
func (h *harness) requireHeader(r labkit.Record, name, want string) {
	h.t.Helper()
	got, ok := r.Header(name)
	if !ok || got != want {
		h.t.Fatalf("record %q on %s: header %s = %q (present %v), want %q; headers %v", r.Key, r.Topic, name, got, ok, want, r.Headers)
	}
}

// waitDorisAsset waits until inventory.assets holds id at version and returns the row.
func (h *harness) waitDorisAsset(id string, version int64, timeout time.Duration) labkit.AssetRow {
	h.t.Helper()
	var row labkit.AssetRow
	h.eventually(timeout, fmt.Sprintf("%s v%d in inventory.assets", id, version), func(ctx context.Context) (bool, string, error) {
		r, err := h.lab.Doris.AssetByID(ctx, id)
		if err != nil {
			return false, "", err
		}
		if r == nil {
			return false, "no row", nil
		}
		row = *r
		return r.Version == version, fmt.Sprintf("row at v%d", r.Version), nil
	})
	return row
}

// waitQuiet waits until datacore has no pending outbox rows and the adapter
// has no lag on triggers.
func (h *harness) waitQuiet(timeout time.Duration) {
	h.t.Helper()
	h.eventually(timeout, "quiet system (outbox_pending = 0, adl-adapter lag = 0)", func(ctx context.Context) (bool, string, error) {
		st, err := h.lab.Datacore.Stats(ctx)
		if err != nil {
			return false, "", err
		}
		lag, _, err := h.lab.Kafka.GroupLag(ctx, "adl-adapter", labkit.TopicTriggers)
		if err != nil {
			return false, "", err
		}
		return st.GetOutboxPending() == 0 && lag == 0, fmt.Sprintf("outbox_pending %d, lag %d", st.GetOutboxPending(), lag), nil
	})
}

// runningJob waits until the Flink job is RUNNING and returns it.
func (h *harness) runningJob(timeout time.Duration) labkit.JobSummary {
	h.t.Helper()
	var job labkit.JobSummary
	h.eventually(timeout, "Flink job "+labkit.JobName+" RUNNING", func(ctx context.Context) (bool, string, error) {
		j, err := h.lab.Flink.JobByName(ctx, labkit.JobName)
		if err != nil {
			return false, "", err
		}
		if j == nil {
			return false, "no job", nil
		}
		job = *j
		return j.State == "RUNNING", "job " + j.ID + " " + j.State, nil
	})
	return job
}

// reconcile runs spec §16.5 and fails with the report unless it passes.
func (h *harness) reconcile(what string, opts labkit.ReconcileOptions) {
	h.t.Helper()
	opts.Logf = h.t.Logf
	rep, err := h.lab.Reconcile(h.ctx, opts)
	if err != nil {
		h.t.Fatalf("%s: %v\n%s", what, err, rep.String())
	}
}

// getStable GETs url until it answers with something other than a transport
// error or 503, and returns that response.
func (h *harness) getStable(url string, timeout time.Duration) *labkit.HTTPResponse {
	h.t.Helper()
	var resp *labkit.HTTPResponse
	h.eventually(timeout, "GET "+url, func(ctx context.Context) (bool, string, error) {
		r, err := labkit.HTTPGet(ctx, url)
		if err != nil {
			return false, "", err
		}
		resp = r
		return r.Status != http.StatusServiceUnavailable, fmt.Sprintf("%d %s", r.Status, strings.TrimSpace(string(r.Body))), nil
	})
	return resp
}

// requireStatus fails unless resp has the status.
func (h *harness) requireStatus(resp *labkit.HTTPResponse, url string, want int) {
	h.t.Helper()
	if resp.Status != want {
		h.t.Fatalf("GET %s: status %d, want %d; body %s", url, resp.Status, want, resp.Body)
	}
}

// apiError is the explorer's error body (spec §13.3).
type apiError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// requireAPIError fails unless resp has the status and the error code.
func (h *harness) requireAPIError(resp *labkit.HTTPResponse, url string, status int, code string) {
	h.t.Helper()
	h.requireStatus(resp, url, status)
	var e apiError
	h.fatalIf(resp.JSON(&e), "GET %s: error body", url)
	if e.Error != code {
		h.t.Fatalf("GET %s: error %q, want %q; body %s", url, e.Error, code, resp.Body)
	}
}

// waitMVConsistent runs the manual refresh, then waits until the MV rows
// equal the aggregate of inventory.assets (tests B07 and D05). A refresh error
// is only logged: the scheduled refresh also satisfies the condition.
func (h *harness) waitMVConsistent() []labkit.OSSiteRow {
	h.t.Helper()
	if err := h.lab.Doris.RefreshMV(h.ctx); err != nil {
		h.t.Logf("manual MV refresh failed; waiting for the scheduled refresh: %v", err)
	}
	var mv []labkit.OSSiteRow
	h.eventually(MVRefreshWait, "inventory.mv_assets_by_os_site equals the aggregate of inventory.assets", func(ctx context.Context) (bool, string, error) {
		m, err := h.lab.Doris.MVRows(ctx)
		if err != nil {
			return false, "", err
		}
		agg, err := h.lab.Doris.GroupByOSSite(ctx)
		if err != nil {
			return false, "", err
		}
		mv = m
		return fmt.Sprint(m) == fmt.Sprint(agg), fmt.Sprintf("mv %v, aggregate %v", m, agg), nil
	})
	return mv
}

// compose returns the docker compose runner of the repository.
func (h *harness) compose() *labkit.Compose {
	h.t.Helper()
	c, err := h.lab.Compose()
	h.fatalIf(err, "locate docker-compose.yml")
	return c
}

// remaining returns the time left until deadline, at least one second.
func remaining(deadline time.Time) time.Duration {
	if d := time.Until(deadline); d > time.Second {
		return d
	}
	return time.Second
}

// rfc3339Millis formats a timestamp as the JSON of spec §5.5.
func rfc3339Millis(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }
