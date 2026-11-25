//go:build acceptance

package acceptance

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/cristianrisueo/asset-discovery-lab/labkit"
)

// Gate C: lake (Iceberg, RustFS, Doris reading the lake). Spec §16.4.

var gateCServices = append(append([]string(nil), gateBServices...), labkit.CompIcebergREST, labkit.CompRustFS)

// loadAssetChanges loads inventory.asset_changes from the REST catalog.
func (h *harness) loadAssetChanges() *labkit.TableMetadata {
	h.t.Helper()
	var md *labkit.TableMetadata
	h.eventually(TShort, "GET /v1/namespaces/inventory/tables/asset_changes returns 200", func(ctx context.Context) (bool, string, error) {
		status, m, err := h.lab.Catalog.LoadTable(ctx, "inventory", "asset_changes")
		if err != nil {
			return false, "", err
		}
		if status != http.StatusOK {
			return false, fmt.Sprintf("status %d", status), nil
		}
		md = m
		return true, "", nil
	})
	return md
}

func TestGateC_01_TableExists(t *testing.T) {
	h := newHarness(t, labkit.CompIcebergREST)
	md := h.loadAssetChanges()

	want := []struct {
		name     string
		typ      string
		required bool
	}{
		{"id", "string", true},
		{"hostname", "string", true},
		{"kind", "string", true},
		{"os", "string", true},
		{"site", "string", true},
		{"owner_user_id", "string", false},
		{"version", "long", true},
		{"changed_at", "timestamptz", true},
		{"ingested_at", "timestamptz", true},
	}
	if len(md.Fields) != len(want) {
		t.Fatalf("schema has %d fields, want %d: %+v", len(md.Fields), len(want), md.Fields)
	}
	byName := map[string]labkit.TableField{}
	for _, f := range md.Fields {
		byName[f.Name] = f
	}
	for _, w := range want {
		f, ok := byName[w.name]
		if !ok {
			t.Errorf("field %s missing from the schema", w.name)
			continue
		}
		if f.Type != w.typ || f.Required != w.required {
			t.Errorf("field %s: type %s required %v, want type %s required %v", w.name, f.Type, f.Required, w.typ, w.required)
		}
	}
	if len(md.PartitionFields) != 1 {
		t.Fatalf("partition spec has %d fields, want day(changed_at): %+v", len(md.PartitionFields), md.PartitionFields)
	}
	pf := md.PartitionFields[0]
	src, ok := md.FieldByID(pf.SourceID)
	if pf.Transform != "day" || !ok || src.Name != "changed_at" {
		t.Errorf("partition field %+v (source %q), want day(changed_at)", pf, src.Name)
	}
	if md.FormatVersion != 2 {
		t.Errorf("format version %d, want 2", md.FormatVersion)
	}
}

func TestGateC_02_ChangesLandInLake(t *testing.T) {
	h := newHarness(t, gateCServices...)
	a1 := h.upsertAsset(labkit.NewAssetRequest("LT-9201", "Alicante", ""))
	h.waitDorisAsset("LT-9201", a1.GetVersion(), TPipe)
	a2 := h.changeSite(a1, "Madrid")
	h.eventually(TLake, fmt.Sprintf("lake rows of LT-9201 at v%d (Alicante) and v%d (Madrid)", a1.GetVersion(), a2.GetVersion()),
		func(ctx context.Context) (bool, string, error) {
			rows, err := h.lab.Doris.LakeVersions(ctx, "LT-9201")
			if err != nil {
				return false, "", err
			}
			has := func(v int64, site string) bool {
				for _, r := range rows {
					if r.Version == v && r.Site == site {
						return true
					}
				}
				return false
			}
			return has(a1.GetVersion(), "Alicante") && has(a2.GetVersion(), "Madrid"), fmt.Sprintf("rows %+v", rows), nil
		})
}

func TestGateC_03_ParquetInRustFS(t *testing.T) {
	h := newHarness(t, labkit.CompIcebergREST, labkit.CompRustFS)
	md := h.loadAssetChanges()
	bucket, prefix, err := labkit.ParseS3URI(md.Location)
	h.fatalIf(err, "table location %q", md.Location)
	prefix = strings.TrimSuffix(prefix, "/") + "/"
	h.eventually(TLake, fmt.Sprintf("Parquet data and metadata JSON under s3://%s/%s", bucket, prefix), func(ctx context.Context) (bool, string, error) {
		keys, err := h.lab.S3.ListKeys(ctx, bucket, prefix)
		if err != nil {
			return false, "", err
		}
		parquet, metadata := 0, 0
		for _, k := range keys {
			rel := strings.TrimPrefix(k, prefix)
			if strings.HasPrefix(rel, "data/") && strings.HasSuffix(rel, ".parquet") {
				parquet++
			}
			if strings.HasPrefix(rel, "metadata/") && strings.HasSuffix(rel, ".metadata.json") {
				metadata++
			}
		}
		return parquet > 0 && metadata > 0, fmt.Sprintf("%d objects: %d parquet under data/, %d .metadata.json under metadata/", len(keys), parquet, metadata), nil
	})
}

func TestGateC_04_NoDuplicatesInLake(t *testing.T) {
	h := newHarness(t, labkit.CompDorisFE, labkit.CompDorisBE, labkit.CompIcebergREST, labkit.CompRustFS)
	var dups []labkit.IDVersionCount
	h.eventually(TShort, "query duplicates in lake.inventory.asset_changes", func(ctx context.Context) (bool, string, error) {
		d, err := h.lab.Doris.LakeDuplicates(ctx)
		if err != nil {
			return false, "", err
		}
		dups = d
		return true, "", nil
	})
	if len(dups) > 0 {
		t.Fatalf("%d duplicated (id, version) pairs in the lake, for example %+v", len(dups), dups[0])
	}
}

func TestGateC_05_LakeReconciled(t *testing.T) {
	h := newHarness(t, gateCServices...)
	h.pauseGenerator()
	h.reconcile("lake reconcile", labkit.ReconcileOptions{Timeout: TLake + TReconcile, NoDoris: true, NoPause: true})
}
