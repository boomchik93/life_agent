package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"lifeagent/internal/calendar"
	"lifeagent/internal/store"
)

// clockServer is a server whose clock the test moves.
func clockServer(t *testing.T, events []calendar.Event, clock *time.Time) (http.Handler, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h := New(Options{
		Store: st, Token: testToken, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		RatePerSec: 100, Burst: 100, Calendar: &fakeCollector{events: events}, Location: yekat,
		Now: func() time.Time { return *clock },
	})
	return h, st
}

func mustJSON(t *testing.T, h http.Handler, method, path, body string, want int, out any) {
	t.Helper()
	rec := request(h, method, path, body)
	if rec.Code != want {
		t.Fatalf("%s %s: %d, want %d: %s", method, path, rec.Code, want, rec.Body)
	}
	if out != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("%s %s: %v: %s", method, path, err, rec.Body)
		}
	}
}

func studyEvent(title string, start, end time.Time) calendar.Event {
	return calendar.Event{ID: title, Source: "ics", Calendar: "x", Title: title, Start: start, End: end, Category: "study"}
}

func TestOverviewSumsTheCalendarByCategoryAndDay(t *testing.T) {
	clock := at(28, 9, 0)
	events := []calendar.Event{
		studyEvent("Лекция", at(27, 10, 0), at(27, 12, 0)),
		{ID: "n", Source: "ics", Calendar: "x", Title: "Сон на ходу", Start: at(27, 23, 0), End: at(28, 1, 0), Category: "rest"},
		{ID: "a", Source: "ics", Calendar: "x", Title: "День рождения", Start: at(29, 0, 0), End: at(30, 0, 0), AllDay: true, Category: "chores"},
	}
	h, st := clockServer(t, events, &clock)
	if err := st.ReplaceActivity("2026-10-27", "android_usage", []store.ActivityRow{
		{Category: "study", Minutes: 50}, {Category: "unknown", Minutes: 20}, {Category: "social", Minutes: 15}}); err != nil {
		t.Fatal(err)
	}

	var ov overviewResponse
	mustJSON(t, h, "GET", "/v1/time/overview?from=2026-10-26&to=2026-11-02&compare=1", ``, 200, &ov)
	if ov.ByCategory["study"] != 120 || ov.ByCategory["rest"] != 120 || ov.TotalMin != 240 {
		t.Fatalf("an all-day event counts no hours: %+v", ov)
	}
	// The event that crosses midnight is split between the two days.
	if len(ov.Days) != 7 || ov.Days[1].ByCategory["rest"] != 60 || ov.Days[2].ByCategory["rest"] != 60 {
		t.Fatalf("days: %+v", ov.Days)
	}
	if ov.Previous == nil || total(ov.Previous) != 0 {
		t.Fatalf("previous week is empty: %+v", ov.Previous)
	}
	if ov.Screen["study"] != 50 || ov.Screen["other"] != 15 || ov.ScreenUnknown != 20 {
		t.Fatalf("screen time by category of the tab: %+v unknown %d", ov.Screen, ov.ScreenUnknown)
	}
	mustJSON(t, h, "GET", "/v1/time/overview?from=2026-11-02&to=2026-10-26", ``, 400, nil)
}

func TestCategoryDetail(t *testing.T) {
	clock := at(27, 10, 0)
	events := []calendar.Event{
		studyEvent("Лекция", at(27, 10, 0), at(27, 11, 30)),
		studyEvent("лекция", at(28, 10, 0), at(28, 11, 0)),
	}
	h, _ := clockServer(t, events, &clock)
	clock = at(29, 9, 0)
	var c categoryResponse
	mustJSON(t, h, "GET", "/v1/time/category?category=study&from=2026-10-26&to=2026-11-02", ``, 200, &c)
	if c.TotalMin != 150 || c.SharePct != 100 {
		t.Fatalf("%+v", c)
	}
	if len(c.Parts) != 1 || c.Parts[0].Minutes != 150 {
		t.Fatalf("the two lectures are one part: %+v", c.Parts)
	}
	if len(c.Records) != 2 || c.Records[0].Source != "calendar" {
		t.Fatalf("records: %+v", c.Records)
	}
	mustJSON(t, h, "GET", "/v1/time/category?category=nope", ``, 400, nil)
}

func TestActivitiesFollowTheGoalsByThemselves(t *testing.T) {
	clock := at(28, 12, 0)
	// The lecture on the 27th is two hours in the calendar, more than the phone saw.
	events := []calendar.Event{studyEvent("Лекция", at(27, 10, 0), at(27, 12, 0))}
	h, st := clockServer(t, events, &clock)
	if err := st.SetGoalNorm("study", 480, "x"); err != nil {
		t.Fatal(err)
	}
	for day, min := range map[string]int{"2026-10-26": 60, "2026-10-27": 60, "2026-10-28": 60} {
		if err := st.ReplaceActivity(day, "android_usage", []store.ActivityRow{{Category: "study", Minutes: min}}); err != nil {
			t.Fatal(err)
		}
	}
	// GitHub reports a count and no minutes; the watch reports a workout.
	if err := st.ReplaceActivity("2026-10-27", "github", []store.ActivityRow{{Category: "career", Count: 4}}); err != nil {
		t.Fatal(err)
	}
	w := 45
	if err := st.PutHealth(store.Health{Date: "2026-10-28", WorkoutMin: &w}); err != nil {
		t.Fatal(err)
	}

	var list struct {
		Activities []activityView `json:"activities"`
	}
	mustJSON(t, h, "GET", "/v1/activities?range=week", ``, 200, &list)
	if len(list.Activities) != 4 || list.Activities[0].Key != "study" || list.Activities[2].Key != "sport" {
		t.Fatalf("the four goals in order: %+v", list.Activities)
	}
	study, career, sport, work := list.Activities[0], list.Activities[1], list.Activities[2], list.Activities[3]
	// 60 + max(60, 120 from the calendar) + 60: the lecture is not counted twice.
	if study.TotalMin != 240 || study.ActiveDays != 3 || study.Streak != 3 {
		t.Fatalf("study: %+v", study)
	}
	if study.GoalPercent != 50 { // 240 of 480 minutes a week
		t.Fatalf("goal percent: %d", study.GoalPercent)
	}
	if career.TotalMin != 0 || career.ActiveDays != 1 || career.GoalPercent != -1 {
		t.Fatalf("career counts a day by contributions: %+v", career)
	}
	if career.Streak != 1 { // the 27th is active; a quiet today does not break it
		t.Fatalf("career streak: %d", career.Streak)
	}
	if sport.TotalMin != 45 || sport.Streak != 1 {
		t.Fatalf("sport comes from the watch: %+v", sport)
	}
	if work.TotalMin != 0 || work.ActiveDays != 0 {
		t.Fatalf("work: %+v", work)
	}
	if len(study.Heat) != 12*7 || !study.Heat[len(study.Heat)-1].Future {
		t.Fatalf("heat: %d cells, the last is Sunday and still ahead", len(study.Heat))
	}
	mustJSON(t, h, "GET", "/v1/activities?range=decade", ``, 400, nil)
}

func TestUserAppCategoryWins(t *testing.T) {
	clock := at(28, 12, 0)
	h, st := clockServer(t, nil, &clock)
	if err := st.PutAppCategory(store.AppCategory{Key: "com.notes", Category: "unknown", Origin: "llm", Confidence: 0.4, At: "x"}); err != nil {
		t.Fatal(err)
	}
	mustJSON(t, h, "PUT", "/v1/apps/category", `{"name":"com.Notes","category":"study"}`, 200, nil)
	got, _ := st.AppCategories([]string{"com.notes"})
	if c := got["com.notes"]; c.Category != "study" || c.Origin != "user" || c.Confidence != 1 {
		t.Fatalf("%+v", c)
	}
	mustJSON(t, h, "PUT", "/v1/apps/category", `{"name":"com.notes","category":"sleep"}`, 400, nil)
	mustJSON(t, h, "PUT", "/v1/apps/category", `{"name":"http://x.y/?q=1","category":"study"}`, 400, nil)
}

func TestRiskHistoryHasADayPerDay(t *testing.T) {
	clock := at(28, 12, 0)
	h, _ := clockServer(t, nil, &clock)
	var res struct {
		Days []riskDay `json:"days"`
	}
	mustJSON(t, h, "GET", "/v1/risks/history?days=7", ``, 200, &res)
	if len(res.Days) != 7 || res.Days[6].Date != "2026-10-28" || res.Days[0].Date != "2026-10-22" {
		t.Fatalf("%+v", res.Days)
	}
	for _, d := range res.Days {
		if d.Worst == "" || d.Sleep == "" {
			t.Fatalf("empty colour: %+v", d)
		}
	}
	mustJSON(t, h, "GET", "/v1/risks/history?days=99", ``, 400, nil)
}
