package labkit

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func lookupFrom(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestConfigDefaults(t *testing.T) {
	c := ConfigFromLookup(lookupFrom(nil))
	want := map[string]string{
		"Kafka": DefaultKafka, "Dragonfly": DefaultDragonfly, "Datacore": DefaultDatacore,
		"DorisDSN": DefaultDorisDSN, "IcebergREST": DefaultIcebergREST, "S3Endpoint": DefaultS3Endpoint,
		"Flink": DefaultFlink, "Explorer": DefaultExplorer, "Web": DefaultWeb,
		"AdapterHTTP": DefaultAdapterHTTP, "DatacoreHTTP": DefaultDatacoreHTTP,
		"S3AccessKey": "adl-admin", "S3SecretKey": "adl-admin-secret", "S3Region": "us-east-1",
	}
	got := map[string]string{
		"Kafka": c.Kafka, "Dragonfly": c.Dragonfly, "Datacore": c.Datacore,
		"DorisDSN": c.DorisDSN, "IcebergREST": c.IcebergREST, "S3Endpoint": c.S3Endpoint,
		"Flink": c.Flink, "Explorer": c.Explorer, "Web": c.Web,
		"AdapterHTTP": c.AdapterHTTP, "DatacoreHTTP": c.DatacoreHTTP,
		"S3AccessKey": c.S3AccessKey, "S3SecretKey": c.S3SecretKey, "S3Region": c.S3Region,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if c.AllowMySQLFallback {
		t.Error("AllowMySQLFallback should default to false")
	}
	// The §16.3 defaults are exact.
	if DefaultKafka != "localhost:29092" || DefaultDorisDSN != "root:@tcp(localhost:9030)/inventory?parseTime=true&loc=UTC" {
		t.Error("spec §16.3 defaults changed")
	}
}

func TestConfigOverrides(t *testing.T) {
	c := ConfigFromLookup(lookupFrom(map[string]string{
		"ADL_KAFKA": "k:1", "ADL_EXPLORER": "http://e:2/", "ADL_ALLOW_MYSQL_FALLBACK": "1",
		"S3_ACCESS_KEY": "ak", "ADL_REPO_ROOT": "/tmp/x", "ADL_FLINK": "  ",
	}))
	if c.Kafka != "k:1" || c.Explorer != "http://e:2" || !c.AllowMySQLFallback || c.S3AccessKey != "ak" || c.RepoRoot != "/tmp/x" {
		t.Fatalf("overrides not applied: %+v", c)
	}
	if c.Flink != DefaultFlink {
		t.Errorf("blank value should fall back to the default, got %q", c.Flink)
	}
}

func TestFindRepoRoot(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"docker-compose.yml", "go.mod"} {
		if err := os.WriteFile(filepath.Join(root, f), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	deep := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(deep, 0o750); err != nil {
		t.Fatal(err)
	}
	got, err := FindRepoRoot(deep)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(root)
	gotReal, _ := filepath.EvalSymlinks(got)
	if gotReal != want {
		t.Fatalf("FindRepoRoot = %q, want %q", got, root)
	}
	if _, err := FindRepoRoot(t.TempDir()); err == nil {
		t.Fatal("expected an error outside a repository")
	}
}

func TestEndpoints(t *testing.T) {
	c := ConfigFromLookup(lookupFrom(nil))
	cases := map[string][]string{
		CompKafka:           {"localhost:29092"},
		CompDatacore:        {"localhost:50051", "localhost:8090"},
		CompDorisFE:         {"localhost:9030"},
		CompFlinkJobManager: {"localhost:8081"},
		CompWeb:             {"localhost:3000"},
		CompDorisBE:         nil,
	}
	for comp, want := range cases {
		eps, err := c.Endpoints(comp)
		if err != nil {
			t.Fatalf("%s: %v", comp, err)
		}
		var got []string
		for _, e := range eps {
			got = append(got, e.Addr)
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: got %v, want %v", comp, got, want)
		}
	}
	if _, err := c.Endpoints("nope"); err == nil {
		t.Error("unknown component should fail")
	}
}

func closedPort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func TestCheckReachable(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	up := ConfigFromLookup(lookupFrom(map[string]string{"ADL_EXPLORER": "http://" + l.Addr().String()}))
	if err := up.CheckReachable(context.Background(), CompExplorer, CompDorisBE); err != nil {
		t.Fatalf("listening endpoint reported unreachable: %v", err)
	}
	down := ConfigFromLookup(lookupFrom(map[string]string{"ADL_EXPLORER": "http://" + closedPort(t)}))
	err = down.CheckReachable(context.Background(), CompExplorer)
	if err == nil || !strings.Contains(err.Error(), "explorer") || !strings.Contains(err.Error(), "not reachable") {
		t.Fatalf("expected a not reachable error naming the explorer, got %v", err)
	}
}
