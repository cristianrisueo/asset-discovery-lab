package labkit

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

// Doris is a client for Doris over the MySQL protocol (spec §11).
type Doris struct {
	addr string
	db   *sql.DB
}

// NewDoris builds a client from a go-sql-driver DSN. It forces UTC time
// parsing and client-side parameter interpolation, so queries never depend on
// server-side prepared statements. It does not connect until the first call.
func NewDoris(dsn string) (*Doris, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse Doris DSN: %w", err)
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.InterpolateParams = true
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 2 * time.Minute
	}
	conn, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, fmt.Errorf("doris at %s: %w", cfg.Addr, err)
	}
	db := sql.OpenDB(conn)
	db.SetMaxOpenConns(4)
	db.SetConnMaxIdleTime(time.Minute)
	return &Doris{addr: cfg.Addr, db: db}, nil
}

// Close releases the pool.
func (d *Doris) Close() error { return d.db.Close() }

// DB exposes the pool for ad-hoc queries.
func (d *Doris) DB() *sql.DB { return d.db }

func (d *Doris) wrap(what string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("doris at %s: %s: %w", d.addr, what, err)
}

// Ping runs SELECT 1.
func (d *Doris) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var one int
	return d.wrap("SELECT 1", d.db.QueryRowContext(ctx, "SELECT 1").Scan(&one))
}

// AssetRow is a row of inventory.assets.
type AssetRow struct {
	ID          string
	Hostname    string
	Kind        string
	OS          string
	Site        string
	OwnerUserID sql.NullString
	Version     int64
	ChangedAt   time.Time
	IngestedAt  time.Time
}

// UserRow is a row of inventory.users.
type UserRow struct {
	ID          string
	DisplayName string
	Email       string
	GroupID     sql.NullString
	Version     int64
	ChangedAt   time.Time
	IngestedAt  time.Time
}

// GroupRow is a row of inventory.groups.
type GroupRow struct {
	ID         string
	Name       string
	Version    int64
	ChangedAt  time.Time
	IngestedAt time.Time
}

// EnrichedRow is a row of inventory.v_assets_enriched (spec §11.4).
type EnrichedRow struct {
	ID          string
	Hostname    string
	Kind        string
	OS          string
	Site        string
	OwnerUserID sql.NullString
	OwnerName   sql.NullString
	GroupID     sql.NullString
	GroupName   sql.NullString
	Version     int64
	ChangedAt   time.Time
}

// OSSiteRow is a row of the OS × site aggregate.
type OSSiteRow struct {
	OS     string
	Site   string
	Assets int64
}

// ChangeRateRow is a row of inventory.change_rate_1m.
type ChangeRateRow struct {
	WindowStart time.Time
	WindowEnd   time.Time
	EntityType  string
	Changes     int64
}

// LakeVersion is one version of an asset in the lake.
type LakeVersion struct {
	Version int64
	Site    string
}

// IDVersionCount is a duplicated (id, version) pair in the lake.
type IDVersionCount struct {
	ID      string
	Version int64
	Count   int64
}

// Backend is one row of SHOW BACKENDS.
type Backend struct {
	Host  string
	Alive bool
}

const assetCols = "id, hostname, kind, os, site, owner_user_id, version, changed_at, ingested_at"

func scanAsset(s interface{ Scan(...any) error }) (AssetRow, error) {
	var r AssetRow
	err := s.Scan(&r.ID, &r.Hostname, &r.Kind, &r.OS, &r.Site, &r.OwnerUserID, &r.Version, &r.ChangedAt, &r.IngestedAt)
	return r, err
}

func queryRows[T any](ctx context.Context, d *Doris, what, q string, scan func(interface{ Scan(...any) error }) (T, error), args ...any) ([]T, error) {
	rows, err := d.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, d.wrap(what, err)
	}
	defer func() { _ = rows.Close() }()
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, d.wrap(what, err)
		}
		out = append(out, v)
	}
	return out, d.wrap(what, rows.Err())
}

// Assets reads every row of inventory.assets.
func (d *Doris) Assets(ctx context.Context) ([]AssetRow, error) {
	return queryRows(ctx, d, "read inventory.assets", "SELECT "+assetCols+" FROM inventory.assets", scanAsset)
}

// AssetByID reads one row of inventory.assets, or nil.
func (d *Doris) AssetByID(ctx context.Context, id string) (*AssetRow, error) {
	rs, err := queryRows(ctx, d, "read inventory.assets", "SELECT "+assetCols+" FROM inventory.assets WHERE id = ?", scanAsset, id)
	if err != nil || len(rs) == 0 {
		return nil, err
	}
	return &rs[0], nil
}

// Users reads every row of inventory.users.
func (d *Doris) Users(ctx context.Context) ([]UserRow, error) {
	return queryRows(ctx, d, "read inventory.users",
		"SELECT id, display_name, email, group_id, version, changed_at, ingested_at FROM inventory.users",
		func(s interface{ Scan(...any) error }) (UserRow, error) {
			var r UserRow
			err := s.Scan(&r.ID, &r.DisplayName, &r.Email, &r.GroupID, &r.Version, &r.ChangedAt, &r.IngestedAt)
			return r, err
		})
}

// Groups reads every row of inventory.groups.
func (d *Doris) Groups(ctx context.Context) ([]GroupRow, error) {
	return queryRows(ctx, d, "read inventory.groups",
		"SELECT id, name, version, changed_at, ingested_at FROM inventory.`groups`",
		func(s interface{ Scan(...any) error }) (GroupRow, error) {
			var r GroupRow
			err := s.Scan(&r.ID, &r.Name, &r.Version, &r.ChangedAt, &r.IngestedAt)
			return r, err
		})
}

// EnrichedByID reads one row of inventory.v_assets_enriched, or nil.
func (d *Doris) EnrichedByID(ctx context.Context, id string) (*EnrichedRow, error) {
	rs, err := queryRows(ctx, d, "read inventory.v_assets_enriched",
		"SELECT id, hostname, kind, os, site, owner_user_id, owner_name, group_id, group_name, version, changed_at "+
			"FROM inventory.v_assets_enriched WHERE id = ?",
		func(s interface{ Scan(...any) error }) (EnrichedRow, error) {
			var r EnrichedRow
			err := s.Scan(&r.ID, &r.Hostname, &r.Kind, &r.OS, &r.Site, &r.OwnerUserID, &r.OwnerName,
				&r.GroupID, &r.GroupName, &r.Version, &r.ChangedAt)
			return r, err
		}, id)
	if err != nil || len(rs) == 0 {
		return nil, err
	}
	return &rs[0], nil
}

func scanOSSite(s interface{ Scan(...any) error }) (OSSiteRow, error) {
	var r OSSiteRow
	err := s.Scan(&r.OS, &r.Site, &r.Assets)
	return r, err
}

// MVRows reads inventory.mv_assets_by_os_site ordered by os, site.
func (d *Doris) MVRows(ctx context.Context) ([]OSSiteRow, error) {
	return queryRows(ctx, d, "read inventory.mv_assets_by_os_site",
		"SELECT os, site, assets FROM inventory.mv_assets_by_os_site ORDER BY os, site", scanOSSite)
}

// GroupByOSSite computes the MV's definition directly on inventory.assets.
func (d *Doris) GroupByOSSite(ctx context.Context) ([]OSSiteRow, error) {
	return queryRows(ctx, d, "aggregate inventory.assets",
		"SELECT os, site, COUNT(*) AS assets FROM inventory.assets GROUP BY os, site ORDER BY os, site", scanOSSite)
}

// RefreshMV runs the manual complete refresh of the MV (spec §11.5).
func (d *Doris) RefreshMV(ctx context.Context) error {
	_, err := d.db.ExecContext(ctx, "REFRESH MATERIALIZED VIEW inventory.mv_assets_by_os_site COMPLETE")
	return d.wrap("refresh inventory.mv_assets_by_os_site", err)
}

// ChangeRateRows reads every row of inventory.change_rate_1m ordered by
// window_start, entity_type.
func (d *Doris) ChangeRateRows(ctx context.Context) ([]ChangeRateRow, error) {
	return queryRows(ctx, d, "read inventory.change_rate_1m",
		"SELECT window_start, window_end, entity_type, changes FROM inventory.change_rate_1m ORDER BY window_start, entity_type",
		func(s interface{ Scan(...any) error }) (ChangeRateRow, error) {
			var r ChangeRateRow
			err := s.Scan(&r.WindowStart, &r.WindowEnd, &r.EntityType, &r.Changes)
			return r, err
		})
}

// LakeVersions returns every lake row of an asset, ordered by version.
func (d *Doris) LakeVersions(ctx context.Context, id string) ([]LakeVersion, error) {
	return queryRows(ctx, d, "read lake.inventory.asset_changes",
		"SELECT version, site FROM lake.inventory.asset_changes WHERE id = ? ORDER BY version",
		func(s interface{ Scan(...any) error }) (LakeVersion, error) {
			var r LakeVersion
			err := s.Scan(&r.Version, &r.Site)
			return r, err
		}, id)
}

// LakeMaxVersions returns MAX(version) per id in the lake.
func (d *Doris) LakeMaxVersions(ctx context.Context) (map[string]int64, error) {
	type kv struct {
		id string
		v  int64
	}
	rs, err := queryRows(ctx, d, "read lake.inventory.asset_changes",
		"SELECT id, MAX(version) FROM lake.inventory.asset_changes GROUP BY id",
		func(s interface{ Scan(...any) error }) (kv, error) {
			var r kv
			err := s.Scan(&r.id, &r.v)
			return r, err
		})
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(rs))
	for _, r := range rs {
		out[r.id] = r.v
	}
	return out, nil
}

// LakeDuplicates runs the duplicate query of test C04.
func (d *Doris) LakeDuplicates(ctx context.Context) ([]IDVersionCount, error) {
	return queryRows(ctx, d, "read lake.inventory.asset_changes",
		"SELECT id, version, COUNT(*) c FROM lake.inventory.asset_changes GROUP BY id, version HAVING c > 1",
		func(s interface{ Scan(...any) error }) (IDVersionCount, error) {
			var r IDVersionCount
			err := s.Scan(&r.ID, &r.Version, &r.Count)
			return r, err
		})
}

// Backends runs SHOW BACKENDS and returns each BE's host and liveness.
func (d *Doris) Backends(ctx context.Context) ([]Backend, error) {
	rows, err := d.db.QueryContext(ctx, "SHOW BACKENDS")
	if err != nil {
		return nil, d.wrap("SHOW BACKENDS", err)
	}
	defer func() { _ = rows.Close() }()
	cols, err := rows.Columns()
	if err != nil {
		return nil, d.wrap("SHOW BACKENDS", err)
	}
	var table [][]string
	for rows.Next() {
		raw := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, d.wrap("SHOW BACKENDS", err)
		}
		vals := make([]string, len(cols))
		for i, v := range raw {
			vals[i] = v.String
		}
		table = append(table, vals)
	}
	if err := rows.Err(); err != nil {
		return nil, d.wrap("SHOW BACKENDS", err)
	}
	return ParseBackends(cols, table)
}

// ParseBackends extracts Host and Alive from SHOW BACKENDS output.
func ParseBackends(cols []string, rows [][]string) ([]Backend, error) {
	hostIdx, aliveIdx := -1, -1
	for i, c := range cols {
		switch strings.ToLower(c) {
		case "host", "ip":
			if hostIdx < 0 {
				hostIdx = i
			}
		case "alive":
			aliveIdx = i
		}
	}
	if hostIdx < 0 || aliveIdx < 0 {
		return nil, fmt.Errorf("SHOW BACKENDS: columns Host and Alive not found in %v", cols)
	}
	out := make([]Backend, 0, len(rows))
	for _, r := range rows {
		out = append(out, Backend{Host: r[hostIdx], Alive: strings.EqualFold(r[aliveIdx], "true")})
	}
	return out, nil
}

// WindowKey identifies a change-rate window row.
type WindowKey struct {
	WindowStart time.Time
	EntityType  string
}

// DuplicateWindows returns every (window_start, entity_type) pair that appears
// more than once.
func DuplicateWindows(rows []ChangeRateRow) []WindowKey {
	seen := map[WindowKey]int{}
	var dups []WindowKey
	for _, r := range rows {
		k := WindowKey{r.WindowStart.UTC(), r.EntityType}
		seen[k]++
		if seen[k] == 2 {
			dups = append(dups, k)
		}
	}
	return dups
}

// Count returns SELECT COUNT(*) of a table name written by the caller.
func (d *Doris) Count(ctx context.Context, table string) (int64, error) {
	var n int64
	err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n)
	return n, d.wrap("count "+table, err)
}
