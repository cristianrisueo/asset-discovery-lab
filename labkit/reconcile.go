package labkit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
)

// Category is a kind of difference found by reconciliation (spec §16.5).
type Category string

// Reconciliation categories. Doris ahead and field mismatches are serious bugs.
const (
	MissingInDoris  Category = "missing_in_doris"
	ExtraInDoris    Category = "extra_in_doris"
	DorisBehind     Category = "doris_behind"
	DorisAhead      Category = "doris_ahead"
	FieldMismatch   Category = "same_version_different_fields"
	LakeMissing     Category = "missing_in_lake"
	LakeBehind      Category = "lake_behind"
	LakeAhead       Category = "lake_ahead"
	LakeDuplicate   Category = "lake_duplicate_id_version"
	MaxReportedDiff          = 20
)

// DorisCategories and LakeCategories list the categories in report order.
var (
	DorisCategories = []Category{MissingInDoris, ExtraInDoris, DorisBehind, DorisAhead, FieldMismatch}
	LakeCategories  = []Category{LakeMissing, LakeBehind, LakeAhead, LakeDuplicate}
)

// Diff is one difference.
type Diff struct {
	Category Category
	Entity   string // asset, user or group
	ID       string
	Detail   string
}

func (d Diff) String() string {
	return fmt.Sprintf("%s %s %s: %s", d.Category, d.Entity, d.ID, d.Detail)
}

// Report is the outcome of one comparison.
type Report struct {
	Counts  map[Category]int
	Diffs   []Diff // the first MaxReportedDiff differences
	Checked map[string]int
	Doris   bool // the Doris part was compared
	Lake    bool // the lake part was compared
}

func newReport() Report {
	return Report{Counts: map[Category]int{}, Checked: map[string]int{}}
}

func (r *Report) add(d Diff) {
	r.Counts[d.Category]++
	if len(r.Diffs) < MaxReportedDiff {
		r.Diffs = append(r.Diffs, d)
	}
}

// Total is the number of differences.
func (r Report) Total() int {
	n := 0
	for _, c := range r.Counts {
		n += c
	}
	return n
}

// OK reports whether every count is zero.
func (r Report) OK() bool { return r.Total() == 0 }

// Merge adds o into r.
func (r *Report) Merge(o Report) {
	if r.Counts == nil {
		*r = newReport()
	}
	for c, n := range o.Counts {
		r.Counts[c] += n
	}
	for k, n := range o.Checked {
		r.Checked[k] += n
	}
	for _, d := range o.Diffs {
		if len(r.Diffs) < MaxReportedDiff {
			r.Diffs = append(r.Diffs, d)
		}
	}
	r.Doris = r.Doris || o.Doris
	r.Lake = r.Lake || o.Lake
}

// String renders the counts per category and the first differences.
func (r Report) String() string {
	var b strings.Builder
	status := "PASS"
	if !r.OK() {
		status = "FAIL"
	}
	keys := make([]string, 0, len(r.Checked))
	for k := range r.Checked {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var checked []string
	for _, k := range keys {
		checked = append(checked, fmt.Sprintf("%s=%d", k, r.Checked[k]))
	}
	fmt.Fprintf(&b, "reconcile: %s (checked %s)\n", status, strings.Join(checked, " "))
	var cats []Category
	if r.Doris {
		cats = append(cats, DorisCategories...)
	}
	if r.Lake {
		cats = append(cats, LakeCategories...)
	}
	for _, c := range cats {
		fmt.Fprintf(&b, "  %-32s %d\n", c, r.Counts[c])
	}
	if len(r.Diffs) > 0 {
		fmt.Fprintf(&b, "first %d differences:\n", len(r.Diffs))
		for _, d := range r.Diffs {
			fmt.Fprintf(&b, "  %s\n", d)
		}
	}
	return b.String()
}

// DatacoreState is every entity listed from datacore.
type DatacoreState struct {
	Assets []*adlv1.Asset
	Users  []*adlv1.User
	Groups []*adlv1.Group
}

// DorisState is every row of the three current-state tables.
type DorisState struct {
	Assets []AssetRow
	Users  []UserRow
	Groups []GroupRow
}

// LakeState is MAX(version) per id and the duplicated (id, version) pairs.
type LakeState struct {
	MaxVersions map[string]int64
	Duplicates  []IDVersionCount
}

// Normalisation (spec §16.5 step 2): empty string equals NULL, timestamps are
// compared in UTC at millisecond precision, enums by name.

func nullRef(n sql.NullString) string {
	if !n.Valid {
		return ""
	}
	return n.String
}

// MillisUTC truncates t to milliseconds in UTC.
func MillisUTC(t time.Time) time.Time { return t.UTC().Truncate(time.Millisecond) }

// TimestampMillis converts a protobuf timestamp to UTC milliseconds.
func TimestampMillis(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return MillisUTC(ts.AsTime())
}

type fieldCheck struct {
	name      string
	want, got string
}

func mismatches(checks []fieldCheck) string {
	var out []string
	for _, c := range checks {
		if c.want != c.got {
			out = append(out, fmt.Sprintf("%s: datacore %q, doris %q", c.name, c.want, c.got))
		}
	}
	return strings.Join(out, "; ")
}

func fmtTime(t time.Time) string { return t.Format("2006-01-02T15:04:05.000Z") }

// AssetFieldDiff compares a datacore asset with a Doris row after
// normalisation, ignoring the version. It returns "" when they are equal.
func AssetFieldDiff(a *adlv1.Asset, r AssetRow) string {
	return mismatches([]fieldCheck{
		{"hostname", a.GetHostname(), r.Hostname},
		{"kind", KindName(a.GetKind()), r.Kind},
		{"os", a.GetOs(), r.OS},
		{"site", a.GetSite(), r.Site},
		{"owner_user_id", a.GetOwnerUserId(), nullRef(r.OwnerUserID)},
		{"changed_at", fmtTime(TimestampMillis(a.GetChangedAt())), fmtTime(MillisUTC(r.ChangedAt))},
	})
}

// UserFieldDiff compares a datacore user with a Doris row, ignoring the version.
func UserFieldDiff(u *adlv1.User, r UserRow) string {
	return mismatches([]fieldCheck{
		{"display_name", u.GetDisplayName(), r.DisplayName},
		{"email", u.GetEmail(), r.Email},
		{"group_id", u.GetGroupId(), nullRef(r.GroupID)},
		{"changed_at", fmtTime(TimestampMillis(u.GetChangedAt())), fmtTime(MillisUTC(r.ChangedAt))},
	})
}

// GroupFieldDiff compares a datacore group with a Doris row, ignoring the version.
func GroupFieldDiff(g *adlv1.Group, r GroupRow) string {
	return mismatches([]fieldCheck{
		{"name", g.GetName(), r.Name},
		{"changed_at", fmtTime(TimestampMillis(g.GetChangedAt())), fmtTime(MillisUTC(r.ChangedAt))},
	})
}

func compareVersions(rep *Report, entity, id string, dc, doris int64, fieldDiff func() string) {
	switch {
	case doris < dc:
		rep.add(Diff{DorisBehind, entity, id, fmt.Sprintf("datacore v%d, doris v%d", dc, doris)})
	case doris > dc:
		rep.add(Diff{DorisAhead, entity, id, fmt.Sprintf("datacore v%d, doris v%d", dc, doris)})
	default:
		if d := fieldDiff(); d != "" {
			rep.add(Diff{FieldMismatch, entity, id, fmt.Sprintf("v%d: %s", dc, d)})
		}
	}
}

// CompareDoris compares datacore with Doris by id (spec §16.5 steps 2-3).
func CompareDoris(dc DatacoreState, d DorisState) Report {
	rep := newReport()
	rep.Doris = true
	rep.Checked["assets"], rep.Checked["users"], rep.Checked["groups"] = len(dc.Assets), len(dc.Users), len(dc.Groups)

	assets := map[string]AssetRow{}
	for _, r := range d.Assets {
		assets[r.ID] = r
	}
	for _, a := range sortedBy(dc.Assets, (*adlv1.Asset).GetId) {
		r, ok := assets[a.GetId()]
		if !ok {
			rep.add(Diff{MissingInDoris, EntityTypeNameAsset, a.GetId(), fmt.Sprintf("datacore v%d", a.GetVersion())})
			continue
		}
		delete(assets, a.GetId())
		compareVersions(&rep, EntityTypeNameAsset, a.GetId(), a.GetVersion(), r.Version, func() string { return AssetFieldDiff(a, r) })
	}
	for _, id := range sortedKeys(assets) {
		rep.add(Diff{ExtraInDoris, EntityTypeNameAsset, id, fmt.Sprintf("doris v%d", assets[id].Version)})
	}

	users := map[string]UserRow{}
	for _, r := range d.Users {
		users[r.ID] = r
	}
	for _, u := range sortedBy(dc.Users, (*adlv1.User).GetId) {
		r, ok := users[u.GetId()]
		if !ok {
			rep.add(Diff{MissingInDoris, EntityTypeNameUser, u.GetId(), fmt.Sprintf("datacore v%d", u.GetVersion())})
			continue
		}
		delete(users, u.GetId())
		compareVersions(&rep, EntityTypeNameUser, u.GetId(), u.GetVersion(), r.Version, func() string { return UserFieldDiff(u, r) })
	}
	for _, id := range sortedKeys(users) {
		rep.add(Diff{ExtraInDoris, EntityTypeNameUser, id, fmt.Sprintf("doris v%d", users[id].Version)})
	}

	groups := map[string]GroupRow{}
	for _, r := range d.Groups {
		groups[r.ID] = r
	}
	for _, g := range sortedBy(dc.Groups, (*adlv1.Group).GetId) {
		r, ok := groups[g.GetId()]
		if !ok {
			rep.add(Diff{MissingInDoris, EntityTypeNameGroup, g.GetId(), fmt.Sprintf("datacore v%d", g.GetVersion())})
			continue
		}
		delete(groups, g.GetId())
		compareVersions(&rep, EntityTypeNameGroup, g.GetId(), g.GetVersion(), r.Version, func() string { return GroupFieldDiff(g, r) })
	}
	for _, id := range sortedKeys(groups) {
		rep.add(Diff{ExtraInDoris, EntityTypeNameGroup, id, fmt.Sprintf("doris v%d", groups[id].Version)})
	}
	return rep
}

// CompareLake checks that MAX(version) in the lake equals datacore's version
// for every asset, and that no (id, version) pair is duplicated (spec §16.5 step 4).
func CompareLake(assets []*adlv1.Asset, lake LakeState) Report {
	rep := newReport()
	rep.Lake = true
	rep.Checked["lake_assets"] = len(assets)
	for _, a := range sortedBy(assets, (*adlv1.Asset).GetId) {
		v, ok := lake.MaxVersions[a.GetId()]
		switch {
		case !ok:
			rep.add(Diff{LakeMissing, EntityTypeNameAsset, a.GetId(), fmt.Sprintf("datacore v%d", a.GetVersion())})
		case v < a.GetVersion():
			rep.add(Diff{LakeBehind, EntityTypeNameAsset, a.GetId(), fmt.Sprintf("datacore v%d, lake max v%d", a.GetVersion(), v)})
		case v > a.GetVersion():
			rep.add(Diff{LakeAhead, EntityTypeNameAsset, a.GetId(), fmt.Sprintf("datacore v%d, lake max v%d", a.GetVersion(), v)})
		}
	}
	dups := append([]IDVersionCount(nil), lake.Duplicates...)
	sort.Slice(dups, func(i, j int) bool {
		if dups[i].ID != dups[j].ID {
			return dups[i].ID < dups[j].ID
		}
		return dups[i].Version < dups[j].Version
	})
	for _, d := range dups {
		rep.add(Diff{LakeDuplicate, EntityTypeNameAsset, d.ID, fmt.Sprintf("v%d appears %d times", d.Version, d.Count)})
	}
	return rep
}

func sortedBy[T any](xs []T, id func(T) string) []T {
	out := append([]T(nil), xs...)
	sort.SliceStable(out, func(i, j int) bool { return id(out[i]) < id(out[j]) })
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ReconcileSources is what Reconcile reads from.
type ReconcileSources interface {
	DatacoreState(ctx context.Context) (DatacoreState, error)
	DorisState(ctx context.Context) (DorisState, error)
	LakeState(ctx context.Context) (LakeState, error)
	PauseGenerator(ctx context.Context) (restore func(context.Context) error, err error)
}

// ReconcileOptions controls Reconcile.
type ReconcileOptions struct {
	Timeout  time.Duration // default 3 minutes
	Interval time.Duration // default 2 seconds (spec §16.5)
	NoPause  bool          // do not pause the generator
	NoLake   bool          // skip the lake comparison
	NoDoris  bool          // skip the Doris comparison (lake-only reconcile, test C05)
	Logf     func(format string, args ...any)
}

// ErrNotReconciled is returned when the comparison does not converge.
var ErrNotReconciled = errors.New("not reconciled")

// Reconcile implements spec §16.5: unless NoPause, pause the generator and
// restore it at the end; then compare every Interval until there is no
// difference or Timeout expires. It returns the last report; the error is nil
// only when that report passed.
func Reconcile(ctx context.Context, src ReconcileSources, opts ReconcileOptions) (rep Report, err error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 3 * time.Minute
	}
	if opts.Interval <= 0 {
		opts.Interval = 2 * time.Second
	}
	if opts.NoDoris && opts.NoLake {
		return Report{}, errors.New("reconcile: nothing to compare (both Doris and the lake are disabled)")
	}
	if !opts.NoPause {
		restore, perr := src.PauseGenerator(ctx)
		if perr != nil {
			return Report{}, fmt.Errorf("reconcile: pause the generator: %w", perr)
		}
		defer func() {
			rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			if rerr := restore(rctx); rerr != nil {
				err = errors.Join(err, fmt.Errorf("reconcile: restore the generator: %w", rerr))
			}
		}()
	}
	deadline := time.Now().Add(opts.Timeout)
	var lastErr error
	attempt := 0
	for {
		attempt++
		cur, cerr := compareOnce(ctx, src, opts)
		if cerr == nil {
			rep, lastErr = cur, nil
			if rep.OK() {
				return rep, nil
			}
			if opts.Logf != nil {
				opts.Logf("reconcile attempt %d: %d differences", attempt, rep.Total())
			}
		} else {
			lastErr = cerr
			if opts.Logf != nil {
				opts.Logf("reconcile attempt %d: %v", attempt, cerr)
			}
		}
		if time.Now().Add(opts.Interval).After(deadline) {
			if lastErr != nil {
				return rep, fmt.Errorf("%w within %s: last error: %w", ErrNotReconciled, opts.Timeout, lastErr)
			}
			return rep, fmt.Errorf("%w within %s: %d differences", ErrNotReconciled, opts.Timeout, rep.Total())
		}
		select {
		case <-ctx.Done():
			return rep, fmt.Errorf("%w: %w", ErrNotReconciled, ctx.Err())
		case <-time.After(opts.Interval):
		}
	}
}

func compareOnce(ctx context.Context, src ReconcileSources, opts ReconcileOptions) (Report, error) {
	dc, err := src.DatacoreState(ctx)
	if err != nil {
		return Report{}, err
	}
	rep := newReport()
	if !opts.NoDoris {
		d, err := src.DorisState(ctx)
		if err != nil {
			return Report{}, err
		}
		rep.Merge(CompareDoris(dc, d))
	}
	if !opts.NoLake {
		l, err := src.LakeState(ctx)
		if err != nil {
			return Report{}, err
		}
		rep.Merge(CompareLake(dc.Assets, l))
	}
	return rep, nil
}

// LabSources reads from the real datacore and Doris.
type LabSources struct {
	Datacore *Datacore
	Doris    *Doris
}

// DatacoreState lists every entity from datacore.
func (s LabSources) DatacoreState(ctx context.Context) (DatacoreState, error) {
	var st DatacoreState
	var err error
	if st.Assets, err = s.Datacore.ListAssets(ctx); err != nil {
		return st, err
	}
	if st.Users, err = s.Datacore.ListUsers(ctx); err != nil {
		return st, err
	}
	st.Groups, err = s.Datacore.ListGroups(ctx)
	return st, err
}

// DorisState reads the three current-state tables.
func (s LabSources) DorisState(ctx context.Context) (DorisState, error) {
	var st DorisState
	var err error
	if st.Assets, err = s.Doris.Assets(ctx); err != nil {
		return st, err
	}
	if st.Users, err = s.Doris.Users(ctx); err != nil {
		return st, err
	}
	st.Groups, err = s.Doris.Groups(ctx)
	return st, err
}

// LakeState reads MAX(version) per id and duplicates from the lake.
func (s LabSources) LakeState(ctx context.Context) (LakeState, error) {
	var st LakeState
	var err error
	if st.MaxVersions, err = s.Doris.LakeMaxVersions(ctx); err != nil {
		return st, err
	}
	st.Duplicates, err = s.Doris.LakeDuplicates(ctx)
	return st, err
}

// PauseGenerator pauses the datacore generator.
func (s LabSources) PauseGenerator(ctx context.Context) (func(context.Context) error, error) {
	return s.Datacore.PauseGenerator(ctx)
}
