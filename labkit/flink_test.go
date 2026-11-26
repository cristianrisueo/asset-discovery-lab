package labkit

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func flinkServer(t *testing.T, routes map[string]func(w http.ResponseWriter, r *http.Request)) *Flink {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		if r.URL.RawQuery != "" {
			key += "?" + r.URL.RawQuery
		}
		h, ok := routes[key]
		if !ok {
			http.Error(w, "no route "+key, http.StatusNotFound)
			return
		}
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return NewFlink(srv.URL)
}

func jsonReply(v any) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
}

func TestPickJob(t *testing.T) {
	jobs := []JobSummary{
		{ID: "old", Name: JobName, State: "FINISHED", StartTime: 300},
		{ID: "run", Name: JobName, State: "RUNNING", StartTime: 100},
		{ID: "other", Name: "x", State: "RUNNING", StartTime: 500},
	}
	if j := PickJob(jobs, JobName); j == nil || j.ID != "run" {
		t.Fatalf("active job should win: %+v", j)
	}
	if j := PickJob(jobs[:1], JobName); j == nil || j.ID != "old" {
		t.Fatalf("inactive job should be returned when alone: %+v", j)
	}
	if PickJob(nil, JobName) != nil {
		t.Fatal("no job expected")
	}
}

func TestFlinkJobAndMetrics(t *testing.T) {
	f := flinkServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /jobs/overview":               jsonReply(map[string]any{"jobs": []map[string]any{{"jid": "j1", "name": JobName, "state": "RUNNING", "start-time": 1764065643000}}}),
		"GET /jobs/j1":                     jsonReply(map[string]any{"vertices": []map[string]any{{"id": "v1", "name": "Source"}, {"id": "v2", "name": "VersionGate -> Map"}}}),
		"GET /jobs/j1/vertices/v1/metrics": jsonReply([]map[string]string{{"id": "0.numRecordsIn"}}),
		"GET /jobs/j1/vertices/v2/metrics": jsonReply([]map[string]string{
			{"id": "0.VersionGate.versionGateDropped"}, {"id": "1.VersionGate.versionGateDropped"},
			{"id": "0.VersionGate.versionGateAccepted"}, {"id": "0.Other.versionGateDropped"},
		}),
		"GET /jobs/j1/vertices/v2/metrics?get=0.VersionGate.versionGateDropped,1.VersionGate.versionGateDropped": jsonReply([]map[string]string{
			{"id": "0.VersionGate.versionGateDropped", "value": "4"}, {"id": "1.VersionGate.versionGateDropped", "value": "6.0"},
		}),
		"GET /jobs/j1/checkpoints": jsonReply(map[string]any{"latest": map[string]any{"restored": map[string]any{
			"id": 12, "restore_timestamp": 1, "is_savepoint": true, "external_path": "file:/flink-data/savepoints/savepoint-abc"}}}),
	})
	ctx := context.Background()
	j, err := f.JobByName(ctx, JobName)
	if err != nil || j == nil || j.ID != "j1" || j.Started().Year() != 2025 {
		t.Fatalf("job %+v err %v", j, err)
	}
	sum, found, err := f.SumOperatorMetric(ctx, "j1", "VersionGate", "versionGateDropped")
	if err != nil || !found || sum != 10 {
		t.Fatalf("sum=%v found=%v err=%v", sum, found, err)
	}
	r, err := f.LatestRestored(ctx, "j1")
	if err != nil || r == nil || !r.IsSavepoint || !strings.Contains(r.ExternalPath, "savepoint-abc") {
		t.Fatalf("restored %+v err %v", r, err)
	}
}

func TestFlinkStopWithSavepoint(t *testing.T) {
	polls := 0
	f := flinkServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"POST /jobs/j1/stop": func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(b, &body)
			if body["targetDirectory"] != "file:///flink-data/savepoints" || body["drain"] != false {
				http.Error(w, "bad body "+string(b), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"request-id":"r1"}`))
		},
		"GET /jobs/j1/savepoints/r1": func(w http.ResponseWriter, r *http.Request) {
			polls++
			if polls < 2 {
				jsonReply(map[string]any{"status": map[string]string{"id": "IN_PROGRESS"}})(w, r)
				return
			}
			jsonReply(map[string]any{"status": map[string]string{"id": "COMPLETED"}, "operation": map[string]string{"location": "file:/flink-data/savepoints/savepoint-1"}})(w, r)
		},
	})
	loc, err := f.StopWithSavepoint(context.Background(), "j1", "file:///flink-data/savepoints", time.Millisecond)
	if err != nil || loc != "file:/flink-data/savepoints/savepoint-1" {
		t.Fatalf("loc=%q err=%v", loc, err)
	}
}

func TestFlinkStopWithSavepointFailureAndTimeout(t *testing.T) {
	f := flinkServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"POST /jobs/j1/stop":         jsonReply(map[string]string{"request-id": "r1"}),
		"GET /jobs/j1/savepoints/r1": jsonReply(map[string]any{"status": map[string]string{"id": "COMPLETED"}, "operation": map[string]any{"failure-cause": map[string]string{"class": "java.lang.Exception", "stack-trace": "boom"}}}),
		"POST /jobs/j2/stop":         jsonReply(map[string]string{"request-id": "r2"}),
		"GET /jobs/j2/savepoints/r2": jsonReply(map[string]any{"status": map[string]string{"id": "IN_PROGRESS"}}),
	})
	if _, err := f.StopWithSavepoint(context.Background(), "j1", "file:///x", time.Millisecond); err == nil || !strings.Contains(err.Error(), "savepoint failed") {
		t.Errorf("failure cause should surface, got %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := f.StopWithSavepoint(ctx, "j2", "file:///x", 5*time.Millisecond); err == nil || !strings.Contains(err.Error(), "not completed") {
		t.Errorf("timeout should surface, got %v", err)
	}
}

func TestFlinkUnreachable(t *testing.T) {
	f := NewFlink("http://" + closedPort(t))
	err := f.Ping(context.Background())
	if err == nil || !strings.Contains(err.Error(), "flink REST at") {
		t.Fatalf("expected a named connection error, got %v", err)
	}
}

func TestFlinkSettledJobWaitsThroughTransientStates(t *testing.T) {
	states := []string{"RESTARTING", "INITIALIZING", "RUNNING"}
	calls := 0
	f := flinkServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /jobs/overview": func(w http.ResponseWriter, r *http.Request) {
			st := states[min(calls, len(states)-1)]
			calls++
			jsonReply(map[string]any{"jobs": []map[string]any{{"jid": "j1", "name": JobName, "state": st, "start-time": 1}}})(w, r)
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	j, err := f.SettledJob(ctx, JobName, 5*time.Millisecond)
	if err != nil || j == nil || j.State != "RUNNING" || calls != 3 {
		t.Fatalf("SettledJob = %+v, %v after %d calls; want RUNNING after 3", j, err, calls)
	}
}

func TestFlinkSettledJobTimeoutAndTerminal(t *testing.T) {
	state := "RESTARTING"
	f := flinkServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /jobs/overview": func(w http.ResponseWriter, r *http.Request) {
			jsonReply(map[string]any{"jobs": []map[string]any{{"jid": "j1", "name": JobName, "state": state, "start-time": 1}}})(w, r)
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	j, err := f.SettledJob(ctx, JobName, 5*time.Millisecond)
	if err == nil || j == nil || j.State != "RESTARTING" {
		t.Fatalf("SettledJob = %+v, %v; want the RESTARTING job and a timeout", j, err)
	}
	state = "FINISHED"
	j, err = f.SettledJob(context.Background(), JobName, 5*time.Millisecond)
	if err != nil || j == nil || j.State != "FINISHED" {
		t.Fatalf("SettledJob = %+v, %v; want the FINISHED job at once", j, err)
	}
}
