package labkit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
)

var t0 = time.Date(2026, 11, 25, 10, 14, 3, 120_456_789, time.UTC)

func dcAsset(id string, v int64, owner string) *adlv1.Asset {
	return &adlv1.Asset{Id: id, Hostname: strings.ToLower(id) + ".acme.local", Kind: AssetKindForID(id),
		Os: "Windows 11", Site: "Madrid", OwnerUserId: owner, Version: v, ChangedAt: timestamppb.New(t0)}
}

func dorisAsset(id string, v int64, owner string) AssetRow {
	return AssetRow{ID: id, Hostname: strings.ToLower(id) + ".acme.local", Kind: KindName(AssetKindForID(id)),
		OS: "Windows 11", Site: "Madrid", OwnerUserID: sql.NullString{String: owner, Valid: owner != ""},
		Version: v, ChangedAt: t0.Truncate(time.Millisecond).In(time.FixedZone("x", 3600))}
}

func TestCompareDorisEqualAfterNormalisation(t *testing.T) {
	dc := DatacoreState{
		Assets: []*adlv1.Asset{dcAsset("LT-0001", 3, ""), dcAsset("LT-0002", 1, "U-0001")},
		Users:  []*adlv1.User{{Id: "U-0001", DisplayName: "Ana", Email: "a@x", Version: 2, ChangedAt: timestamppb.New(t0)}},
		Groups: []*adlv1.Group{{Id: "G-01", Name: "Dirección", Version: 1, ChangedAt: timestamppb.New(t0)}},
	}
	d := DorisState{
		Assets: []AssetRow{dorisAsset("LT-0002", 1, "U-0001"), dorisAsset("LT-0001", 3, "")},
		Users:  []UserRow{{ID: "U-0001", DisplayName: "Ana", Email: "a@x", Version: 2, ChangedAt: t0.Truncate(time.Millisecond)}},
		Groups: []GroupRow{{ID: "G-01", Name: "Dirección", Version: 1, ChangedAt: t0.Truncate(time.Millisecond)}},
	}
	rep := CompareDoris(dc, d)
	if !rep.OK() {
		t.Fatalf("expected equal, got\n%s", rep)
	}
	if rep.Checked["assets"] != 2 || rep.Checked["users"] != 1 || rep.Checked["groups"] != 1 {
		t.Errorf("checked counts: %v", rep.Checked)
	}
}

func TestCompareDorisCategories(t *testing.T) {
	dc := DatacoreState{Assets: []*adlv1.Asset{
		dcAsset("LT-0001", 3, ""),       // missing
		dcAsset("LT-0002", 5, ""),       // behind
		dcAsset("LT-0003", 2, ""),       // ahead
		dcAsset("LT-0004", 4, "U-0001"), // field mismatch
	}}
	diffRow := dorisAsset("LT-0004", 4, "")
	diffRow.Site = "Bilbao"
	d := DorisState{Assets: []AssetRow{
		dorisAsset("LT-0002", 4, ""), dorisAsset("LT-0003", 3, ""), diffRow, dorisAsset("LT-0099", 1, ""),
	}}
	rep := CompareDoris(dc, d)
	want := map[Category]int{MissingInDoris: 1, DorisBehind: 1, DorisAhead: 1, FieldMismatch: 1, ExtraInDoris: 1}
	for c, n := range want {
		if rep.Counts[c] != n {
			t.Errorf("%s = %d, want %d\n%s", c, rep.Counts[c], n, rep)
		}
	}
	if rep.OK() || rep.Total() != 5 {
		t.Errorf("total %d", rep.Total())
	}
	s := rep.String()
	for _, frag := range []string{"FAIL", "site", "owner_user_id", "LT-0099"} {
		if !strings.Contains(s, frag) {
			t.Errorf("report misses %q:\n%s", frag, s)
		}
	}
}

func TestCompareDorisTimestampMillis(t *testing.T) {
	a := dcAsset("LT-0001", 1, "")
	r := dorisAsset("LT-0001", 1, "")
	r.ChangedAt = r.ChangedAt.Add(time.Millisecond)
	if d := AssetFieldDiff(a, r); !strings.Contains(d, "changed_at") {
		t.Errorf("a 1 ms difference must be reported, got %q", d)
	}
}

func TestReportCapsDiffs(t *testing.T) {
	var dc DatacoreState
	for i := 0; i < 50; i++ {
		dc.Assets = append(dc.Assets, dcAsset(fmt.Sprintf("LT-%04d", i+1), 1, ""))
	}
	rep := CompareDoris(dc, DorisState{})
	if rep.Counts[MissingInDoris] != 50 || len(rep.Diffs) != MaxReportedDiff {
		t.Fatalf("counts %v, diffs %d", rep.Counts, len(rep.Diffs))
	}
}

func TestCompareLake(t *testing.T) {
	assets := []*adlv1.Asset{dcAsset("LT-0001", 3, ""), dcAsset("LT-0002", 3, ""), dcAsset("LT-0003", 3, ""), dcAsset("LT-0004", 3, "")}
	rep := CompareLake(assets, LakeState{
		MaxVersions: map[string]int64{"LT-0001": 3, "LT-0002": 2, "LT-0003": 4},
		Duplicates:  []IDVersionCount{{ID: "LT-0001", Version: 2, Count: 2}},
	})
	want := map[Category]int{LakeMissing: 1, LakeBehind: 1, LakeAhead: 1, LakeDuplicate: 1}
	for c, n := range want {
		if rep.Counts[c] != n {
			t.Errorf("%s = %d, want %d", c, rep.Counts[c], n)
		}
	}
	ok := CompareLake(assets[:1], LakeState{MaxVersions: map[string]int64{"LT-0001": 3}})
	if !ok.OK() {
		t.Errorf("expected pass: %s", ok)
	}
}

type fakeSources struct {
	dc        DatacoreState
	dorisSeq  []DorisState
	calls     int
	lake      LakeState
	paused    bool
	restored  bool
	dorisErr  error
	pauseErr  error
	lakeCalls int
}

func (f *fakeSources) DatacoreState(context.Context) (DatacoreState, error) { return f.dc, nil }
func (f *fakeSources) DorisState(context.Context) (DorisState, error) {
	if f.dorisErr != nil {
		return DorisState{}, f.dorisErr
	}
	i := f.calls
	if i >= len(f.dorisSeq) {
		i = len(f.dorisSeq) - 1
	}
	f.calls++
	return f.dorisSeq[i], nil
}
func (f *fakeSources) LakeState(context.Context) (LakeState, error) {
	f.lakeCalls++
	return f.lake, nil
}
func (f *fakeSources) PauseGenerator(context.Context) (func(context.Context) error, error) {
	if f.pauseErr != nil {
		return nil, f.pauseErr
	}
	f.paused = true
	return func(context.Context) error { f.restored = true; return nil }, nil
}

func TestReconcileConverges(t *testing.T) {
	f := &fakeSources{
		dc:       DatacoreState{Assets: []*adlv1.Asset{dcAsset("LT-0001", 2, "")}},
		dorisSeq: []DorisState{{Assets: []AssetRow{dorisAsset("LT-0001", 1, "")}}, {Assets: []AssetRow{dorisAsset("LT-0001", 2, "")}}},
	}
	rep, err := Reconcile(context.Background(), f, ReconcileOptions{Timeout: time.Second, Interval: time.Millisecond, NoLake: true})
	if err != nil || !rep.OK() {
		t.Fatalf("err=%v\n%s", err, rep)
	}
	if !f.paused || !f.restored || f.calls != 2 || f.lakeCalls != 0 {
		t.Errorf("paused=%v restored=%v calls=%d lake=%d", f.paused, f.restored, f.calls, f.lakeCalls)
	}
}

func TestReconcileTimesOutWithReport(t *testing.T) {
	f := &fakeSources{
		dc:       DatacoreState{Assets: []*adlv1.Asset{dcAsset("LT-0001", 2, "")}},
		dorisSeq: []DorisState{{}},
		lake:     LakeState{MaxVersions: map[string]int64{"LT-0001": 2}},
	}
	rep, err := Reconcile(context.Background(), f, ReconcileOptions{Timeout: 30 * time.Millisecond, Interval: 5 * time.Millisecond, NoPause: true})
	if !errors.Is(err, ErrNotReconciled) || rep.Counts[MissingInDoris] != 1 || !rep.Lake {
		t.Fatalf("err=%v\n%s", err, rep)
	}
	if f.paused {
		t.Error("NoPause must not pause the generator")
	}
}

func TestReconcileLakeOnly(t *testing.T) {
	f := &fakeSources{
		dc:       DatacoreState{Assets: []*adlv1.Asset{dcAsset("LT-0001", 2, "")}},
		dorisErr: errors.New("doris must not be read"),
		lake:     LakeState{MaxVersions: map[string]int64{"LT-0001": 2}},
	}
	rep, err := Reconcile(context.Background(), f, ReconcileOptions{Timeout: time.Second, Interval: time.Millisecond, NoDoris: true})
	if err != nil || !rep.OK() || rep.Doris {
		t.Fatalf("err=%v\n%s", err, rep)
	}
}

func TestReconcileErrors(t *testing.T) {
	f := &fakeSources{pauseErr: errors.New("datacore down")}
	if _, err := Reconcile(context.Background(), f, ReconcileOptions{}); err == nil || !strings.Contains(err.Error(), "datacore down") {
		t.Errorf("pause failure should surface, got %v", err)
	}
	if _, err := Reconcile(context.Background(), &fakeSources{}, ReconcileOptions{NoDoris: true, NoLake: true}); err == nil {
		t.Error("nothing to compare should fail")
	}
	f2 := &fakeSources{dorisErr: errors.New("connection refused"), dc: DatacoreState{}}
	_, err := Reconcile(context.Background(), f2, ReconcileOptions{Timeout: 20 * time.Millisecond, Interval: 5 * time.Millisecond, NoLake: true})
	if !errors.Is(err, ErrNotReconciled) || !strings.Contains(err.Error(), "connection refused") || !f2.restored {
		t.Errorf("expected the last source error and a restored generator, got %v (restored=%v)", err, f2.restored)
	}
}
