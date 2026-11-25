package labkit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Catalog is a client for the Iceberg REST catalog (spec §12.2).
type Catalog struct {
	base string
}

// NewCatalog builds a client for base, for example http://localhost:8181.
func NewCatalog(base string) *Catalog { return &Catalog{base: strings.TrimRight(base, "/")} }

// TableField is a field of the current schema.
type TableField struct {
	ID       int
	Name     string
	Type     string // primitive name, or the raw JSON of a nested type
	Required bool
}

// PartitionField is a field of the default partition spec.
type PartitionField struct {
	SourceID  int
	FieldID   int
	Name      string
	Transform string
}

// TableMetadata is the part of a LoadTable response the tests need.
type TableMetadata struct {
	FormatVersion   int
	Location        string
	Fields          []TableField
	PartitionFields []PartitionField
}

// FieldByID returns the schema field with id, if any.
func (m *TableMetadata) FieldByID(id int) (TableField, bool) {
	for _, f := range m.Fields {
		if f.ID == id {
			return f, true
		}
	}
	return TableField{}, false
}

// Ping calls GET /v1/config.
func (c *Catalog) Ping(ctx context.Context) error {
	r, err := HTTPGet(ctx, c.base+"/v1/config")
	if err != nil {
		return fmt.Errorf("iceberg REST catalog at %s: %w", c.base, err)
	}
	if r.Status != http.StatusOK {
		return fmt.Errorf("iceberg REST catalog at %s: GET /v1/config returned %d", c.base, r.Status)
	}
	return nil
}

// LoadTable calls GET /v1/namespaces/{ns}/tables/{table}. It returns the HTTP
// status, and the parsed metadata when the status is 200.
func (c *Catalog) LoadTable(ctx context.Context, namespace, table string) (int, *TableMetadata, error) {
	u := fmt.Sprintf("%s/v1/namespaces/%s/tables/%s", c.base, url.PathEscape(namespace), url.PathEscape(table))
	r, err := HTTPGet(ctx, u)
	if err != nil {
		return 0, nil, fmt.Errorf("iceberg REST catalog at %s: %w", c.base, err)
	}
	if r.Status != http.StatusOK {
		return r.Status, nil, nil
	}
	md, err := ParseLoadTable(r.Body)
	return r.Status, md, err
}

// ParseLoadTable parses a LoadTable response body.
func ParseLoadTable(body []byte) (*TableMetadata, error) {
	var resp struct {
		Metadata struct {
			FormatVersion   int    `json:"format-version"`
			Location        string `json:"location"`
			CurrentSchemaID int    `json:"current-schema-id"`
			Schemas         []struct {
				SchemaID int `json:"schema-id"`
				Fields   []struct {
					ID       int             `json:"id"`
					Name     string          `json:"name"`
					Required bool            `json:"required"`
					Type     json.RawMessage `json:"type"`
				} `json:"fields"`
			} `json:"schemas"`
			DefaultSpecID  int `json:"default-spec-id"`
			PartitionSpecs []struct {
				SpecID int `json:"spec-id"`
				Fields []struct {
					Name      string `json:"name"`
					Transform string `json:"transform"`
					SourceID  int    `json:"source-id"`
					FieldID   int    `json:"field-id"`
				} `json:"fields"`
			} `json:"partition-specs"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse LoadTable response: %w", err)
	}
	m := resp.Metadata
	md := &TableMetadata{FormatVersion: m.FormatVersion, Location: m.Location}
	found := false
	for _, s := range m.Schemas {
		if s.SchemaID != m.CurrentSchemaID {
			continue
		}
		found = true
		for _, f := range s.Fields {
			typ := string(f.Type)
			var name string
			if json.Unmarshal(f.Type, &name) == nil {
				typ = name
			}
			md.Fields = append(md.Fields, TableField{ID: f.ID, Name: f.Name, Type: typ, Required: f.Required})
		}
	}
	if !found {
		return nil, fmt.Errorf("parse LoadTable response: current schema %d not found", m.CurrentSchemaID)
	}
	for _, s := range m.PartitionSpecs {
		if s.SpecID != m.DefaultSpecID {
			continue
		}
		for _, f := range s.Fields {
			md.PartitionFields = append(md.PartitionFields, PartitionField{
				SourceID: f.SourceID, FieldID: f.FieldID, Name: f.Name, Transform: f.Transform,
			})
		}
	}
	return md, nil
}
