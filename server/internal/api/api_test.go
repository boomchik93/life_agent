package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lifeagent/internal/calendar"
	"lifeagent/internal/store"
)

const testToken = "test-token-test-token-test-token-test-token-1234"

func newServer(t *testing.T, burst int) http.Handler {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(Options{
		Store: st, Token: testToken, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		RatePerSec: 1, Burst: burst,
	})
}

func do(h http.Handler, method, path, auth, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHealthzNoToken(t *testing.T) {
	if rec := do(newServer(t, 10), "GET", "/healthz", "", ""); rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestAuth(t *testing.T) {
	h := newServer(t, 100)
	tests := []struct {
		name, auth string
		want       int
	}{
		{"no header", "", 401},
		{"wrong token", "Bearer nope", 401},
		{"wrong scheme", "Basic " + testToken, 401},
		{"bare token", testToken, 401},
		{"prefix of token", "Bearer " + testToken[:10], 401},
		{"ok", "Bearer " + testToken, 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rec := do(h, "GET", "/v1/settings", tt.auth, ""); rec.Code != tt.want {
				t.Fatalf("status %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

func TestUnknownV1RequiresTokenFirst(t *testing.T) {
	h := newServer(t, 100)
	if rec := do(h, "GET", "/v1/nothing", "", ""); rec.Code != 401 {
		t.Errorf("no token: %d", rec.Code)
	}
	if rec := do(h, "GET", "/v1/nothing", "Bearer "+testToken, ""); rec.Code != 404 {
		t.Errorf("with token: %d", rec.Code)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	h := newServer(t, 100)
	auth := "Bearer " + testToken
	rec := do(h, "PUT", "/v1/settings", auth, `{"sleep_hours":7,"tz":"Asia/Yekaterinburg"}`)
	if rec.Code != 200 {
		t.Fatalf("put %d %s", rec.Code, rec.Body)
	}
	rec = do(h, "GET", "/v1/settings", auth, "")
	body := rec.Body.String()
	if !strings.Contains(body, `"sleep_hours":7`) || !strings.Contains(body, `"tz":"Asia/Yekaterinburg"`) {
		t.Fatalf("get body %s", body)
	}
	for _, bad := range []string{`[]`, `null`, `not json`, `{"":1}`} {
		if rec := do(h, "PUT", "/v1/settings", auth, bad); rec.Code != 400 {
			t.Errorf("body %q: status %d", bad, rec.Code)
		}
	}
}

type fakeCollector struct {
	from, to time.Time
	events   []calendar.Event
	warnings []string
}

func (f *fakeCollector) Collect(_ context.Context, from, to time.Time) ([]calendar.Event, []string) {
	f.from, f.to = from, to
	return f.events, f.warnings
}

func TestWeek(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Yekaterinburg")
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	col := &fakeCollector{warnings: []string{"ics: timeout"}}
	h := New(Options{
		Store: st, Token: testToken, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		RatePerSec: 100, Burst: 100, Calendar: col, Location: loc,
		Now: func() time.Time { return time.Date(2026, 10, 27, 12, 0, 0, 0, loc) },
	})
	auth := "Bearer " + testToken

	rec := do(h, "GET", "/v1/week", auth, "")
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	if !col.from.Equal(time.Date(2026, 10, 26, 0, 0, 0, 0, loc)) || !col.to.Equal(time.Date(2026, 11, 2, 0, 0, 0, 0, loc)) {
		t.Errorf("window %v - %v", col.from, col.to)
	}
	var got struct {
		Events   []calendar.Event `json:"events"`
		Warnings []string         `json:"warnings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Events == nil || len(got.Events) != 0 || len(got.Warnings) != 1 {
		t.Errorf("body %s", rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"events":[]`) {
		t.Errorf("events must be [] not null: %s", rec.Body)
	}

	do(h, "GET", "/v1/week?start=2026-11-04", auth, "")
	if !col.from.Equal(time.Date(2026, 11, 2, 0, 0, 0, 0, loc)) {
		t.Errorf("start param: window from %v", col.from)
	}
	if rec := do(h, "GET", "/v1/week?start=tomorrow", auth, ""); rec.Code != 400 {
		t.Errorf("bad start: %d", rec.Code)
	}
	if rec := do(h, "GET", "/v1/week", "", ""); rec.Code != 401 {
		t.Errorf("no token: %d", rec.Code)
	}
}

func TestWeekNotConfigured(t *testing.T) {
	if rec := do(newServer(t, 10), "GET", "/v1/week", "Bearer "+testToken, ""); rec.Code != 503 {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestRateLimit(t *testing.T) {
	h := newServer(t, 3)
	var last int
	for i := 0; i < 5; i++ {
		last = do(h, "GET", "/v1/settings", "", "").Code
	}
	if last != 429 {
		t.Fatalf("last status %d, want 429", last)
	}
	// /healthz is not limited.
	if rec := do(h, "GET", "/healthz", "", ""); rec.Code != 200 {
		t.Fatalf("healthz %d", rec.Code)
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "127.0.0.1:5555"
	r.Header.Set("X-Real-IP", "203.0.113.7")
	if got := clientIP(r); got != "203.0.113.7" {
		t.Errorf("via proxy: %q", got)
	}
	r.RemoteAddr = "198.51.100.1:5555"
	if got := clientIP(r); got != "198.51.100.1" {
		t.Errorf("direct must ignore header: %q", got)
	}
}
