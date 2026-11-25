package labkit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

// HTTPResponse is a fully read HTTP response.
type HTTPResponse struct {
	Status int
	Header http.Header
	Body   []byte
}

// JSON decodes the body into v.
func (r *HTTPResponse) JSON(v any) error {
	if err := json.Unmarshal(r.Body, v); err != nil {
		return fmt.Errorf("decode JSON body %q: %w", truncate(string(r.Body), 200), err)
	}
	return nil
}

// HTTPGet performs a GET and reads the whole body. The URL is used as given,
// so paths with unusual characters reach the server unchanged.
func HTTPGet(ctx context.Context, url string) (*HTTPResponse, error) {
	return httpDo(ctx, http.MethodGet, url, nil)
}

// HTTPPostJSON posts body as JSON and reads the whole response.
func HTTPPostJSON(ctx context.Context, url string, body any) (*HTTPResponse, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return httpDo(ctx, http.MethodPost, url, b)
}

func httpDo(ctx context.Context, method, url string, body []byte) (*HTTPResponse, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%s %s: read body: %w", method, url, err)
	}
	return &HTTPResponse{Status: resp.StatusCode, Header: resp.Header, Body: b}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
