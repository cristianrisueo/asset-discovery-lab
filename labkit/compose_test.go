package labkit

import (
	"strings"
	"testing"
)

func TestParsePSJSON(t *testing.T) {
	ndjson := `{"Name":"adl-kafka-1","Service":"kafka","State":"running","Health":"healthy","ExitCode":0}
{"Name":"adl-kafka-init-1","Service":"kafka-init","State":"exited","Health":"","ExitCode":0}`
	got, err := ParsePSJSON([]byte(ndjson))
	if err != nil || len(got) != 2 || got[1].Service != "kafka-init" || got[0].Health != "healthy" {
		t.Fatalf("ndjson: %+v %v", got, err)
	}
	arr := `[{"Name":"a","Service":"a","State":"running"}]`
	got, err = ParsePSJSON([]byte(arr))
	if err != nil || len(got) != 1 {
		t.Fatalf("array: %+v %v", got, err)
	}
	got, err = ParsePSJSON([]byte("  \n"))
	if err != nil || got != nil {
		t.Fatalf("empty: %+v %v", got, err)
	}
	if _, err := ParsePSJSON([]byte("{oops")); err == nil {
		t.Fatal("bad JSON should fail")
	}
}

func TestParseConfigJSON(t *testing.T) {
	cfg := `{"name":"adl","services":{
	  "kafka":{"restart":"unless-stopped","healthcheck":{"test":["CMD","x"]}},
	  "kafka-init":{"restart":"no"},
	  "flink-taskmanager":{"restart":"unless-stopped"},
	  "doris-init":{},
	  "weird":{"restart":"always","healthcheck":{"test":["NONE"]}}
	}}`
	got, err := ParseConfigJSON([]byte(cfg))
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]ServiceConfig{}
	for _, s := range got {
		by[s.Name] = s
	}
	if got[0].Name != "doris-init" {
		t.Errorf("services should be sorted: %+v", got)
	}
	if !by["kafka"].HasHealthcheck || by["kafka"].OneShot() {
		t.Errorf("kafka: %+v", by["kafka"])
	}
	if !by["kafka-init"].OneShot() || !by["doris-init"].OneShot() || by["flink-taskmanager"].OneShot() {
		t.Error("one-shot detection")
	}
	if by["weird"].HasHealthcheck {
		t.Error("NONE healthcheck counts as none")
	}
	empty, err := ParseConfigJSON([]byte(`{"name":"adl","services":{}}`))
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty: %v %v", empty, err)
	}
}

func TestEvaluateServices(t *testing.T) {
	services := []ServiceConfig{
		{Name: "kafka", Restart: "unless-stopped", HasHealthcheck: true},
		{Name: "kafka-init", Restart: "no"},
		{Name: "flink-taskmanager", Restart: "unless-stopped"},
		{Name: "doris-init"},
		{Name: "datacore", Restart: "unless-stopped", HasHealthcheck: true},
		{Name: "adapter", Restart: "unless-stopped", HasHealthcheck: true},
	}
	containers := []ContainerStatus{
		{Service: "kafka", State: "running", Health: "healthy"},
		{Service: "kafka-init", State: "exited", ExitCode: 0},
		{Service: "flink-taskmanager", State: "running"},
		{Service: "doris-init", State: "exited", ExitCode: 1},
		{Service: "datacore", State: "running", Health: "starting"},
	}
	rows := EvaluateServices(services, containers)
	want := map[string]bool{"kafka": true, "kafka-init": true, "flink-taskmanager": true, "doris-init": false, "datacore": false, "adapter": false}
	for _, r := range rows {
		if r.Ready != want[r.Component] {
			t.Errorf("%s ready=%v (%s)", r.Component, r.Ready, r.Status)
		}
	}
	if AllReady(rows) {
		t.Error("not all ready")
	}
	if f := FailedOneShot(rows); len(f) != 1 || f[0] != "doris-init" {
		t.Errorf("failed one-shots: %v", f)
	}
	table := FormatWaitTable(rows)
	if !strings.Contains(table, "COMPONENT") || !strings.Contains(table, "not created") || !strings.Contains(table, "failed (exit 1)") {
		t.Errorf("table:\n%s", table)
	}
	if !AllReady(nil) {
		t.Error("no services means ready")
	}
}
