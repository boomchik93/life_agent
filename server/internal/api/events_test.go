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

type fakeWriter struct {
	calls []calendar.NewEvent
	seen  map[string]bool
	err   error
}

func (f *fakeWriter) Insert(_ context.Context, e calendar.NewEvent) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	f.calls = append(f.calls, e)
	if f.seen == nil {
		f.seen = map[string]bool{}
	}
	dup := f.seen[e.ID]
	f.seen[e.ID] = true
	return !dup, nil
}

var yekat, _ = time.LoadLocation("Asia/Yekaterinburg")

func at(day, h, m int) time.Time { return time.Date(2026, 10, day, h, m, 0, 0, yekat) }

// writeServer: "now" is Tuesday 27 Oct 2026, 12:00; the week has [events].
func writeServer(t *testing.T, events []calendar.Event, w EventWriter) http.Handler {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(Options{
		Store: st, Token: testToken, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		RatePerSec: 100, Burst: 100, Calendar: &fakeCollector{events: events}, Writer: w, Location: yekat,
		Now: func() time.Time { return at(27, 12, 0) },
	})
}

func post(h http.Handler, path, key, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func slotBody(title string, from, to time.Time, extra string) string {
	return `{"title":"` + title + `","start":"` + from.Format(time.RFC3339) + `","end":"` +
		to.Format(time.RFC3339) + `","category":"hobby"` + extra + `}`
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("bad json %q: %v", rec.Body.String(), err)
	}
}

func TestCheckFreeSlotAsksTheCategory(t *testing.T) {
	h := writeServer(t, nil, &fakeWriter{})
	rec := post(h, "/v1/check", "", slotBody("КВН", at(30, 18, 0), at(30, 20, 0), ""))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var res checkResponse
	decodeJSON(t, rec, &res)
	if !res.OK || res.NeedsConfirm || len(res.Overlaps) != 0 {
		t.Errorf("free slot must be fine: %+v", res)
	}
	if !res.AskCategory || res.Category != "hobby" || res.CategorySrc != "guess" {
		t.Errorf("unknown title: ask for the category: %+v", res)
	}

	rec = post(h, "/v1/check", "", slotBody("КВН", at(30, 18, 0), at(30, 20, 0), `,"category_confirmed":true`))
	decodeJSON(t, rec, &res)
	if res.AskCategory || res.CategorySrc != "user" {
		t.Errorf("confirmed category must not be asked again: %+v", res)
	}
}

func TestCheckOverlapNeedsConfirm(t *testing.T) {
	ev := []calendar.Event{{ID: "1", Source: "google", Calendar: "x", Title: "Пара", Start: at(30, 17, 0), End: at(30, 19, 0)}}
	h := writeServer(t, ev, &fakeWriter{})
	var res checkResponse
	decodeJSON(t, post(h, "/v1/check", "", slotBody("КВН", at(30, 18, 0), at(30, 20, 0), "")), &res)
	if res.OK || !res.NeedsConfirm || len(res.Overlaps) != 1 || res.Overlaps[0].Title != "Пара" {
		t.Fatalf("overlap: %+v", res)
	}
	if !strings.Contains(res.Reasons[0], "Пересекается с «Пара» (17:00–19:00)") {
		t.Errorf("reason: %q", res.Reasons[0])
	}
	// All-day events do not block a slot.
	ev[0].AllDay = true
	decodeJSON(t, post(h, "/v1/check", "", slotBody("КВН", at(30, 18, 0), at(30, 20, 0), "")), &res)
	if len(res.Overlaps) != 0 {
		t.Errorf("all-day event counted as overlap: %+v", res.Overlaps)
	}
}

func TestCheckStudyOverloadTurnsRed(t *testing.T) {
	// 9 h of classes on Thursday in blocks of 3 h with breaks: yellow, not red.
	// Two more hours of study make the day 11 h (> 9 h = red).
	ev := []calendar.Event{
		{ID: "1", Source: "ics", Calendar: "Расписание", Title: "Пары 1", Start: at(29, 8, 0), End: at(29, 11, 0)},
		{ID: "2", Source: "ics", Calendar: "Расписание", Title: "Пары 2", Start: at(29, 12, 0), End: at(29, 15, 0)},
		{ID: "3", Source: "ics", Calendar: "Расписание", Title: "Пары 3", Start: at(29, 16, 0), End: at(29, 19, 0)},
	}
	h := writeServer(t, ev, &fakeWriter{})
	body := slotBody("Курсовая", at(29, 20, 0), at(29, 22, 0), `,"category_confirmed":true`)
	body = strings.Replace(body, `"hobby"`, `"study"`, 1)
	var res checkResponse
	decodeJSON(t, post(h, "/v1/check", "", body), &res)
	if res.Before.Overload.Color == "red" || res.After.Overload.Color != "red" {
		t.Fatalf("overload must turn red: before %s after %s", res.Before.Overload.Color, res.After.Overload.Color)
	}
	if !res.NeedsConfirm || len(res.Worsened) == 0 || res.Worsened[0] != "overload" {
		t.Errorf("a new red risk needs confirmation: %+v", res)
	}
}

func TestCheckRejectsBadInput(t *testing.T) {
	h := writeServer(t, nil, &fakeWriter{})
	for name, body := range map[string]string{
		"empty title":  slotBody("  ", at(30, 18, 0), at(30, 20, 0), ""),
		"end<start":    slotBody("x", at(30, 20, 0), at(30, 18, 0), ""),
		"over a day":   slotBody("x", at(30, 8, 0), at(31, 9, 0), ""),
		"bad category": strings.Replace(slotBody("x", at(30, 8, 0), at(30, 9, 0), ""), "hobby", "party", 1),
		"bad time":     `{"title":"x","start":"завтра","end":"потом","category":"hobby"}`,
		"bad rrule":    slotBody("x", at(30, 8, 0), at(30, 9, 0), `,"recurrence":"FREQ=SOMETIMES"`),
		"not json":     `x`,
	} {
		if rec := post(h, "/v1/check", "", body); rec.Code != 400 {
			t.Errorf("%s: status %d, want 400", name, rec.Code)
		}
	}
}

func TestEventsWriteOnceAndRememberCategory(t *testing.T) {
	fw := &fakeWriter{}
	h := writeServer(t, nil, fw)
	body := slotBody("КВН в пятницу", at(30, 18, 0), at(30, 20, 0), `,"category_confirmed":true,"recurrence":"FREQ=WEEKLY;BYDAY=FR"`)

	if rec := post(h, "/v1/events", "", body); rec.Code != 400 {
		t.Fatalf("no Idempotency-Key must be 400, got %d", rec.Code)
	}
	rec := post(h, "/v1/events", "key-aaaa-1111", body)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var first writtenResponse
	decodeJSON(t, rec, &first)
	if first.Duplicate || first.Item.EventID == nil || first.Item.Status != "written" {
		t.Fatalf("first write: %+v", first)
	}
	if len(fw.calls) != 1 || fw.calls[0].Recurrence != "FREQ=WEEKLY;BYDAY=FR" || fw.calls[0].Category != "hobby" {
		t.Fatalf("calendar call: %+v", fw.calls)
	}

	// The same key again (a double tap): the same event, nothing new in the calendar.
	var second writtenResponse
	decodeJSON(t, post(h, "/v1/events", "key-aaaa-1111", body), &second)
	if !second.Duplicate || second.Item.ID != first.Item.ID || len(fw.calls) != 1 {
		t.Fatalf("repeat must not write again: %+v calls=%d", second, len(fw.calls))
	}

	// The answer is remembered under the normalized title.
	var res checkResponse
	decodeJSON(t, post(h, "/v1/check", "", slotBody("КВН в субботу 12:00", at(31, 12, 0), at(31, 13, 0), "")), &res)
	if res.AskCategory || res.CategorySrc != "memory" || res.Category != "hobby" {
		t.Errorf("memory must answer the category: %+v", res)
	}
}

func TestEventsRequireConfirmationForRisk(t *testing.T) {
	ev := []calendar.Event{{ID: "1", Source: "google", Calendar: "x", Title: "Пара", Start: at(30, 18, 0), End: at(30, 19, 0)}}
	fw := &fakeWriter{}
	h := writeServer(t, ev, fw)
	body := slotBody("КВН", at(30, 18, 30), at(30, 20, 0), `,"category_confirmed":true`)

	rec := post(h, "/v1/events", "key-bbbb-2222", body)
	if rec.Code != 409 || len(fw.calls) != 0 {
		t.Fatalf("unconfirmed overlap: status %d calls %d", rec.Code, len(fw.calls))
	}
	var conflict struct {
		Code  string        `json:"code"`
		Check checkResponse `json:"check"`
	}
	decodeJSON(t, rec, &conflict)
	if conflict.Code != "needs_confirm" || len(conflict.Check.Overlaps) != 1 {
		t.Errorf("409 must carry the check: %+v", conflict)
	}

	rec = post(h, "/v1/events", "key-bbbb-2222", strings.TrimSuffix(body, "}")+`,"confirmed":true}`)
	if rec.Code != 200 || len(fw.calls) != 1 {
		t.Fatalf("accepted warning: status %d calls %d", rec.Code, len(fw.calls))
	}
}

func TestEventsNeedAConfirmedCategory(t *testing.T) {
	fw := &fakeWriter{}
	h := writeServer(t, nil, fw)
	rec := post(h, "/v1/events", "key-cccc-3333", slotBody("Йога", at(30, 9, 0), at(30, 10, 0), ""))
	if rec.Code != 409 || len(fw.calls) != 0 {
		t.Fatalf("status %d calls %d: %s", rec.Code, len(fw.calls), rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "category_unconfirmed") {
		t.Errorf("body %s", rec.Body)
	}
}

func TestEventsWriteFailures(t *testing.T) {
	body := slotBody("КВН", at(30, 18, 0), at(30, 20, 0), `,"category_confirmed":true`)

	h := writeServer(t, nil, &fakeWriter{err: calendar.ErrNoWriteAccess})
	rec := post(h, "/v1/events", "key-dddd-4444", body)
	if rec.Code != 502 || !strings.Contains(rec.Body.String(), "no_write_access") {
		t.Errorf("no access: %d %s", rec.Code, rec.Body)
	}
	// A failed write leaves no row: the retry with the same key is a real attempt.
	if rec := post(h, "/v1/events", "key-dddd-4444", body); rec.Code != 502 {
		t.Errorf("retry: %d", rec.Code)
	}

	h = writeServer(t, nil, nil)
	if rec := post(h, "/v1/events", "key-eeee-5555", body); rec.Code != 503 || !strings.Contains(rec.Body.String(), "write_not_configured") {
		t.Errorf("no writer: %d %s", rec.Code, rec.Body)
	}
}

func TestItemsSaveATaskOnce(t *testing.T) {
	h := writeServer(t, nil, &fakeWriter{})
	body := `{"title":"Курсовая","category":"study","category_confirmed":true,"duration_min":360,"deadline":"2026-10-30"}`
	var first, second writtenResponse
	rec := post(h, "/v1/items", "task-key-0001", body)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	decodeJSON(t, rec, &first)
	decodeJSON(t, post(h, "/v1/items", "task-key-0001", body), &second)
	if first.Item.Kind != "task" || first.Item.Status != "confirmed" || *first.Item.DurationMin != 360 {
		t.Errorf("task: %+v", first.Item)
	}
	if !second.Duplicate || second.Item.ID != first.Item.ID {
		t.Errorf("repeat: %+v", second)
	}

	for name, b := range map[string]string{
		"no duration": `{"title":"x","category":"study","duration_min":0,"deadline":"2026-10-30"}`,
		"bad date":    `{"title":"x","category":"study","duration_min":60,"deadline":"пятница"}`,
		"bad cat":     `{"title":"x","category":"fun","duration_min":60,"deadline":"2026-10-30"}`,
	} {
		if rec := post(h, "/v1/items", "task-key-0002", b); rec.Code != 400 {
			t.Errorf("%s: status %d", name, rec.Code)
		}
	}
	if rec := post(h, "/v1/items", "", body); rec.Code != 400 {
		t.Errorf("no key: %d", rec.Code)
	}
}

func TestSlotAcceptsWallClockTime(t *testing.T) {
	h := writeServer(t, nil, &fakeWriter{})
	body := `{"title":"КВН","start":"2026-10-30T18:00","end":"2026-10-30T20:00","category":"hobby","category_confirmed":true}`
	fw := &fakeWriter{}
	h = writeServer(t, nil, fw)
	if rec := post(h, "/v1/events", "wall-key-0001", body); rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if got := fw.calls[0].Start; !got.Equal(at(30, 18, 0)) {
		t.Errorf("start %v, want 18:00 in the user's zone", got)
	}
}
