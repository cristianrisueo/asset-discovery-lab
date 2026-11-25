package labkit

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
)

func TestComputeLag(t *testing.T) {
	end := Offsets{0: 10, 1: 5, 2: 7}
	start := Offsets{0: 0, 1: 2, 2: 0}
	committed := Offsets{0: 10, 2: 3}
	got := ComputeLag(end, start, committed)
	if got[0] != 0 || got[1] != 3 || got[2] != 4 {
		t.Fatalf("lag %v", got)
	}
	if ComputeLag(Offsets{0: 5}, Offsets{0: 0}, Offsets{0: 9})[0] != 0 {
		t.Error("negative lag must clamp to 0")
	}
	if ComputeLag(Offsets{0: 5}, Offsets{0: 1}, Offsets{0: -1})[0] != 4 {
		t.Error("-1 means nothing committed")
	}
}

func TestParseBackends(t *testing.T) {
	cols := []string{"BackendId", "Host", "HeartbeatPort", "Alive"}
	got, err := ParseBackends(cols, [][]string{{"10001", "172.30.0.3", "9050", "true"}, {"10002", "x", "1", "false"}})
	if err != nil || len(got) != 2 || got[0].Host != "172.30.0.3" || !got[0].Alive || got[1].Alive {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := ParseBackends([]string{"a"}, nil); err == nil {
		t.Error("missing columns should fail")
	}
}

func TestDuplicateWindows(t *testing.T) {
	w := time.Date(2026, 11, 25, 10, 14, 0, 0, time.UTC)
	rows := []ChangeRateRow{
		{WindowStart: w, EntityType: "asset"}, {WindowStart: w, EntityType: "user"},
		{WindowStart: w, EntityType: "asset"}, {WindowStart: w, EntityType: "asset"},
	}
	d := DuplicateWindows(rows)
	if len(d) != 1 || d[0].EntityType != "asset" {
		t.Fatalf("%v", d)
	}
}

// Clients against a closed port fail quickly with an error naming the target.
func TestClientsUnreachable(t *testing.T) {
	addr := closedPort(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	k, err := NewKafka(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	if err := k.Ping(ctx); err == nil || !strings.Contains(err.Error(), "kafka at") {
		t.Errorf("kafka: %v", err)
	}
	d, err := NewDatacore(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	if err := d.Ping(ctx); err == nil || !strings.Contains(err.Error(), "datacore admin API at") {
		t.Errorf("datacore: %v", err)
	}
	db, err := NewDoris("root:@tcp(" + addr + ")/inventory")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.Ping(ctx); err == nil || !strings.Contains(err.Error(), "doris at") {
		t.Errorf("doris: %v", err)
	}
	df := NewDragonfly(addr)
	defer func() { _ = df.Close() }()
	if err := df.Ping(ctx); err == nil || !strings.Contains(err.Error(), "dragonfly at") {
		t.Errorf("dragonfly: %v", err)
	}
	if err := NewCatalog("http://" + addr).Ping(ctx); err == nil || !strings.Contains(err.Error(), "iceberg REST catalog at") {
		t.Errorf("catalog: %v", err)
	}
	if _, err := NewS3("http://"+addr, "a", "b", "us-east-1").ListKeys(ctx, "warehouse", ""); err == nil || !strings.Contains(err.Error(), "s3 at") {
		t.Errorf("s3: %v", err)
	}
	if time.Until(deadlineOf(ctx)) <= 0 {
		t.Error("clients took too long to fail")
	}
}

func deadlineOf(ctx context.Context) time.Time { d, _ := ctx.Deadline(); return d }

func TestRandomHelpers(t *testing.T) {
	u := NewUUID()
	if len(u) != 36 || u[14] != '4' {
		t.Errorf("uuid %q", u)
	}
	for i := 0; i < 200; i++ {
		b := UndecodableBytes(16, 64, &adlv1.Trigger{})
		if len(b) < 16 || len(b) > 64 {
			t.Fatalf("length %d", len(b))
		}
		if proto.Unmarshal(b, &adlv1.Trigger{}) == nil {
			t.Fatal("bytes decode as a Trigger")
		}
	}
}
