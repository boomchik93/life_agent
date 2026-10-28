package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var yekat = mustLoc("Asia/Yekaterinburg")

func mustLoc(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}

func day(loc *time.Location, y int, m time.Month, d, h, mi int) time.Time {
	return time.Date(y, m, d, h, mi, 0, 0, loc)
}

func TestWeekBounds(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time
		from time.Time
	}{
		{"tuesday", day(yekat, 2026, 10, 27, 15, 0), day(yekat, 2026, 10, 26, 0, 0)},
		{"monday midnight", day(yekat, 2026, 10, 26, 0, 0), day(yekat, 2026, 10, 26, 0, 0)},
		{"sunday night", day(yekat, 2026, 11, 1, 23, 59), day(yekat, 2026, 10, 26, 0, 0)},
		{"utc instant is local monday", time.Date(2026, 10, 25, 20, 0, 0, 0, time.UTC), day(yekat, 2026, 10, 26, 0, 0)},
		{"year boundary", day(yekat, 2027, 1, 1, 10, 0), day(yekat, 2026, 12, 28, 0, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, to := WeekBounds(tt.now, yekat)
			if !from.Equal(tt.from) {
				t.Errorf("from = %v, want %v", from, tt.from)
			}
			if !to.Equal(tt.from.AddDate(0, 0, 7)) {
				t.Errorf("to = %v", to)
			}
		})
	}
}

func TestWeekBoundsDST(t *testing.T) {
	berlin := mustLoc("Europe/Berlin")
	from, to := WeekBounds(day(berlin, 2026, 10, 27, 12, 0), berlin) // clocks go back on Oct 25, before this week
	if from.Day() != 26 || to.Day() != 2 || to.Month() != 11 || to.Hour() != 0 {
		t.Errorf("from %v to %v", from, to)
	}
	from, to = WeekBounds(day(berlin, 2026, 10, 21, 12, 0), berlin) // week containing Oct 25
	if to.Sub(from) != 7*24*time.Hour+time.Hour {
		t.Errorf("week with DST change is %v long", to.Sub(from))
	}
}

// --- ICS ---

const scheduleICS = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:a1\r\nSUMMARY:Экономика  Экзамен (Экзамен)\r\n" +
	"DTSTART;TZID=Asia/Yekaterinburg:20261026T113000\r\nDTEND;TZID=Asia/Yekaterinburg:20261026T125000\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:a2\r\nSUMMARY:Деловой английский · группа 3\r\n" +
	"DTSTART;TZID=Asia/Yekaterinburg:20261027T081000\r\nDTEND;TZID=Asia/Yekaterinburg:20261027T093000\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:a3\r\nSUMMARY:Next week\r\nDTSTART;TZID=Asia/Yekaterinburg:20261103T081000\r\nDTEND;TZID=Asia/Yekaterinburg:20261103T093000\r\nEND:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

func week() (time.Time, time.Time) { return WeekBounds(day(yekat, 2026, 10, 27, 12, 0), yekat) }

func TestParseICSTZID(t *testing.T) {
	from, to := week()
	ev, warn := ParseICS(scheduleICS, "Расписание", yekat, from, to)
	if len(warn) != 0 {
		t.Fatalf("warnings %v", warn)
	}
	if len(ev) != 2 {
		t.Fatalf("got %d events: %+v", len(ev), ev)
	}
	if !ev[0].Start.Equal(day(yekat, 2026, 10, 26, 11, 30)) || !ev[0].End.Equal(day(yekat, 2026, 10, 26, 12, 50)) {
		t.Errorf("event 0: %v - %v", ev[0].Start, ev[0].End)
	}
	if ev[0].Title != "Экономика  Экзамен (Экзамен)" || ev[0].Source != "ics" || ev[0].Calendar != "Расписание" {
		t.Errorf("event 0 meta: %+v", ev[0])
	}
}

func TestParseICSFormats(t *testing.T) {
	ics := strings.Join([]string{
		"BEGIN:VEVENT", "UID:utc", "SUMMARY:Utc",
		"DTSTART:20261027T040000Z", "DTEND:20261027T050000Z", "END:VEVENT",
		"BEGIN:VEVENT", "UID:float", "SUMMARY:Floating", "DTSTART:20261027T100000", "DURATION:PT1H30M", "END:VEVENT",
		"BEGIN:VEVENT", "UID:allday", "SUMMARY:Holiday", "DTSTART;VALUE=DATE:20261028", "DTEND;VALUE=DATE:20261030", "END:VEVENT",
		"BEGIN:VEVENT", "UID:cancelled", "SUMMARY:Gone", "STATUS:CANCELLED", "DTSTART:20261027T120000", "DTEND:20261027T130000", "END:VEVENT",
		"BEGIN:VEVENT", "UID:esc", "SUMMARY:A\\, B\\; C", "DTSTART:20261029T100000", "DTEND:20261029T110000", "END:VEVENT",
		"BEGIN:VEVENT", "UID:fold", "SUMMARY:Long ti", " tle", "DTSTART:20261029T120000", "DTEND:20261029T130000", "END:VEVENT",
	}, "\r\n")
	from, to := week()
	ev, warn := ParseICS(ics, "c", yekat, from, to)
	if len(warn) != 0 {
		t.Fatalf("warnings %v", warn)
	}
	byTitle := map[string]Event{}
	for _, e := range ev {
		byTitle[e.Title] = e
	}
	if len(ev) != 5 {
		t.Fatalf("got %d: %+v", len(ev), ev)
	}
	if e := byTitle["Utc"]; !e.Start.Equal(day(yekat, 2026, 10, 27, 9, 0)) || e.Start.Location() != yekat {
		t.Errorf("utc event %v (%v)", e.Start, e.Start.Location())
	}
	if e := byTitle["Floating"]; !e.Start.Equal(day(yekat, 2026, 10, 27, 10, 0)) || e.End.Sub(e.Start) != 90*time.Minute {
		t.Errorf("floating event %v - %v", e.Start, e.End)
	}
	h := byTitle["Holiday"]
	if !h.AllDay || !h.Start.Equal(day(yekat, 2026, 10, 28, 0, 0)) || !h.End.Equal(day(yekat, 2026, 10, 30, 0, 0)) {
		t.Errorf("all-day event %+v", h)
	}
	if _, ok := byTitle["Gone"]; ok {
		t.Error("cancelled event returned")
	}
	if _, ok := byTitle["A, B; C"]; !ok {
		t.Errorf("unescaped title missing: %v", byTitle)
	}
	if _, ok := byTitle["Long title"]; !ok {
		t.Errorf("folded title missing: %v", byTitle)
	}
}

func TestParseICSIgnoresAlarmProps(t *testing.T) {
	ics := strings.Join([]string{
		"BEGIN:VEVENT", "UID:a", "DTSTART:20261027T100000",
		"BEGIN:VALARM", "ACTION:EMAIL", "SUMMARY:Напоминание", "TRIGGER:-PT15M", "DURATION:PT5M", "REPEAT:2", "END:VALARM",
		"SUMMARY:Пара", "DTEND:20261027T113000", "END:VEVENT",
	}, "\r\n")
	from, to := week()
	ev, warn := ParseICS(ics, "c", yekat, from, to)
	if len(warn) != 0 || len(ev) != 1 {
		t.Fatalf("events %+v warnings %v", ev, warn)
	}
	if ev[0].Title != "Пара" || ev[0].End.Sub(ev[0].Start) != 90*time.Minute {
		t.Errorf("alarm props leaked into event: %+v", ev[0])
	}
}

func TestParseICSAllDayAcrossDST(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	// 2026-03-29 is 23 hours long in Berlin.
	ics := "BEGIN:VEVENT\r\nUID:d\r\nSUMMARY:Day\r\nDTSTART;VALUE=DATE:20260329\r\nDTEND;VALUE=DATE:20260330\r\nEND:VEVENT"
	from := time.Date(2026, 3, 23, 0, 0, 0, 0, berlin)
	ev, _ := ParseICS(ics, "c", berlin, from, from.AddDate(0, 0, 7))
	if len(ev) != 1 || !ev[0].End.Equal(time.Date(2026, 3, 30, 0, 0, 0, 0, berlin)) {
		t.Fatalf("23h all-day event: %+v", ev)
	}
}

func TestParseICSRecurrence(t *testing.T) {
	from, to := week() // Mon 2026-10-26 .. Mon 2026-11-02, Yekaterinburg
	tests := []struct {
		name string
		body string
		want []string // local start days, "27 09:00"
	}{
		{"weekly byday", "DTSTART;TZID=Asia/Yekaterinburg:20260914T090000\r\nDTEND;TZID=Asia/Yekaterinburg:20260914T104000\r\nRRULE:FREQ=WEEKLY;BYDAY=MO,WE,FR",
			[]string{"26 09:00", "28 09:00", "30 09:00"}},
		{"weekly default day", "DTSTART;TZID=Asia/Yekaterinburg:20260915T090000\r\nDTEND;TZID=Asia/Yekaterinburg:20260915T104000\r\nRRULE:FREQ=WEEKLY",
			[]string{"27 09:00"}},
		{"biweekly hits", "DTSTART;TZID=Asia/Yekaterinburg:20261012T090000\r\nDTEND;TZID=Asia/Yekaterinburg:20261012T104000\r\nRRULE:FREQ=WEEKLY;INTERVAL=2",
			[]string{"26 09:00"}},
		{"biweekly misses", "DTSTART;TZID=Asia/Yekaterinburg:20261019T090000\r\nDTEND;TZID=Asia/Yekaterinburg:20261019T104000\r\nRRULE:FREQ=WEEKLY;INTERVAL=2",
			nil},
		{"count exhausted", "DTSTART;TZID=Asia/Yekaterinburg:20261012T090000\r\nDTEND;TZID=Asia/Yekaterinburg:20261012T104000\r\nRRULE:FREQ=WEEKLY;COUNT=2",
			nil},
		{"until before week", "DTSTART;TZID=Asia/Yekaterinburg:20260914T090000\r\nDTEND;TZID=Asia/Yekaterinburg:20260914T104000\r\nRRULE:FREQ=DAILY;UNTIL=20261027T000000Z",
			[]string{"26 09:00"}},
		{"daily with exdate", "DTSTART;TZID=Asia/Yekaterinburg:20261025T090000\r\nDTEND;TZID=Asia/Yekaterinburg:20261025T100000\r\nRRULE:FREQ=DAILY;COUNT=4\r\nEXDATE;TZID=Asia/Yekaterinburg:20261027T090000",
			[]string{"26 09:00", "28 09:00"}},
		{"monthly", "DTSTART;TZID=Asia/Yekaterinburg:20260827T090000\r\nDTEND;TZID=Asia/Yekaterinburg:20260827T100000\r\nRRULE:FREQ=MONTHLY",
			[]string{"27 09:00"}},
		{"yearly", "DTSTART;TZID=Asia/Yekaterinburg:20241030T090000\r\nDTEND;TZID=Asia/Yekaterinburg:20241030T100000\r\nRRULE:FREQ=YEARLY",
			[]string{"30 09:00"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ics := "BEGIN:VEVENT\r\nUID:r\r\nSUMMARY:S\r\n" + tt.body + "\r\nEND:VEVENT\r\n"
			ev, warn := ParseICS(ics, "c", yekat, from, to)
			if len(warn) != 0 {
				t.Fatalf("warnings %v", warn)
			}
			var got []string
			for _, e := range ev {
				got = append(got, e.Start.Format("2 15:04"))
				if !e.Recurring {
					t.Error("instance not marked recurring")
				}
			}
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseICSRecurrenceOverrideAndUnsupported(t *testing.T) {
	from, to := week()
	ics := strings.Join([]string{
		"BEGIN:VEVENT", "UID:s", "SUMMARY:Lecture", "DTSTART;TZID=Asia/Yekaterinburg:20260914T090000",
		"DTEND;TZID=Asia/Yekaterinburg:20260914T104000", "RRULE:FREQ=WEEKLY;BYDAY=TU", "END:VEVENT",
		"BEGIN:VEVENT", "UID:s", "SUMMARY:Lecture moved", "RECURRENCE-ID;TZID=Asia/Yekaterinburg:20261027T090000",
		"DTSTART;TZID=Asia/Yekaterinburg:20261027T150000", "DTEND;TZID=Asia/Yekaterinburg:20261027T164000", "END:VEVENT",
		"BEGIN:VEVENT", "UID:u", "SUMMARY:Odd", "DTSTART;TZID=Asia/Yekaterinburg:20261029T100000",
		"DTEND;TZID=Asia/Yekaterinburg:20261029T110000", "RRULE:FREQ=MONTHLY;BYSETPOS=1;BYDAY=TH", "END:VEVENT",
	}, "\r\n")
	ev, warn := ParseICS(ics, "c", yekat, from, to)
	if len(warn) != 1 || !strings.Contains(warn[0], "Odd") {
		t.Fatalf("warnings %v", warn)
	}
	titles := map[string]string{}
	for _, e := range ev {
		titles[e.Title] = e.Start.Format("2 15:04")
	}
	if titles["Lecture moved"] != "27 15:00" {
		t.Errorf("override missing: %v", titles)
	}
	if _, dup := titles["Lecture"]; dup {
		t.Errorf("overridden instance still present: %v", titles)
	}
	if titles["Odd"] != "29 10:00" {
		t.Errorf("unsupported rule should show the first event once: %v", titles)
	}
}

func TestRecurrenceKeepsWallClockAcrossDST(t *testing.T) {
	berlin := mustLoc("Europe/Berlin") // clocks go back Sun 2026-10-25 03:00
	ics := "BEGIN:VEVENT\r\nUID:d\r\nSUMMARY:S\r\nDTSTART;TZID=Europe/Berlin:20261019T090000\r\nDTEND;TZID=Europe/Berlin:20261019T100000\r\nRRULE:FREQ=DAILY;COUNT=10\r\nEND:VEVENT\r\n"
	from, to := day(berlin, 2026, 10, 19, 0, 0), day(berlin, 2026, 10, 29, 0, 0)
	ev, _ := ParseICS(ics, "c", berlin, from, to)
	if len(ev) != 10 {
		t.Fatalf("got %d events", len(ev))
	}
	for _, e := range ev {
		if e.Start.Hour() != 9 {
			t.Errorf("%v drifted off 09:00 local", e.Start)
		}
	}
}

func TestParseDuration(t *testing.T) {
	tests := map[string]time.Duration{
		"PT1H":     time.Hour,
		"PT1H30M":  90 * time.Minute,
		"P1D":      24 * time.Hour,
		"P1W":      7 * 24 * time.Hour,
		"P1DT2H":   26 * time.Hour,
		"-PT15M":   -15 * time.Minute,
		"PT0S":     0,
		"PT45M30S": 45*time.Minute + 30*time.Second,
	}
	for in, want := range tests {
		got, err := parseDuration(in)
		if err != nil || got != want {
			t.Errorf("%q = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "1H", "PT", "P1H", "PTxH"} {
		if _, err := parseDuration(bad); err == nil && bad != "PT" {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestICSFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("fail") != "" {
			http.Error(w, "boom", 500)
			return
		}
		w.Write([]byte(scheduleICS))
	}))
	defer srv.Close()
	from, to := week()

	ev, err := NewICS(srv.URL+"/feed.ics?group=secret", "S", yekat).Events(context.Background(), from, to)
	if err != nil || len(ev) != 2 {
		t.Fatalf("events %d, err %v", len(ev), err)
	}
	_, err = NewICS(srv.URL+"/feed.ics?fail=1&group=secret", "S", yekat).Events(context.Background(), from, to)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("error must exist and not leak the URL: %v", err)
	}
	_, err = NewICS("http://127.0.0.1:1/x?group=secret", "S", yekat).Events(context.Background(), from, to)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("transport error must not leak the URL: %v", err)
	}
}

func TestICSReusesFeedAndAsksIfChanged(t *testing.T) {
	var gets, notModified int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gets++
		if r.Header.Get("If-None-Match") == `"v1"` {
			notModified++
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Write([]byte(scheduleICS))
	}))
	defer srv.Close()
	from, to := week()
	now := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	c := NewICS(srv.URL+"/feed.ics", "S", yekat)
	c.now = func() time.Time { return now }

	for range 2 {
		if ev, err := c.Events(context.Background(), from, to); err != nil || len(ev) != 2 {
			t.Fatalf("events %d, err %v", len(ev), err)
		}
	}
	if gets != 1 {
		t.Errorf("gets %d, want 1: a recent copy is reused", gets)
	}
	now = now.Add(icsReuse)
	if ev, err := c.Events(context.Background(), from, to); err != nil || len(ev) != 2 {
		t.Fatalf("after 304: events %d, err %v", len(ev), err)
	}
	if gets != 2 || notModified != 1 {
		t.Errorf("gets %d, not modified %d", gets, notModified)
	}
}

// --- Google ---

func fakeGoogle(t *testing.T, handler http.HandlerFunc) *Google {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Google{client: srv.Client(), baseURL: srv.URL, calendarIDs: []string{"me@example.com"}, loc: yekat}
}

func TestGoogleEvents(t *testing.T) {
	var queries []string
	g := fakeGoogle(t, func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		q := r.URL.Query()
		if q.Get("singleEvents") != "true" {
			t.Error("series must be expanded server-side")
		}
		if q.Get("pageToken") == "" {
			json.NewEncoder(w).Encode(map[string]any{
				"summary": "Личный", "nextPageToken": "p2",
				"items": []map[string]any{
					{"id": "1", "summary": "КВН", "start": map[string]string{"dateTime": "2026-10-30T18:00:00+05:00"}, "end": map[string]string{"dateTime": "2026-10-30T20:00:00+05:00"}},
					{"id": "2", "status": "cancelled", "summary": "X", "start": map[string]string{"dateTime": "2026-10-30T10:00:00+05:00"}, "end": map[string]string{"dateTime": "2026-10-30T11:00:00+05:00"}},
				},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"summary": "Личный",
			"items": []map[string]any{
				{"id": "3_20261031", "recurringEventId": "3", "summary": "Зарядка", "start": map[string]string{"dateTime": "2026-10-31T04:00:00Z"}, "end": map[string]string{"dateTime": "2026-10-31T05:00:00Z"}},
				{"id": "4", "start": map[string]string{"date": "2026-10-28"}, "end": map[string]string{"date": "2026-10-29"}},
			},
		})
	})
	from, to := week()
	ev, err := g.Events(context.Background(), from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) != 2 {
		t.Errorf("pages fetched: %d", len(queries))
	}
	if len(ev) != 3 {
		t.Fatalf("got %d events: %+v", len(ev), ev)
	}
	if ev[0].Title != "КВН" || ev[0].Calendar != "Личный" || ev[0].Source != "google" || ev[0].AllDay {
		t.Errorf("event 0 %+v", ev[0])
	}
	if !ev[1].Start.Equal(day(yekat, 2026, 10, 31, 9, 0)) || ev[1].Start.Location() != yekat || !ev[1].Recurring {
		t.Errorf("event 1 %+v", ev[1])
	}
	if !ev[2].AllDay || ev[2].Title != "(без названия)" || !ev[2].Start.Equal(day(yekat, 2026, 10, 28, 0, 0)) || !ev[2].End.Equal(day(yekat, 2026, 10, 29, 0, 0)) {
		t.Errorf("event 2 %+v", ev[2])
	}
}

func TestGoogleErrors(t *testing.T) {
	g := fakeGoogle(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "nope", 404) })
	from, to := week()
	if _, err := g.Events(context.Background(), from, to); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("err = %v", err)
	}
}

// --- Aggregator ---

type fakeProvider struct {
	ev  []Event
	err error
}

func (f fakeProvider) Events(context.Context, time.Time, time.Time) ([]Event, error) {
	return f.ev, f.err
}

func TestAggregator(t *testing.T) {
	a := day(yekat, 2026, 10, 27, 9, 0)
	b := day(yekat, 2026, 10, 27, 8, 0)
	agg := &Aggregator{Sources: []Named{
		{"google", fakeProvider{ev: []Event{{ID: "g", Start: a, End: a.Add(time.Hour)}}}},
		{"ics", fakeProvider{ev: []Event{{ID: "i", Start: b, End: b.Add(time.Hour)}}}},
		{"broken", fakeProvider{err: errors.New("timeout")}},
	}}
	from, to := week()
	ev, warn := agg.Collect(context.Background(), from, to)
	if len(ev) != 2 || ev[0].ID != "i" || ev[1].ID != "g" {
		t.Errorf("order/content: %+v", ev)
	}
	if len(warn) != 1 || warn[0] != "broken: timeout" {
		t.Errorf("warnings %v", warn)
	}
}

func TestEventIDIsValidAndStable(t *testing.T) {
	a, b := EventID("key-1"), EventID("key-1")
	if a != b || a == EventID("key-2") {
		t.Fatalf("id must follow the key: %q %q", a, b)
	}
	if len(a) < 5 || len(a) > 1024 {
		t.Fatalf("bad length %d", len(a))
	}
	for _, r := range a {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'v') {
			t.Fatalf("character %q is not base32hex", r)
		}
	}
}

func TestWriterInsert(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Yekaterinburg")
	var status = 200
	var got map[string]any
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.EscapedPath()
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(status)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	w := &Writer{client: srv.Client(), baseURL: srv.URL, calendarID: "me@example.com", loc: loc}
	ev := NewEvent{ID: "abcde", Title: "КВН", Category: "hobby", Recurrence: "FREQ=WEEKLY;BYDAY=FR",
		Start: time.Date(2026, 10, 30, 18, 0, 0, 0, loc), End: time.Date(2026, 10, 30, 20, 0, 0, 0, loc)}

	created, err := w.Insert(context.Background(), ev)
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if path != "/calendars/me@example.com/events" && path != "/calendars/me%40example.com/events" {
		t.Errorf("path %q", path)
	}
	start := got["start"].(map[string]any)
	if start["dateTime"] != "2026-10-30T18:00:00+05:00" || start["timeZone"] != "Asia/Yekaterinburg" {
		t.Errorf("start %v", start)
	}
	if got["id"] != "abcde" || got["summary"] != "КВН" {
		t.Errorf("body %v", got)
	}
	if rec := got["recurrence"].([]any); rec[0] != "RRULE:FREQ=WEEKLY;BYDAY=FR" {
		t.Errorf("recurrence %v", rec)
	}
	priv := got["extendedProperties"].(map[string]any)["private"].(map[string]any)
	if priv["category"] != "hobby" {
		t.Errorf("category not kept: %v", priv)
	}

	status = 409
	if created, err = w.Insert(context.Background(), ev); err != nil || created {
		t.Errorf("duplicate id: created=%v err=%v", created, err)
	}
	status = 403
	if _, err = w.Insert(context.Background(), ev); !errors.Is(err, ErrNoWriteAccess) {
		t.Errorf("403 must be ErrNoWriteAccess, got %v", err)
	}
	status = 500
	if _, err = w.Insert(context.Background(), ev); err == nil || errors.Is(err, ErrNoWriteAccess) {
		t.Errorf("500 must be a plain error, got %v", err)
	}
}
