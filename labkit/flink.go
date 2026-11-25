package labkit

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// JobName is the processor's Flink job name (spec §10.1).
const JobName = "adl-processor"

// Flink is a client for the Flink REST API.
type Flink struct {
	base string
}

// NewFlink builds a client for base, for example http://localhost:8081.
func NewFlink(base string) *Flink { return &Flink{base: strings.TrimRight(base, "/")} }

// Base is the REST base URL.
func (f *Flink) Base() string { return f.base }

func (f *Flink) get(ctx context.Context, path string, v any) error {
	r, err := HTTPGet(ctx, f.base+path)
	if err != nil {
		return fmt.Errorf("flink REST at %s: %w", f.base, err)
	}
	if r.Status != http.StatusOK {
		return fmt.Errorf("flink REST at %s: GET %s returned %d: %s", f.base, path, r.Status, truncate(string(r.Body), 300))
	}
	if v == nil {
		return nil
	}
	return r.JSON(v)
}

// Ping calls GET /overview.
func (f *Flink) Ping(ctx context.Context) error { return f.get(ctx, "/overview", nil) }

// JobSummary is an entry of GET /jobs/overview.
type JobSummary struct {
	ID        string `json:"jid"`
	Name      string `json:"name"`
	State     string `json:"state"`
	StartTime int64  `json:"start-time"` // epoch milliseconds
}

// Started returns the start time.
func (j JobSummary) Started() time.Time { return time.UnixMilli(j.StartTime).UTC() }

// Jobs lists every job the JobManager knows.
func (f *Flink) Jobs(ctx context.Context) ([]JobSummary, error) {
	var resp struct {
		Jobs []JobSummary `json:"jobs"`
	}
	if err := f.get(ctx, "/jobs/overview", &resp); err != nil {
		return nil, err
	}
	return resp.Jobs, nil
}

// ActiveJobStates are the states in which a submission counts as present
// (spec §10.11 step 2), plus the transient ones of a running job.
var ActiveJobStates = map[string]bool{
	"CREATED": true, "RUNNING": true, "RESTARTING": true, "INITIALIZING": true,
	"FAILING": true, "CANCELLING": true, "RECONCILING": true, "SUSPENDED": true,
}

// PickJob chooses the job named name: the newest active one, otherwise the
// newest of any state. It returns nil when there is none.
func PickJob(jobs []JobSummary, name string) *JobSummary {
	var cands []JobSummary
	for _, j := range jobs {
		if j.Name == name {
			cands = append(cands, j)
		}
	}
	if len(cands) == 0 {
		return nil
	}
	sort.SliceStable(cands, func(i, k int) bool {
		ai, ak := ActiveJobStates[cands[i].State], ActiveJobStates[cands[k].State]
		if ai != ak {
			return ai
		}
		return cands[i].StartTime > cands[k].StartTime
	})
	return &cands[0]
}

// JobByName returns the job named name (see PickJob), or nil.
func (f *Flink) JobByName(ctx context.Context, name string) (*JobSummary, error) {
	jobs, err := f.Jobs(ctx)
	if err != nil {
		return nil, err
	}
	return PickJob(jobs, name), nil
}

// Vertex is a job vertex.
type Vertex struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Vertices lists the vertices of a job.
func (f *Flink) Vertices(ctx context.Context, jid string) ([]Vertex, error) {
	var resp struct {
		Vertices []Vertex `json:"vertices"`
	}
	if err := f.get(ctx, "/jobs/"+jid, &resp); err != nil {
		return nil, err
	}
	return resp.Vertices, nil
}

// SumOperatorMetric sums an operator metric over every subtask of every
// vertex: Flink exposes it as "<subtask>.<operator>.<metric>". It returns
// found=false when no vertex exposes the metric yet.
func (f *Flink) SumOperatorMetric(ctx context.Context, jid, operator, metric string) (sum float64, found bool, err error) {
	vs, err := f.Vertices(ctx, jid)
	if err != nil {
		return 0, false, err
	}
	re := regexp.MustCompile(`^\d+\.` + regexp.QuoteMeta(operator) + `\.` + regexp.QuoteMeta(metric) + `$`)
	for _, v := range vs {
		var avail []struct {
			ID string `json:"id"`
		}
		base := "/jobs/" + jid + "/vertices/" + v.ID + "/metrics"
		if err := f.get(ctx, base, &avail); err != nil {
			return 0, false, err
		}
		var ids []string
		for _, a := range avail {
			if re.MatchString(a.ID) {
				ids = append(ids, a.ID)
			}
		}
		if len(ids) == 0 {
			continue
		}
		var vals []struct {
			ID    string `json:"id"`
			Value string `json:"value"`
		}
		if err := f.get(ctx, base+"?get="+strings.Join(ids, ","), &vals); err != nil {
			return 0, false, err
		}
		for _, mv := range vals {
			x, perr := strconv.ParseFloat(mv.Value, 64)
			if perr != nil {
				return 0, false, fmt.Errorf("metric %s: value %q: %w", mv.ID, mv.Value, perr)
			}
			sum += x
			found = true
		}
	}
	return sum, found, nil
}

// RestoredCheckpoint is latest.restored of GET /jobs/{id}/checkpoints.
type RestoredCheckpoint struct {
	ID               int64  `json:"id"`
	RestoreTimestamp int64  `json:"restore_timestamp"`
	IsSavepoint      bool   `json:"is_savepoint"`
	ExternalPath     string `json:"external_path"`
}

// LatestRestored returns what the job restored from, or nil for a fresh start.
func (f *Flink) LatestRestored(ctx context.Context, jid string) (*RestoredCheckpoint, error) {
	var resp struct {
		Latest struct {
			Restored *RestoredCheckpoint `json:"restored"`
		} `json:"latest"`
	}
	if err := f.get(ctx, "/jobs/"+jid+"/checkpoints", &resp); err != nil {
		return nil, err
	}
	return resp.Latest.Restored, nil
}

// TaskManager is an entry of GET /taskmanagers.
type TaskManager struct {
	ID        string `json:"id"`
	Slots     int    `json:"slotsNumber"`
	FreeSlots int    `json:"freeSlots"`
}

// TaskManagers lists the registered TaskManagers.
func (f *Flink) TaskManagers(ctx context.Context) ([]TaskManager, error) {
	var resp struct {
		TaskManagers []TaskManager `json:"taskmanagers"`
	}
	if err := f.get(ctx, "/taskmanagers", &resp); err != nil {
		return nil, err
	}
	return resp.TaskManagers, nil
}

// StopWithSavepoint stops the job with a savepoint in targetDir, without
// draining (spec §10.11), and waits until it completes or ctx ends. It
// returns the savepoint location.
func (f *Flink) StopWithSavepoint(ctx context.Context, jid, targetDir string, interval time.Duration) (string, error) {
	r, err := HTTPPostJSON(ctx, f.base+"/jobs/"+jid+"/stop", map[string]any{"targetDirectory": targetDir, "drain": false})
	if err != nil {
		return "", fmt.Errorf("flink REST at %s: %w", f.base, err)
	}
	if r.Status != http.StatusAccepted && r.Status != http.StatusOK {
		return "", fmt.Errorf("flink REST at %s: POST /jobs/%s/stop returned %d: %s", f.base, jid, r.Status, truncate(string(r.Body), 300))
	}
	var trig struct {
		RequestID string `json:"request-id"`
	}
	if err := r.JSON(&trig); err != nil {
		return "", err
	}
	if trig.RequestID == "" {
		return "", errors.New("flink REST: stop returned no request-id")
	}
	if interval <= 0 {
		interval = time.Second
	}
	for {
		var st struct {
			Status struct {
				ID string `json:"id"`
			} `json:"status"`
			Operation *struct {
				Location     string `json:"location"`
				FailureCause *struct {
					Class      string `json:"class"`
					StackTrace string `json:"stack-trace"`
				} `json:"failure-cause"`
			} `json:"operation"`
		}
		if err := f.get(ctx, "/jobs/"+jid+"/savepoints/"+trig.RequestID, &st); err != nil {
			if ctx.Err() != nil {
				return "", fmt.Errorf("savepoint of job %s not completed: %w", jid, ctx.Err())
			}
			return "", err
		}
		if st.Status.ID == "COMPLETED" {
			if st.Operation == nil {
				return "", errors.New("savepoint completed without an operation result")
			}
			if st.Operation.FailureCause != nil {
				return "", fmt.Errorf("savepoint failed: %s: %s", st.Operation.FailureCause.Class,
					truncate(st.Operation.FailureCause.StackTrace, 500))
			}
			return st.Operation.Location, nil
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("savepoint of job %s not completed: %w", jid, ctx.Err())
		case <-time.After(interval):
		}
	}
}
