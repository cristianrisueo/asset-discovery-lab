//go:build acceptance

package acceptance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cristianrisueo/asset-discovery-lab/labkit"
)

// Gate D: serving (explorer, web). Spec §16.4.

var gateDServices = append(append([]string(nil), gateCServices...), labkit.CompExplorer)

// apiOwner and apiAsset are the explorer's Asset JSON (spec §13.3).
type apiOwner struct {
	UserID  string  `json:"user_id"`
	Name    *string `json:"name"`
	GroupID *string `json:"group_id"`
	Group   *string `json:"group"`
}

type apiAsset struct {
	ID        string    `json:"id"`
	Hostname  string    `json:"hostname"`
	Kind      string    `json:"kind"`
	OS        string    `json:"os"`
	Site      string    `json:"site"`
	Owner     *apiOwner `json:"owner"`
	Version   int64     `json:"version"`
	ChangedAt string    `json:"changed_at"`
}

type apiHistory struct {
	ID       string `json:"id"`
	Versions []struct {
		Version     int64   `json:"version"`
		Hostname    string  `json:"hostname"`
		Kind        string  `json:"kind"`
		OS          string  `json:"os"`
		Site        string  `json:"site"`
		OwnerUserID *string `json:"owner_user_id"`
		ChangedAt   string  `json:"changed_at"`
	} `json:"versions"`
}

type apiList struct {
	Items  []apiAsset `json:"items"`
	Limit  int        `json:"limit"`
	Offset int        `json:"offset"`
}

func strp(p *string) string {
	if p == nil {
		return "<null>"
	}
	return *p
}

func TestGateD_01_AssetEndpoint(t *testing.T) {
	h := newHarness(t, gateDServices...)
	g := h.upsertGroup(labkit.NewGroupRequest("G-93", "QA Explorer"))
	u := h.upsertUser(labkit.NewUserRequest("U-9301", "Explorer User", "G-93"))
	a := h.upsertAsset(labkit.NewAssetRequest("LT-9301", "Madrid", "U-9301"))
	url := h.cfg.Explorer + "/api/assets/LT-9301"

	want := apiAsset{
		ID: a.GetId(), Hostname: a.GetHostname(), Kind: labkit.KindName(a.GetKind()), OS: a.GetOs(), Site: a.GetSite(),
		Version: a.GetVersion(), ChangedAt: rfc3339Millis(a.GetChangedAt().AsTime()),
	}
	h.eventually(TPipe, "GET "+url+" equals datacore", func(ctx context.Context) (bool, string, error) {
		r, err := labkit.HTTPGet(ctx, url)
		if err != nil {
			return false, "", err
		}
		state := fmt.Sprintf("%d %s", r.Status, r.Body)
		if r.Status != http.StatusOK {
			return false, state, nil
		}
		var got apiAsset
		if err := r.JSON(&got); err != nil {
			return false, state, err
		}
		if got.Owner == nil {
			return false, state, nil
		}
		ownerOK := got.Owner.UserID == u.GetId() && strp(got.Owner.Name) == u.GetDisplayName() &&
			strp(got.Owner.GroupID) == g.GetId() && strp(got.Owner.Group) == g.GetName()
		gotFlat, wantFlat := got, want
		gotFlat.Owner, wantFlat.Owner = nil, nil
		return ownerOK && gotFlat == wantFlat, state, nil
	})
}

func TestGateD_02_ErrorsAndValidation(t *testing.T) {
	h := newHarness(t, labkit.CompExplorer)
	base := h.cfg.Explorer

	u := base + "/api/assets/LT-9999"
	h.requireAPIError(h.getStable(u, TShort), u, http.StatusNotFound, "not_found")

	u = base + "/api/assets/LT-0042'%20OR%20'1'='1"
	h.requireAPIError(h.getStable(u, TShort), u, http.StatusBadRequest, "invalid_argument")

	u = base + "/api/assets?limit=0"
	h.requireStatus(h.getStable(u, TShort), u, http.StatusBadRequest)

	u = base + "/api/stats/change-rate?minutes=0"
	h.requireStatus(h.getStable(u, TShort), u, http.StatusBadRequest)
}

func TestGateD_03_HistoryEndpoint(t *testing.T) {
	h := newHarness(t, gateDServices...)
	a1 := h.upsertAsset(labkit.NewAssetRequest("LT-9302", "Alicante", ""))
	h.waitDorisAsset("LT-9302", a1.GetVersion(), TPipe)
	a2 := h.changeSite(a1, "Madrid")
	url := h.cfg.Explorer + "/api/assets/LT-9302/history"
	h.eventually(TLake, fmt.Sprintf("history of LT-9302 includes v%d Alicante and v%d Madrid", a1.GetVersion(), a2.GetVersion()),
		func(ctx context.Context) (bool, string, error) {
			r, err := labkit.HTTPGet(ctx, url)
			if err != nil {
				return false, "", err
			}
			state := fmt.Sprintf("%d %s", r.Status, r.Body)
			if r.Status != http.StatusOK {
				return false, state, nil
			}
			var hist apiHistory
			if err := r.JSON(&hist); err != nil {
				return false, state, err
			}
			seen1, seen2 := false, false
			for i, v := range hist.Versions {
				if i > 0 && v.Version <= hist.Versions[i-1].Version {
					return false, state, labkit.Stop(fmt.Errorf("history versions are not in ascending order: %s", r.Body))
				}
				seen1 = seen1 || (v.Version == a1.GetVersion() && v.Site == "Alicante")
				seen2 = seen2 || (v.Version == a2.GetVersion() && v.Site == "Madrid")
			}
			return seen1 && seen2, state, nil
		})
}

func TestGateD_04_ListEndpoint(t *testing.T) {
	h := newHarness(t, gateDServices...)
	// A quiet system keeps the two pages consistent with each other (spec §16.3).
	h.pauseGenerator()
	page := func(offset int) apiList {
		u := fmt.Sprintf("%s/api/assets?site=Madrid&limit=10", h.cfg.Explorer)
		if offset > 0 {
			u += fmt.Sprintf("&offset=%d", offset)
		}
		r := h.getStable(u, TShort)
		h.requireStatus(r, u, http.StatusOK)
		var l apiList
		h.fatalIf(r.JSON(&l), "GET %s", u)
		if len(l.Items) > 10 {
			t.Fatalf("GET %s returned %d items, want at most 10", u, len(l.Items))
		}
		for i, it := range l.Items {
			if it.Site != "Madrid" {
				t.Fatalf("GET %s: item %s is in %q, want Madrid", u, it.ID, it.Site)
			}
			if i > 0 && it.ID <= l.Items[i-1].ID {
				t.Fatalf("GET %s: ids not ascending (%s after %s)", u, it.ID, l.Items[i-1].ID)
			}
		}
		return l
	}
	first, second := page(0), page(10)
	ids := map[string]bool{}
	for _, it := range first.Items {
		ids[it.ID] = true
	}
	for _, it := range second.Items {
		if ids[it.ID] {
			t.Fatalf("asset %s appears on both pages", it.ID)
		}
	}
}

func TestGateD_05_StatsEndpoints(t *testing.T) {
	h := newHarness(t, gateDServices...)
	h.pauseGenerator()
	h.reconcile("Doris reconcile", labkit.ReconcileOptions{Timeout: TReconcile, NoLake: true, NoPause: true})
	h.waitMVConsistent()

	mv, err := h.lab.Doris.MVRows(h.ctx)
	h.fatalIf(err, "read the MV")
	u := h.cfg.Explorer + "/api/stats/os-site"
	r := h.getStable(u, TShort)
	h.requireStatus(r, u, http.StatusOK)
	var osSite struct {
		Items []struct {
			OS     string `json:"os"`
			Site   string `json:"site"`
			Assets int64  `json:"assets"`
		} `json:"items"`
	}
	h.fatalIf(r.JSON(&osSite), "GET %s", u)
	got := make([]labkit.OSSiteRow, 0, len(osSite.Items))
	for _, it := range osSite.Items {
		got = append(got, labkit.OSSiteRow{OS: it.OS, Site: it.Site, Assets: it.Assets})
	}
	if fmt.Sprint(got) != fmt.Sprint(mv) {
		t.Fatalf("GET %s = %v, want the MV rows %v", u, got, mv)
	}

	u = h.cfg.Explorer + "/api/stats/change-rate?minutes=60"
	r = h.getStable(u, TShort)
	h.requireStatus(r, u, http.StatusOK)
	var cr struct {
		Items []struct {
			WindowStart string `json:"window_start"`
			WindowEnd   string `json:"window_end"`
			EntityType  string `json:"entity_type"`
			Changes     int64  `json:"changes"`
		} `json:"items"`
	}
	h.fatalIf(r.JSON(&cr), "GET %s", u)
	type key struct {
		start time.Time
		typ   string
	}
	seen := map[key]bool{}
	var prev *key
	for _, it := range cr.Items {
		ws, err := time.Parse(time.RFC3339Nano, it.WindowStart)
		h.fatalIf(err, "window_start %q", it.WindowStart)
		k := key{ws.UTC(), it.EntityType}
		if seen[k] {
			t.Fatalf("GET %s: duplicate (window_start, entity_type) pair %v", u, k)
		}
		seen[k] = true
		if prev != nil && (k.start.Before(prev.start) || (k.start.Equal(prev.start) && k.typ < prev.typ)) {
			t.Fatalf("GET %s: items not ordered by window_start, entity_type (%v after %v)", u, k, *prev)
		}
		kk := k
		prev = &kk
	}
}

func TestGateD_06_FlightSqlTransport(t *testing.T) {
	h := newHarness(t, labkit.CompExplorer)
	u := h.cfg.Explorer + "/readyz"
	r := h.getStable(u, TShort)
	h.requireStatus(r, u, http.StatusOK)
	var ready struct {
		Status         string `json:"status"`
		DorisTransport string `json:"doris_transport"`
	}
	h.fatalIf(r.JSON(&ready), "GET %s", u)
	switch ready.DorisTransport {
	case "flight-sql", "adbc-flight-sql":
	case "mysql":
		if !h.cfg.AllowMySQLFallback {
			t.Fatalf("doris_transport is mysql; allowed only with ADL_ALLOW_MYSQL_FALLBACK=1 (spec §3 R4)")
		}
	default:
		t.Fatalf("doris_transport %q, want flight-sql or adbc-flight-sql", ready.DorisTransport)
	}
}

func TestGateD_07_WebServes(t *testing.T) {
	h := newHarness(t, labkit.CompWeb, labkit.CompExplorer)
	u := h.cfg.Web + "/"
	r := h.getStable(u, TShort)
	h.requireStatus(r, u, http.StatusOK)
	if !strings.Contains(string(r.Body), `<div id="root">`) {
		t.Fatalf("GET %s: the HTML does not contain <div id=\"root\">: %s", u, r.Body)
	}
	u = h.cfg.Web + "/api/assets/LT-0042"
	r = h.getStable(u, TShort)
	h.requireStatus(r, u, http.StatusOK)
	var a apiAsset
	if !json.Valid(r.Body) {
		t.Fatalf("GET %s: body is not JSON: %s", u, r.Body)
	}
	h.fatalIf(r.JSON(&a), "GET %s", u)
	if a.ID != "LT-0042" {
		t.Fatalf("GET %s: id %q, want LT-0042", u, a.ID)
	}
}

func TestGateD_08_HealthEndpoints(t *testing.T) {
	h := newHarness(t, labkit.CompExplorer, labkit.CompAdapter, labkit.CompDatacore)
	for _, base := range []string{h.cfg.Explorer, h.cfg.AdapterHTTP, h.cfg.DatacoreHTTP} {
		u := base + "/healthz"
		r := h.getStable(u, TShort)
		h.requireStatus(r, u, http.StatusOK)
	}
}
