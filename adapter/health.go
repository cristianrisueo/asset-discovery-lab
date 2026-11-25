package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// TransientStallLimit is how long a partition may retry transient errors
// before /readyz reports 503 (spec §9.4, §9.8).
const TransientStallLimit = 30 * time.Second

// healthHandler serves /healthz (200 while the process runs) and /readyz
// (200 while ready() is true).
func healthHandler(ready func() (bool, string)) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if ok, why := ready(); !ok {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "reason": why})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// healthcheck GETs url and returns an error unless it answers 200. Compose
// runs it through `adapter healthcheck`, because distroless has no curl.
func healthcheck(url string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %d", url, resp.StatusCode)
	}
	return nil
}
