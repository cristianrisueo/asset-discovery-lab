package main

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cristianrisueo/asset-discovery-lab/labkit"
)

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

// downConfig points every endpoint at a closed local port.
func downConfig(t *testing.T) labkit.Config {
	addr := closedPort(t)
	return labkit.ConfigFromLookup(func(k string) (string, bool) {
		switch k {
		case "ADL_KAFKA", "ADL_DRAGONFLY", "ADL_DATACORE":
			return addr, true
		case "ADL_DORIS_DSN":
			return "root:@tcp(" + addr + ")/inventory", true
		case "ADL_ICEBERG_REST", "ADL_S3_ENDPOINT", "ADL_FLINK", "ADL_EXPLORER", "ADL_WEB", "ADL_ADAPTER_HTTP", "ADL_DATACORE_HTTP":
			return "http://" + addr, true
		}
		return "", false
	})
}

func runCmd(t *testing.T, cfg labkit.Config, args ...string) (int, string, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var out, errb bytes.Buffer
	code := run(ctx, args, cfg, &out, &errb)
	return code, out.String(), errb.String()
}

func TestUsageErrors(t *testing.T) {
	cfg := downConfig(t)
	cases := [][]string{
		{"nope"},
		{"generator"},
		{"generator", "rate"},
		{"generator", "rate", "abc"},
		{"generator", "rate", "501"},
		{"generator", "jump"},
		{"chaos"},
		{"chaos", "maybe"},
		{"burst"},
		{"burst", "0"},
		{"burst", "10001"},
		{"move", "LT-0042"},
		{"move", "XX-1", "Madrid"},
		{"reconcile", "--bogus"},
		{"reconcile", "extra"},
		{"flink"},
		{"flink", "cancel"},
		{"stats", "x"},
		{"wait", "--timeout", "soon"},
	}
	for _, args := range cases {
		code, _, stderr := runCmd(t, cfg, args...)
		if code != 2 || !strings.Contains(stderr, "usage") {
			t.Errorf("%v: code %d, stderr %q", args, code, stderr)
		}
	}
	if code, out, _ := runCmd(t, cfg, "help"); code != 0 || !strings.Contains(out, "stop-with-savepoint") {
		t.Errorf("help: %d %q", code, out)
	}
	if code, _, _ := runCmd(t, cfg); code != 2 {
		t.Errorf("no arguments should exit 2, got %d", code)
	}
}

// Every command fails cleanly (exit 1, one message naming the target) when
// its target is not running.
func TestCommandsFailCleanlyWhenTargetDown(t *testing.T) {
	cfg := downConfig(t)
	cases := []struct {
		args   []string
		target string
	}{
		{[]string{"generator", "pause"}, "datacore"},
		{[]string{"generator", "resume"}, "datacore"},
		{[]string{"generator", "rate", "20"}, "datacore"},
		{[]string{"chaos", "on"}, "datacore"},
		{[]string{"chaos", "off"}, "datacore"},
		{[]string{"chaos", "show"}, "datacore"},
		{[]string{"burst", "10"}, "datacore"},
		{[]string{"move", "LT-0042", "Madrid"}, "datacore"},
		{[]string{"reconcile", "--timeout", "1s"}, "datacore"},
		{[]string{"reconcile", "--timeout", "1s", "--no-pause"}, "datacore"},
		{[]string{"flink", "stop-with-savepoint", "--timeout", "1s"}, "flink REST"},
		{[]string{"stats"}, "kafka"},
	}
	for _, c := range cases {
		start := time.Now()
		code, _, stderr := runCmd(t, cfg, c.args...)
		if code != 1 {
			t.Errorf("%v: exit %d, want 1 (stderr %q)", c.args, code, stderr)
		}
		if !strings.Contains(stderr, "adlctl: ") || !strings.Contains(stderr, c.target) {
			t.Errorf("%v: stderr %q should name %q", c.args, stderr, c.target)
		}
		if strings.Contains(stderr, "panic") {
			t.Errorf("%v: panicked", c.args)
		}
		if time.Since(start) > 45*time.Second {
			t.Errorf("%v: took %v", c.args, time.Since(start))
		}
	}
}

func TestChaosConfigs(t *testing.T) {
	on, ok := chaosConfigFor("on")
	if !ok || on.GetDuplicateRate() != 0.05 || on.GetMalformedRate() != 0.01 || on.GetPhantomRate() != 0.01 ||
		on.GetStaleReadRate() != 0.05 || on.GetStaleWindowMs() != 300 || on.GetUnavailableRate() != 0.05 {
		t.Errorf("chaos on must match spec §7.9: %v", on)
	}
	off, ok := chaosConfigFor("off")
	if !ok || off.GetDuplicateRate() != 0 || off.GetStaleWindowMs() != 0 || off.GetUnavailableRate() != 0 {
		t.Errorf("chaos off must zero every knob: %v", off)
	}
	if _, ok := chaosConfigFor("show"); ok {
		t.Error("show is not a configuration")
	}
	if !strings.Contains(formatChaos(on), "window 300 ms") {
		t.Error(formatChaos(on))
	}
}

// With a compose file that defines no services, wait succeeds immediately.
func TestWaitNoServices(t *testing.T) {
	if _, err := os.Stat("/var/run/docker.sock"); err != nil {
		if _, err2 := os.Stat(filepath.Join(os.Getenv("HOME"), ".docker", "run", "docker.sock")); err2 != nil {
			t.Skip("docker is not available on this machine")
		}
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "docker-compose.yml"), []byte("name: adl-test-empty\nservices: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := downConfig(t)
	cfg.RepoRoot = root
	code, out, stderr := runCmd(t, cfg, "wait", "--timeout", "30s")
	if code != 0 || !strings.Contains(out, "no services defined") {
		t.Fatalf("code %d out %q stderr %q", code, out, stderr)
	}
}

func TestWaitWithoutRepoRoot(t *testing.T) {
	cfg := downConfig(t)
	cfg.RepoRoot = filepath.Join(t.TempDir(), "missing")
	code, _, stderr := runCmd(t, cfg, "wait", "--timeout", "1s")
	if code != 1 || !strings.Contains(stderr, "adlctl:") {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
}
