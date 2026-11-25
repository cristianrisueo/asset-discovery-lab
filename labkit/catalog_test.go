package labkit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const loadTableBody = `{
  "metadata-location": "s3://warehouse/inventory/asset_changes/metadata/00001.metadata.json",
  "metadata": {
    "format-version": 2,
    "location": "s3://warehouse/inventory/asset_changes",
    "current-schema-id": 1,
    "schemas": [
      {"schema-id": 0, "fields": [{"id": 1, "name": "old", "required": true, "type": "string"}]},
      {"schema-id": 1, "fields": [
        {"id": 1, "name": "id", "required": true, "type": "string"},
        {"id": 8, "name": "changed_at", "required": true, "type": "timestamptz"},
        {"id": 6, "name": "owner_user_id", "required": false, "type": "string"},
        {"id": 10, "name": "tags", "required": false, "type": {"type": "list", "element-id": 11, "element": "string", "element-required": false}}
      ]}
    ],
    "default-spec-id": 0,
    "partition-specs": [{"spec-id": 0, "fields": [{"name": "changed_at_day", "transform": "day", "source-id": 8, "field-id": 1000}]}]
  }
}`

func TestParseLoadTable(t *testing.T) {
	md, err := ParseLoadTable([]byte(loadTableBody))
	if err != nil {
		t.Fatal(err)
	}
	if md.FormatVersion != 2 || md.Location != "s3://warehouse/inventory/asset_changes" || len(md.Fields) != 4 {
		t.Fatalf("%+v", md)
	}
	if f, ok := md.FieldByID(8); !ok || f.Name != "changed_at" || f.Type != "timestamptz" || !f.Required {
		t.Errorf("field 8: %+v", f)
	}
	if f, _ := md.FieldByID(10); f.Type[0] != '{' {
		t.Errorf("nested type should keep raw JSON, got %q", f.Type)
	}
	if len(md.PartitionFields) != 1 || md.PartitionFields[0].Transform != "day" || md.PartitionFields[0].SourceID != 8 {
		t.Errorf("partition: %+v", md.PartitionFields)
	}
	if _, err := ParseLoadTable([]byte(`{"metadata":{"current-schema-id":3,"schemas":[]}}`)); err == nil {
		t.Error("missing current schema should fail")
	}
}

func TestCatalogLoadTable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/config":
			_, _ = w.Write([]byte(`{"defaults":{},"overrides":{}}`))
		case "/v1/namespaces/inventory/tables/asset_changes":
			_, _ = w.Write([]byte(loadTableBody))
		default:
			http.Error(w, `{"error":{"code":404}}`, http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := NewCatalog(srv.URL + "/")
	if err := c.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, md, err := c.LoadTable(context.Background(), "inventory", "asset_changes")
	if err != nil || st != 200 || md == nil {
		t.Fatalf("st=%d md=%v err=%v", st, md, err)
	}
	st, md, err = c.LoadTable(context.Background(), "inventory", "nope")
	if err != nil || st != 404 || md != nil {
		t.Fatalf("missing table: st=%d md=%v err=%v", st, md, err)
	}
}

func TestParseS3URI(t *testing.T) {
	b, p, err := ParseS3URI("s3://warehouse/inventory/asset_changes")
	if err != nil || b != "warehouse" || p != "inventory/asset_changes" {
		t.Fatalf("%q %q %v", b, p, err)
	}
	if _, _, err := ParseS3URI("http://x/y"); err == nil {
		t.Error("non-s3 URI should fail")
	}
}
