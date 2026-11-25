// Package store is datacore's repository: SQLite through modernc.org/sqlite
// (spec §7.2). It keeps the current and the previous state of every entity
// and the transactional outbox, and applies every change in one transaction
// (spec §7.3).
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	// Registers the pure-Go "sqlite" driver (no cgo, spec §2.5).
	_ "modernc.org/sqlite"

	"github.com/cristianrisueo/asset-discovery-lab/datacore/internal/model"
	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
)

const schema = `
CREATE TABLE IF NOT EXISTS entities (
  type          TEXT    NOT NULL,
  id            TEXT    NOT NULL,
  state         BLOB    NOT NULL,
  prev_state    BLOB,
  version       INTEGER NOT NULL,
  changed_at_ms INTEGER NOT NULL,
  PRIMARY KEY (type, id)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS outbox (
  seq           INTEGER PRIMARY KEY AUTOINCREMENT,
  topic         TEXT    NOT NULL,
  key           TEXT    NOT NULL,
  value         BLOB    NOT NULL,
  created_at_ms INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
) WITHOUT ROWID;
`

// ErrNotFound is returned when an entity does not exist.
var ErrNotFound = errors.New("not found")

var marshal = proto.MarshalOptions{Deterministic: true}

// Store is the datacore repository.
type Store struct {
	db    *sql.DB
	topic string
	now   func() time.Time

	changesApplied atomic.Int64

	// beforeCommit, when set, runs inside every change transaction just
	// before the commit. Tests use it to inject failures.
	beforeCommit func() error
}

// Open opens (or creates) the database at path with WAL journaling and
// synchronous=NORMAL (spec §7.2). Triggers are written for triggersTopic.
func Open(ctx context.Context, path, triggersTopic string) (*Store, error) {
	q := url.Values{}
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Add("_pragma", "busy_timeout(10000)")
	db, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	// One connection serialises every write, so outbox seq order equals
	// commit order, and SQLite never reports SQLITE_BUSY to ourselves.
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(0)
	if _, err := db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create schema in %s: %w", path, err)
	}
	return &Store{db: db, topic: triggersTopic, now: time.Now}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// ChangesApplied counts the changes applied since start (spec §6.5 GetStats);
// the seed is not counted.
func (s *Store) ChangesApplied() int64 { return s.changesApplied.Load() }

// Now returns the current time in UTC with millisecond precision, the
// precision of changed_at (spec §5.1).
func (s *Store) Now() time.Time { return s.now().UTC().Truncate(time.Millisecond) }

// newMessage returns an empty message of the type.
func newMessage(t model.Type) proto.Message {
	switch t {
	case model.TypeAsset:
		return &adlv1.Asset{}
	case model.TypeUser:
		return &adlv1.User{}
	default:
		return &adlv1.Group{}
	}
}

// stamp sets version and changed_at on an entity message.
func stamp(m proto.Message, version int64, ts *timestamppb.Timestamp) {
	switch e := m.(type) {
	case *adlv1.Asset:
		e.Version, e.ChangedAt = version, ts
	case *adlv1.User:
		e.Version, e.ChangedAt = version, ts
	case *adlv1.Group:
		e.Version, e.ChangedAt = version, ts
	}
}

// Mutator returns the new state of an entity from its current state. cur is
// nil when the entity does not exist yet. It must return a fresh message;
// version and changed_at are set by Apply.
type Mutator = func(cur proto.Message) (proto.Message, error)

// Apply runs one change as spec §7.3 describes, in one transaction: read the
// entity, keep its current state as the previous state, apply the change with
// version+1 (or 1 on creation) and changed_at = now, insert the outbox row
// with the trigger, and commit. The context is detached from cancellation so
// a change that has started always finishes (spec §7.11).
func (s *Store) Apply(ctx context.Context, t model.Type, id string, mutate Mutator) (proto.Message, error) {
	ctx = context.WithoutCancel(ctx)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var (
		curState []byte
		curVer   int64
		cur      proto.Message
	)
	err = tx.QueryRowContext(ctx, `SELECT state, version FROM entities WHERE type = ? AND id = ?`, string(t), id).Scan(&curState, &curVer)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return nil, fmt.Errorf("read %s %s: %w", t, id, err)
	default:
		cur = newMessage(t)
		if err := proto.Unmarshal(curState, cur); err != nil {
			return nil, fmt.Errorf("decode %s %s: %w", t, id, err)
		}
	}

	next, err := mutate(cur)
	if err != nil {
		return nil, err
	}
	now := s.Now()
	ts := timestamppb.New(now)
	version := curVer + 1
	stamp(next, version, ts)
	state, err := marshal.Marshal(next)
	if err != nil {
		return nil, fmt.Errorf("encode %s %s: %w", t, id, err)
	}
	if cur == nil {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO entities (type, id, state, prev_state, version, changed_at_ms) VALUES (?, ?, ?, NULL, ?, ?)`,
			string(t), id, state, version, now.UnixMilli())
	} else {
		_, err = tx.ExecContext(ctx,
			`UPDATE entities SET prev_state = state, state = ?, version = ?, changed_at_ms = ? WHERE type = ? AND id = ?`,
			state, version, now.UnixMilli(), string(t), id)
	}
	if err != nil {
		return nil, fmt.Errorf("write %s %s: %w", t, id, err)
	}
	if err := s.insertTrigger(ctx, tx, t, id, version, ts, now); err != nil {
		return nil, err
	}
	if s.beforeCommit != nil {
		if err := s.beforeCommit(); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit %s %s: %w", t, id, err)
	}
	s.changesApplied.Add(1)
	return next, nil
}

func (s *Store) insertTrigger(ctx context.Context, tx *sql.Tx, t model.Type, id string, version int64, ts *timestamppb.Timestamp, now time.Time) error {
	trig, err := marshal.Marshal(&adlv1.Trigger{EntityType: t.EntityType(), EntityId: id, Version: version, ChangedAt: ts})
	if err != nil {
		return fmt.Errorf("encode trigger: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO outbox (topic, key, value, created_at_ms) VALUES (?, ?, ?, ?)`,
		s.topic, model.TriggerKey(t, id), trig, now.UnixMilli()); err != nil {
		return fmt.Errorf("insert outbox row for %s %s: %w", t, id, err)
	}
	return nil
}

// Snapshot is the stored state of an entity.
type Snapshot struct {
	Current   proto.Message
	Previous  proto.Message // nil when the entity has no previous state
	ChangedAt time.Time
}

// Get reads an entity with its previous state. It returns ErrNotFound when
// the entity does not exist.
func (s *Store) Get(ctx context.Context, t model.Type, id string) (Snapshot, error) {
	var (
		state, prev []byte
		changedMs   int64
	)
	err := s.db.QueryRowContext(ctx, `SELECT state, prev_state, changed_at_ms FROM entities WHERE type = ? AND id = ?`,
		string(t), id).Scan(&state, &prev, &changedMs)
	if errors.Is(err, sql.ErrNoRows) {
		return Snapshot{}, ErrNotFound
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("read %s %s: %w", t, id, err)
	}
	snap := Snapshot{Current: newMessage(t), ChangedAt: time.UnixMilli(changedMs).UTC()}
	if err := proto.Unmarshal(state, snap.Current); err != nil {
		return Snapshot{}, fmt.Errorf("decode %s %s: %w", t, id, err)
	}
	if prev != nil {
		snap.Previous = newMessage(t)
		if err := proto.Unmarshal(prev, snap.Previous); err != nil {
			return Snapshot{}, fmt.Errorf("decode previous %s %s: %w", t, id, err)
		}
	}
	return snap, nil
}

// Exists reports whether an entity exists.
func (s *Store) Exists(ctx context.Context, t model.Type, id string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entities WHERE type = ? AND id = ?`, string(t), id).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("look up %s %s: %w", t, id, err)
	}
	return n > 0, nil
}

// List reads every entity of the type, ordered by id. It reads everything
// into memory, so the connection is free while the caller streams.
func (s *Store) List(ctx context.Context, t model.Type) ([]proto.Message, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT state FROM entities WHERE type = ? ORDER BY id`, string(t))
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", t, err)
	}
	defer func() { _ = rows.Close() }()
	var out []proto.Message
	for rows.Next() {
		var state []byte
		if err := rows.Scan(&state); err != nil {
			return nil, fmt.Errorf("list %s: %w", t, err)
		}
		m := newMessage(t)
		if err := proto.Unmarshal(state, m); err != nil {
			return nil, fmt.Errorf("decode %s: %w", t, err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Counts returns the number of assets, users and groups.
func (s *Store) Counts(ctx context.Context) (assets, users, groups int64, err error) {
	rows, err := s.db.QueryContext(ctx, `SELECT type, COUNT(*) FROM entities GROUP BY type`)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("count entities: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			t string
			n int64
		)
		if err := rows.Scan(&t, &n); err != nil {
			return 0, 0, 0, fmt.Errorf("count entities: %w", err)
		}
		switch model.Type(t) {
		case model.TypeAsset:
			assets = n
		case model.TypeUser:
			users = n
		case model.TypeGroup:
			groups = n
		}
	}
	return assets, users, groups, rows.Err()
}

// OutboxRow is one pending trigger (spec §7.2).
type OutboxRow struct {
	Seq       int64
	Topic     string
	Key       string
	Value     []byte
	CreatedAt time.Time
}

// OutboxBatch reads up to limit pending rows in seq order.
func (s *Store) OutboxBatch(ctx context.Context, limit int) ([]OutboxRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT seq, topic, key, value, created_at_ms FROM outbox ORDER BY seq LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("read outbox: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []OutboxRow
	for rows.Next() {
		var (
			r  OutboxRow
			ms int64
		)
		if err := rows.Scan(&r.Seq, &r.Topic, &r.Key, &r.Value, &ms); err != nil {
			return nil, fmt.Errorf("read outbox: %w", err)
		}
		r.CreatedAt = time.UnixMilli(ms).UTC()
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteOutboxThrough deletes every outbox row with seq <= seq. Writes are
// serialised, so rows with a lower seq can never commit after a higher one.
func (s *Store) DeleteOutboxThrough(ctx context.Context, seq int64) error {
	if _, err := s.db.ExecContext(context.WithoutCancel(ctx), `DELETE FROM outbox WHERE seq <= ?`, seq); err != nil {
		return fmt.Errorf("delete published outbox rows: %w", err)
	}
	return nil
}

// OutboxPending counts the rows not yet published.
func (s *Store) OutboxPending(ctx context.Context) (int64, error) {
	var n int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count outbox: %w", err)
	}
	return n, nil
}

const metaSeedComplete = "seed_complete"

// SeedComplete reports whether the seed finished.
func (s *Store) SeedComplete(ctx context.Context) (bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, metaSeedComplete).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read meta: %w", err)
	}
	return true, nil
}

func (s *Store) markSeedComplete(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO meta (key, value) VALUES (?, ?)`,
		metaSeedComplete, s.Now().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("mark seed complete: %w", err)
	}
	return nil
}

// insertSeedBatch creates the entities that do not exist yet, at version 1,
// with one outbox row each, in one transaction. Existing entities are left
// alone, so an interrupted seed resumes where it stopped.
func (s *Store) insertSeedBatch(ctx context.Context, t model.Type, msgs []proto.Message, ids []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin seed batch: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := s.Now()
	ts := timestamppb.New(now)
	for i, m := range msgs {
		stamp(m, 1, ts)
		state, err := marshal.Marshal(m)
		if err != nil {
			return fmt.Errorf("encode seed %s %s: %w", t, ids[i], err)
		}
		res, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO entities (type, id, state, prev_state, version, changed_at_ms) VALUES (?, ?, ?, NULL, 1, ?)`,
			string(t), ids[i], state, now.UnixMilli())
		if err != nil {
			return fmt.Errorf("insert seed %s %s: %w", t, ids[i], err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue
		}
		if err := s.insertTrigger(ctx, tx, t, ids[i], 1, ts, now); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit seed batch: %w", err)
	}
	return nil
}
