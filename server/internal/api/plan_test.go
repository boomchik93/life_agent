package api

import (
	"fmt"
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

func addTask(t *testing.T, h http.Handler, key, title, cat string, mins int, deadline string) int64 {
	t.Helper()
	body := fmt.Sprintf(`{"title":%q,"category":%q,"category_confirmed":true,"duration_min":%d,"deadline":%q,"priority":0}`,
		title, cat, mins, deadline)
	rec := post(h, "/v1/items", key, body)
	if rec.Code != 200 {
		t.Fatalf("add task: %d %s", rec.Code, rec.Body)
	}
	var res writtenResponse
	decodeJSON(t, rec, &res)
	return res.Item.ID
}

func request(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func buildPlan(t *testing.T, h http.Handler) planResponse {
	t.Helper()
	rec := post(h, "/v1/plan/week", "", "")
	if rec.Code != 200 {
		t.Fatalf("plan: %d %s", rec.Code, rec.Body)
	}
	var res planResponse
	decodeJSON(t, rec, &res)
	return res
}

func applyBody(p planResponse) string {
	var parts []string
	for _, c := range p.Placed {
		parts = append(parts, fmt.Sprintf(`{"item_id":%d,"start":%q,"end":%q}`, c.ItemID, c.Start.Format(time.RFC3339), c.End.Format(time.RFC3339)))
	}
	return `{"placed":[` + strings.Join(parts, ",") + `]}`
}

func minutes(p planResponse, id int64) int {
	n := 0
	for _, c := range p.Placed {
		if c.ItemID == id {
			n += int(c.End.Sub(c.Start) / time.Minute)
		}
	}
	return n
}

func TestPlanWeekPreviewWritesNothing(t *testing.T) {
	w := &fakeWriter{}
	h := writeServer(t, nil, w)
	id := addTask(t, h, "task-key-1", "Курсовая", "study", 360, "2026-10-30")

	p := buildPlan(t, h)
	if minutes(p, id) != 360 || len(p.Unplaced) != 0 || len(p.Violations) != 0 {
		t.Fatalf("plan: %+v", p)
	}
	if p.Placed[0].Title != "Курсовая" || p.Placed[0].Category != "study" {
		t.Errorf("chunk view: %+v", p.Placed[0])
	}
	if !p.Placed[0].Start.Equal(at(27, 12, 0)) {
		t.Errorf("earliest window after now: %v", p.Placed[0].Start)
	}
	if len(w.calls) != 0 {
		t.Errorf("preview wrote %d events", len(w.calls))
	}
	again := buildPlan(t, h)
	if fmt.Sprint(again.Placed) != fmt.Sprint(p.Placed) {
		t.Errorf("same data, different plan")
	}
}

func TestPlanWeekShowsMissingTime(t *testing.T) {
	h := writeServer(t, nil, &fakeWriter{})
	id := addTask(t, h, "task-key-1", "Отчёт", "work", 900, "2026-10-27")
	p := buildPlan(t, h)
	if len(p.Unplaced) != 1 || p.Unplaced[0].ItemID != id || p.Unplaced[0].MissingMin <= 0 || p.Unplaced[0].Title != "Отчёт" {
		t.Fatalf("unplaced: %+v", p.Unplaced)
	}
	if minutes(p, id)+p.Unplaced[0].MissingMin != 900 {
		t.Errorf("minutes lost: placed %d missing %d", minutes(p, id), p.Unplaced[0].MissingMin)
	}
}

func TestPlanWeekDefersLaterTasks(t *testing.T) {
	h := writeServer(t, nil, &fakeWriter{})
	id := addTask(t, h, "task-key-1", "Диплом", "study", 600, "2026-11-20")
	p := buildPlan(t, h)
	if len(p.Placed) != 0 || len(p.Deferred) != 1 || p.Deferred[0].ItemID != id {
		t.Fatalf("plan: %+v", p)
	}
}

func TestPlanApplyWritesOnceAndMarksTheTask(t *testing.T) {
	w := &fakeWriter{}
	h := writeServer(t, nil, w)
	id := addTask(t, h, "task-key-1", "Курсовая", "study", 240, "2026-10-30")
	p := buildPlan(t, h)

	rec := post(h, "/v1/plan/week/apply", "apply-key-1", applyBody(p))
	if rec.Code != 200 {
		t.Fatalf("apply: %d %s", rec.Code, rec.Body)
	}
	var res applyResponse
	decodeJSON(t, rec, &res)
	if res.Duplicate || len(res.Applied) != len(p.Placed) || len(w.calls) != len(p.Placed) {
		t.Fatalf("apply: %+v, events written %d", res, len(w.calls))
	}
	for _, c := range w.calls {
		if c.Title != "Курсовая" || c.Category != "study" {
			t.Errorf("event: %+v", c)
		}
	}

	// A double tap with the same key writes nothing more.
	rec = post(h, "/v1/plan/week/apply", "apply-key-1", applyBody(p))
	decodeJSON(t, rec, &res)
	if rec.Code != 200 || !res.Duplicate || len(w.calls) != len(p.Placed) {
		t.Fatalf("repeat: %d %+v, events %d", rec.Code, res, len(w.calls))
	}

	var items []itemView
	decodeJSON(t, request(h, "GET", "/v1/items?kind=task&status=open", ""), &items)
	if len(items) != 1 || items[0].ID != id || items[0].PlannedMin != 240 || items[0].Status != "written" {
		t.Fatalf("items: %+v", items)
	}
	// The task is planned: the next build has nothing to place.
	if next := buildPlan(t, h); len(next.Placed) != 0 || len(next.Unplaced) != 0 {
		t.Errorf("planned task must not be placed again: %+v", next)
	}
}

func TestPlanApplyRefusesAStalePlan(t *testing.T) {
	w := &fakeWriter{}
	h := writeServer(t, nil, w)
	id := addTask(t, h, "task-key-1", "Курсовая", "study", 60, "2026-10-30")
	p := buildPlan(t, h)
	if minutes(p, id) != 60 {
		t.Fatalf("plan: %+v", p)
	}

	// The same plan on a server whose calendar now has an event in that slot.
	busy := []calendar.Event{{ID: "x", Source: "google", Calendar: "c", Title: "Пара", Start: p.Placed[0].Start, End: p.Placed[0].End}}
	h2 := writeServer(t, busy, w)
	addTask(t, h2, "task-key-1", "Курсовая", "study", 60, "2026-10-30")
	rec := post(h2, "/v1/plan/week/apply", "apply-key-1", applyBody(p))
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "plan_stale") {
		t.Fatalf("stale plan: %d %s", rec.Code, rec.Body)
	}
	if len(w.calls) != 0 {
		t.Errorf("a stale plan wrote %d events", len(w.calls))
	}
}

func TestPlanApplyRefusesBrokenInput(t *testing.T) {
	h := writeServer(t, nil, &fakeWriter{})
	id := addTask(t, h, "task-key-1", "Курсовая", "study", 60, "2026-10-30")
	for name, tc := range map[string]struct{ key, body string }{
		"no key":      {"", `{"placed":[]}`},
		"empty plan":  {"apply-key-1", `{"placed":[]}`},
		"bad json":    {"apply-key-1", `[`},
		"bad time":    {"apply-key-1", fmt.Sprintf(`{"placed":[{"item_id":%d,"start":"x","end":"y"}]}`, id)},
		"sleep hours": {"apply-key-1", fmt.Sprintf(`{"placed":[{"item_id":%d,"start":"2026-10-28T02:00","end":"2026-10-28T03:00"}]}`, id)},
	} {
		rec := post(h, "/v1/plan/week/apply", tc.key, tc.body)
		if rec.Code != 400 && rec.Code != 409 {
			t.Errorf("%s: status %d %s", name, rec.Code, rec.Body)
		}
	}
}

func TestPlanNeedsAllCalendars(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	w := &fakeWriter{}
	h := New(Options{
		Store: st, Token: testToken, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		RatePerSec: 100, Burst: 100, Writer: w, Location: yekat,
		Calendar: &fakeCollector{warnings: []string{"ics: timeout"}},
		Now:      func() time.Time { return at(27, 12, 0) },
	})
	addTask(t, h, "task-key-1", "Курсовая", "study", 60, "2026-10-30")
	for _, path := range []string{"/v1/plan/week", "/v1/plan/week/apply"} {
		rec := post(h, path, "apply-key-1", `{"placed":[{"item_id":1,"start":"2026-10-27T13:00","end":"2026-10-27T14:00"}]}`)
		if rec.Code != 502 || !strings.Contains(rec.Body.String(), "calendars_unavailable") {
			t.Errorf("%s with a failed calendar: %d %s", path, rec.Code, rec.Body)
		}
	}
	if len(w.calls) != 0 {
		t.Errorf("wrote %d events", len(w.calls))
	}
}

func TestPatchItem(t *testing.T) {
	h := writeServer(t, nil, &fakeWriter{})
	id := addTask(t, h, "task-key-1", "Курсовая", "study", 60, "2026-10-30")
	rec := request(h, "PATCH", fmt.Sprintf("/v1/items/%d", id), `{"duration_min":90,"priority":2}`)
	var v itemView
	decodeJSON(t, rec, &v)
	if rec.Code != 200 || *v.DurationMin != 90 || v.Priority != 2 || v.Status != "confirmed" {
		t.Fatalf("patch: %d %+v", rec.Code, v)
	}
	if rec := request(h, "PATCH", fmt.Sprintf("/v1/items/%d", id), `{"status":"written"}`); rec.Code != 400 {
		t.Errorf("status written is set by the planner only: %d", rec.Code)
	}
	if rec := request(h, "PATCH", "/v1/items/999", `{"priority":1}`); rec.Code != 404 {
		t.Errorf("missing item: %d", rec.Code)
	}
	request(h, "PATCH", fmt.Sprintf("/v1/items/%d", id), `{"status":"done"}`)
	if p := buildPlan(t, h); len(p.Placed) != 0 {
		t.Errorf("a done task must not be planned: %+v", p)
	}
}
