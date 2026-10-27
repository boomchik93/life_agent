package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lifeagent/internal/calendar"
	"lifeagent/internal/review"
	"lifeagent/internal/store"
)

// goalServer: "now" is Tuesday 27 Oct 2026, 12:00, as in the plan tests.
func goalServer(t *testing.T, events []calendar.Event, w *fakeWriter) (http.Handler, *store.Store) {
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
	}), st
}

func putAct(t *testing.T, st *store.Store, date, source, category string, minutes int) {
	t.Helper()
	rows, _ := st.Activity(date, date, source)
	rows = append(rows, store.ActivityRow{Date: date, Source: source, Category: category, Minutes: minutes, Confidence: 1, Extra: "{}"})
	if err := st.ReplaceActivity(date, source, rows); err != nil {
		t.Fatal(err)
	}
}

type goalsBody struct {
	Goals []struct {
		Goal          string `json:"goal"`
		Title         string `json:"title"`
		WeeklyNormMin *int   `json:"weekly_norm_min"`
		SuggestedMin  *int   `json:"suggested_min"`
	} `json:"goals"`
	DataDays int `json:"data_days"`
}

func getGoals(t *testing.T, h http.Handler) goalsBody {
	t.Helper()
	code, body := send(h, "GET", "/v1/goals", "")
	if code != 200 {
		t.Fatalf("goals: %d %s", code, body)
	}
	var g goalsBody
	json.Unmarshal([]byte(body), &g)
	return g
}

func TestGoalsStartWithoutNorms(t *testing.T) {
	h, _ := goalServer(t, nil, &fakeWriter{})
	g := getGoals(t, h)
	if len(g.Goals) != 4 || g.Goals[0].Goal != "study" || g.Goals[3].Goal != "work" || g.DataDays != 0 {
		t.Fatalf("%+v", g)
	}
	for _, x := range g.Goals {
		if x.WeeklyNormMin != nil || x.SuggestedMin != nil {
			t.Errorf("a norm out of nothing: %+v", x)
		}
	}
}

func TestNoSuggestionBeforeFourteenDays(t *testing.T) {
	h, st := goalServer(t, nil, &fakeWriter{})
	putAct(t, st, "2026-10-20", "android_usage", "study", 600) // 8 days ago
	if g := getGoals(t, h); g.Goals[0].SuggestedMin != nil || g.DataDays != 8 {
		t.Errorf("%+v", g)
	}
}

func TestSuggestionConfirmedOnceAndRecalculatedAfterFourWeeks(t *testing.T) {
	h, st := goalServer(t, nil, &fakeWriter{})
	// Three completed weeks of data and the current one.
	for _, w := range []struct {
		date    string
		minutes int
	}{{"2026-10-05", 600}, {"2026-10-12", 300}, {"2026-10-19", 900}} {
		putAct(t, st, w.date, "android_usage", "study", w.minutes)
	}
	putAct(t, st, "2026-10-27", "android_usage", "study", 50) // the running week is not a basis

	g := getGoals(t, h)
	if g.DataDays != 23 || g.Goals[0].SuggestedMin == nil || *g.Goals[0].SuggestedMin != 660 { // median 600 + 10%
		t.Fatalf("study suggestion: %+v", g.Goals[0])
	}
	if g.Goals[0].WeeklyNormMin != nil {
		t.Error("the norm is the user's to confirm, not the server's")
	}
	if g.Goals[1].SuggestedMin != nil {
		t.Errorf("career has no data: %+v", g.Goals[1])
	}
	// Asking again gives the same suggestion.
	if again := getGoals(t, h); *again.Goals[0].SuggestedMin != 660 {
		t.Errorf("unstable: %+v", again.Goals[0])
	}

	if code, _ := send(h, "PUT", "/v1/goals", `{"goal":"career","action":"confirm"}`); code != 409 {
		t.Errorf("confirming nothing: %d", code)
	}
	code, body := send(h, "PUT", "/v1/goals", `{"goal":"study","action":"confirm"}`)
	var after goalsBody
	json.Unmarshal([]byte(body), &after)
	if code != 200 || after.Goals[0].WeeklyNormMin == nil || *after.Goals[0].WeeklyNormMin != 660 || after.Goals[0].SuggestedMin != nil {
		t.Fatalf("confirm: %d %s", code, body)
	}
	// Right after, nothing is proposed again.
	if g := getGoals(t, h); g.Goals[0].SuggestedMin != nil {
		t.Errorf("a new suggestion at once: %+v", g.Goals[0])
	}

	// Four weeks later the confirmed norm is looked at again (more work done).
	st.DB.Exec(`UPDATE goals SET recalculated_at = ? WHERE goal = 'study'`, at(27, 12, 0).AddDate(0, 0, -29).UTC().Format(time.RFC3339))
	for _, d := range []string{"2026-10-05", "2026-10-12", "2026-10-19"} {
		putAct(t, st, d, "mac_activitywatch", "study", 1200)
	}
	g = getGoals(t, h)
	if g.Goals[0].SuggestedMin == nil || *g.Goals[0].SuggestedMin != 1980 || *g.Goals[0].WeeklyNormMin != 660 {
		t.Errorf("recalculation: %+v", g.Goals[0])
	}
	if code, _ := send(h, "PUT", "/v1/goals", `{"goal":"study","action":"keep"}`); code != 200 {
		t.Fatalf("keep: %d", code)
	}
	if g := getGoals(t, h); g.Goals[0].SuggestedMin != nil || *g.Goals[0].WeeklyNormMin != 660 {
		t.Errorf("keep: %+v", g.Goals[0])
	}
}

func TestSettingAndClearingNorms(t *testing.T) {
	h, _ := goalServer(t, nil, &fakeWriter{})
	if code, _ := send(h, "PUT", "/v1/goals", `{"goal":"sport","action":"set","norm_min":180}`); code != 200 {
		t.Fatal(code)
	}
	if g := getGoals(t, h); *g.Goals[2].WeeklyNormMin != 180 {
		t.Errorf("%+v", g.Goals[2])
	}
	send(h, "PUT", "/v1/goals", `{"goal":"sport","action":"clear"}`)
	if g := getGoals(t, h); g.Goals[2].WeeklyNormMin != nil {
		t.Errorf("%+v", g.Goals[2])
	}
	for name, body := range map[string]string{
		"unknown goal": `{"goal":"fun","action":"set","norm_min":60}`, "no minutes": `{"goal":"sport","action":"set"}`,
		"too few": `{"goal":"sport","action":"set","norm_min":1}`, "more than a week": `{"goal":"sport","action":"set","norm_min":20000}`,
		"unknown action": `{"goal":"sport","action":"boost"}`, "extra field": `{"goal":"sport","action":"clear","x":1}`, "not json": `[`,
	} {
		if code, _ := send(h, "PUT", "/v1/goals", body); code != 400 {
			t.Errorf("%s: %d", name, code)
		}
	}
	if code, _ := send(h, "PUT", "/v1/goals", `{"goal":"work","action":"keep"}`); code != 409 {
		t.Errorf("keeping nothing: %d", code)
	}
}

func TestWeeklyReviewIsDeterministicAndStored(t *testing.T) {
	h, st := goalServer(t, nil, &fakeWriter{})
	putAct(t, st, "2026-10-26", "mac_activitywatch", "study", 300)
	putAct(t, st, "2026-10-26", "android_usage", "unknown", 25)
	putAct(t, st, "2026-10-27", "steam", "leisure", 120)
	st.PutHealth(store.Health{Date: "2026-10-27", SleepHours: fp(7), WorkoutMin: ip(40)})
	send(h, "PUT", "/v1/goals", `{"goal":"study","action":"set","norm_min":600}`)
	send(h, "PUT", "/v1/goals", `{"goal":"sport","action":"set","norm_min":120}`)

	code, a := send(h, "GET", "/v1/review/week", "")
	_, b := send(h, "GET", "/v1/review/week?start=2026-10-30", "")
	if code != 200 || a != b {
		t.Fatalf("the same week, the same data, different answers:\n%s\n%s", a, b)
	}
	var res review.Result
	json.Unmarshal([]byte(a), &res)
	if res.WeekStart != "2026-10-26" || len(res.Goals) != 4 {
		t.Fatalf("%+v", res)
	}
	byGoal := map[string]review.GoalResult{}
	for _, g := range res.Goals {
		byGoal[g.Goal] = g
	}
	if s := byGoal["study"]; s.FactMin != 300 || *s.Percent != 50 {
		t.Errorf("study: %+v", s)
	}
	if s := byGoal["sport"]; s.FactMin != 40 || *s.Percent != 33 {
		t.Errorf("sport from the watch: %+v", s)
	}
	if byGoal["career"].NormMin != nil || byGoal["work"].Percent != nil {
		t.Error("goals without a norm show only the fact")
	}
	if res.LeisureMin != 120 || res.SleepMin != 420 || res.UnknownMin != 25 {
		t.Errorf("%+v", res)
	}
	stored, ok, _ := st.Review("2026-10-26")
	if !ok || stored != strings.TrimSpace(a) {
		t.Errorf("the review is kept as history: %v %q", ok, stored)
	}
	if code, _ := send(h, "GET", "/v1/review/week?start=nope", ""); code != 400 {
		t.Errorf("bad start: %d", code)
	}
}

func fp(v float64) *float64 { return &v }
func ip(v int) *int         { return &v }

// weatherRows: Wednesday 28 is dry and light, Thursday 29 rains.
func weatherRows(t *testing.T, st *store.Store) {
	t.Helper()
	for date, extra := range map[string]string{
		"2026-10-28": `{"precip_mm":0.2,"sunrise_h":7.9,"sunset_h":17.3,"temp_c":4}`,
		"2026-10-29": `{"precip_mm":8,"sunrise_h":7.9,"sunset_h":17.3,"temp_c":3}`,
	} {
		if err := st.ReplaceActivity(date, "weather", []store.ActivityRow{{Date: date, Source: "weather", Category: "unknown", Confidence: 1, Extra: extra}}); err != nil {
			t.Fatal(err)
		}
	}
}

func goalPlan(t *testing.T, h http.Handler) planResponse {
	t.Helper()
	rec := post(h, "/v1/plan/week", "", `{"goals":true}`)
	if rec.Code != 200 {
		t.Fatalf("plan: %d %s", rec.Code, rec.Body)
	}
	var p planResponse
	decodeJSON(t, rec, &p)
	return p
}

func TestGoalsPlanFillsTheMissingMinutes(t *testing.T) {
	w := &fakeWriter{}
	h, st := goalServer(t, nil, w)
	weatherRows(t, st)
	putAct(t, st, "2026-10-26", "mac_activitywatch", "study", 240)
	send(h, "PUT", "/v1/goals", `{"goal":"study","action":"set","norm_min":600}`)
	send(h, "PUT", "/v1/goals", `{"goal":"sport","action":"set","norm_min":120}`)

	p := goalPlan(t, h)
	var study, sport int
	for _, c := range p.Placed {
		switch c.Title {
		case "Цель: Учёба":
			study += int(c.End.Sub(c.Start) / time.Minute)
			if c.Category != "study" {
				t.Errorf("study block category %s", c.Category)
			}
		case "Цель: Спорт":
			sport += int(c.End.Sub(c.Start) / time.Minute)
			if c.Start.Day() != 28 || c.Start.Hour() < 8 || c.End.Hour() > 17 {
				t.Errorf("sport belongs to the dry light day, got %v-%v", c.Start, c.End)
			}
		}
	}
	if study != 360 || sport != 120 { // 600 - 240 done; the whole sport norm
		t.Errorf("study %d sport %d, want 360 and 120", study, sport)
	}
	if len(p.Violations) != 0 || len(p.SkippedGoals) != 0 {
		t.Errorf("%+v", p)
	}
	if len(w.calls) != 0 {
		t.Error("the preview wrote to the calendar")
	}
	// Without the flag the goals are left alone.
	if plain := buildPlan(t, h); len(plain.Placed) != 0 {
		t.Errorf("goals planned without being asked: %+v", plain.Placed)
	}
}

func TestGoalsApplyWritesOnceAndCountsAsPlanned(t *testing.T) {
	w := &fakeWriter{}
	h, st := goalServer(t, nil, w)
	send(h, "PUT", "/v1/goals", `{"goal":"work","action":"set","norm_min":240}`)
	p := goalPlan(t, h)
	if len(p.Placed) == 0 {
		t.Fatalf("nothing planned: %+v", p)
	}
	body := strings.Replace(applyBody(p), `{"placed"`, `{"goals":true,"placed"`, 1)
	rec := post(h, "/v1/plan/week/apply", "goal-apply-1", body)
	if rec.Code != 200 {
		t.Fatalf("apply: %d %s", rec.Code, rec.Body)
	}
	var res applyResponse
	decodeJSON(t, rec, &res)
	if res.Duplicate || len(w.calls) != len(p.Placed) {
		t.Fatalf("%+v calls %d", res, len(w.calls))
	}
	for _, c := range w.calls {
		if c.Title != "Цель: Работа" || c.Category != "work" {
			t.Errorf("event: %+v", c)
		}
	}
	for _, v := range res.Applied {
		if v.Title != "Цель: Работа" {
			t.Errorf("view: %+v", v)
		}
	}
	chunks, _ := st.ChunksByKey("goal-apply-1")
	if len(chunks) != len(p.Placed) || chunks[0].Goal != "work" || chunks[0].ItemID != 0 {
		t.Errorf("chunks: %+v", chunks)
	}

	// A double tap writes nothing more; the minutes now count as planned.
	rec = post(h, "/v1/plan/week/apply", "goal-apply-1", body)
	decodeJSON(t, rec, &res)
	if rec.Code != 200 || !res.Duplicate || len(w.calls) != len(p.Placed) {
		t.Fatalf("repeat: %d %+v", rec.Code, res)
	}
	if next := goalPlan(t, h); len(next.Placed) != 0 {
		t.Errorf("the same goal minutes were planned twice: %+v", next.Placed)
	}
	// Plan deletes with the data.
	send(h, "DELETE", "/v1/data", `{"confirm":"delete-all-data"}`)
	if left, _ := st.ChunksByKey("goal-apply-1"); len(left) != 0 {
		t.Error("goal chunks survived the delete")
	}
}

func TestGoalsAreNotPutOnTopOfARedRisk(t *testing.T) {
	// 11 hours of lessons on Monday make the overload risk red.
	heavy := []calendar.Event{{ID: "1", Source: "ics", Calendar: "Расписание", Title: "Пары", Start: at(26, 8, 0), End: at(26, 19, 0)}}
	h, _ := goalServer(t, heavy, &fakeWriter{})
	for _, g := range []string{"study", "career", "sport"} {
		send(h, "PUT", "/v1/goals", fmt.Sprintf(`{"goal":%q,"action":"set","norm_min":120}`, g))
	}
	p := goalPlan(t, h)
	skipped := map[string]string{}
	for _, s := range p.SkippedGoals {
		skipped[s.Goal] = s.Reason
	}
	if skipped["study"] == "" || skipped["career"] == "" || skipped["sport"] != "" {
		t.Fatalf("skipped: %+v", p.SkippedGoals)
	}
	if !strings.Contains(skipped["study"], "Перегруз красный") {
		t.Errorf("reason: %q", skipped["study"])
	}
	for _, c := range p.Placed {
		if c.Title != "Цель: Спорт" {
			t.Errorf("a block on top of the red overload: %+v", c)
		}
	}
	if len(p.Placed) == 0 {
		t.Error("sport still has room")
	}
}

func TestRedSleepStopsAllGoals(t *testing.T) {
	h, _ := goalServer(t, []calendar.Event{{ID: "1", Source: "google", Calendar: "c", Title: "x", Start: at(28, 10, 0), End: at(28, 11, 0)}}, &fakeWriter{})
	post(h, "/v1/checkin", "", `{"energy":4,"mood":4,"sleep_hours":4}`)
	send(h, "PUT", "/v1/goals", `{"goal":"sport","action":"set","norm_min":120}`)
	p := goalPlan(t, h)
	if len(p.Placed) != 0 || len(p.SkippedGoals) != 1 || !strings.Contains(p.SkippedGoals[0].Reason, "Недосып") {
		t.Errorf("%+v", p)
	}
}
