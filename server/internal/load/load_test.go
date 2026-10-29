package load

import (
	"strings"
	"testing"
	"time"
)

var ekb = mustLoc("Asia/Yekaterinburg")

func mustLoc(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}

// Week of Monday 2026-10-26; "now" is Tuesday noon.
func week(loc *time.Location) (from, to, now time.Time) {
	return time.Date(2026, 10, 26, 0, 0, 0, 0, loc), time.Date(2026, 11, 2, 0, 0, 0, 0, loc),
		time.Date(2026, 10, 27, 12, 0, 0, 0, loc)
}

// at builds a time on day d (26 = Monday) of the test week.
func at(loc *time.Location, d, h, m int) time.Time {
	return time.Date(2026, 10, d, h, m, 0, 0, loc)
}

func ev(loc *time.Location, c Category, d1, h1, m1, d2, h2, m2 int) Event {
	return Event{Start: at(loc, d1, h1, m1), End: at(loc, d2, h2, m2), Category: c}
}

func input(events ...Event) Input {
	from, to, now := week(ekb)
	return Input{Events: events, From: from, To: to, Loc: ekb, Now: now, T: DefaultThresholds()}
}

func f(v float64) *float64 { return &v }

func TestOverload(t *testing.T) {
	// n study days of h hours each starting 09:00, Monday on.
	days := func(n, hours int) []Event {
		var out []Event
		for d := 26; d < 26+n; d++ {
			out = append(out, ev(ekb, Study, d, 9, 0, d, 9+hours, 0))
		}
		return out
	}
	tests := []struct {
		name   string
		events []Event
		want   Color
		metric string
		value  float64
	}{
		{"no events at all", nil, Gray, "", 0},
		{"only all-day events", []Event{{Start: at(ekb, 26, 0, 0), End: at(ekb, 27, 0, 0), AllDay: true, Category: Study}}, Gray, "", 0},
		{"events but no study is green", []Event{ev(ekb, Hobby, 26, 18, 0, 26, 20, 0)}, Green, "week_hours", 0},
		{"day at yellow limit stays green", days(1, 3), Green, "max_day_hours", 3},
		{"streak above 3h is yellow", days(1, 4), Yellow, "max_streak_hours", 4},
		{"streak above 5h is red", days(1, 6), Red, "max_streak_hours", 6},
		{"day above 6h with breaks is yellow", []Event{
			ev(ekb, Study, 26, 8, 0, 26, 10, 0), ev(ekb, Study, 26, 11, 0, 26, 13, 0),
			ev(ekb, Study, 26, 14, 0, 26, 16, 0), ev(ekb, Study, 26, 17, 0, 26, 18, 0)}, Yellow, "max_day_hours", 7},
		{"day above 9h is red", []Event{
			ev(ekb, Study, 26, 7, 0, 26, 10, 0), ev(ekb, Study, 26, 11, 0, 26, 14, 0),
			ev(ekb, Study, 26, 15, 0, 26, 18, 0), ev(ekb, Study, 26, 19, 0, 26, 20, 0)}, Red, "max_day_hours", 10},
		{"short pause does not break streak", []Event{
			ev(ekb, Study, 26, 9, 0, 26, 10, 30), ev(ekb, Study, 26, 10, 40, 26, 12, 10)}, Yellow, "max_streak_hours", 3.17},
		{"15 min pause breaks streak", []Event{
			ev(ekb, Study, 26, 9, 0, 26, 11, 0), ev(ekb, Study, 26, 11, 15, 26, 13, 15)}, Green, "max_streak_hours", 2},
		{"duplicated event counted once", []Event{
			ev(ekb, Study, 26, 9, 0, 26, 11, 0), ev(ekb, Study, 26, 9, 0, 26, 11, 0)}, Green, "week_hours", 2},
		{"overlapping events merge", []Event{
			ev(ekb, Study, 26, 9, 0, 26, 11, 0), ev(ekb, Study, 26, 10, 0, 26, 12, 0)}, Green, "week_hours", 3},
		{"other categories ignored", []Event{ev(ekb, Work, 26, 8, 0, 26, 20, 0)}, Green, "week_hours", 0},
		{"event clipped at week end", []Event{ev(ekb, Study, 32, 22, 0, 33, 4, 0)}, Green, "week_hours", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Assess(input(tt.events...)).Overload
			if r.Color != tt.want {
				t.Fatalf("color %s, want %s; reasons %v metrics %v", r.Color, tt.want, r.Reasons, r.Metrics)
			}
			if tt.metric != "" && r.Metrics[tt.metric] != tt.value {
				t.Errorf("%s = %v, want %v", tt.metric, r.Metrics[tt.metric], tt.value)
			}
			if tt.want == Gray && len(r.Metrics) != 0 {
				t.Errorf("gray with metrics %v", r.Metrics)
			}
		})
	}
}

func TestOverloadWeekThresholds(t *testing.T) {
	// 7h study every day: week = 49h (yellow), 8h * 7 = 56h (red); day limits raised to isolate the week rule.
	for _, tt := range []struct {
		hours int
		want  Color
	}{{5, Green}, {6, Yellow}, {8, Red}} {
		in := input()
		in.T.StudyDayYellowAbove, in.T.StudyDayRedAbove = 24, 24
		in.T.StudyStreakYellowAbove, in.T.StudyStreakRedAbove = 24, 24
		for d := 26; d < 33; d++ {
			in.Events = append(in.Events, ev(ekb, Study, d, 8, 0, d, 8+tt.hours, 0))
		}
		in.T.StudyWeekYellowAbove, in.T.StudyWeekRedAbove = 35, 50
		if got := Assess(in).Overload.Color; got != tt.want {
			t.Errorf("%dh x7: %s, want %s", tt.hours, got, tt.want)
		}
	}
}

func TestOverloadReasonNamesTheDay(t *testing.T) {
	r := Assess(input(ev(ekb, Study, 28, 7, 0, 28, 14, 0))).Overload
	if len(r.Reasons) == 0 || !strings.Contains(strings.Join(r.Reasons, "|"), "ср 28.10") {
		t.Fatalf("reasons %v", r.Reasons)
	}
}

func TestOverloadAcrossMidnight(t *testing.T) {
	// 22:00 Mon to 04:00 Tue: 2h on Monday, 4h on Tuesday, 6h in a row.
	r := Assess(input(ev(ekb, Study, 26, 22, 0, 27, 4, 0))).Overload
	if r.Metrics["max_day_hours"] != 4 || r.Metrics["max_streak_hours"] != 6 || r.Color != Red {
		t.Fatalf("%+v", r)
	}
}

func TestOverloadDST(t *testing.T) {
	// Berlin switched to summer time on 2026-03-29 at 02:00. Sat 22:00 to Sun 04:00 lasts 5 real hours.
	berlin := mustLoc("Europe/Berlin")
	from := time.Date(2026, 3, 23, 0, 0, 0, 0, berlin)
	to := time.Date(2026, 3, 30, 0, 0, 0, 0, berlin)
	in := Input{
		From: from, To: to, Loc: berlin, Now: time.Date(2026, 3, 25, 12, 0, 0, 0, berlin), T: DefaultThresholds(),
		Events: []Event{{
			Start: time.Date(2026, 3, 28, 22, 0, 0, 0, berlin), End: time.Date(2026, 3, 29, 4, 0, 0, 0, berlin), Category: Study,
		}},
	}
	r := Assess(in).Overload
	if r.Metrics["week_hours"] != 5 || r.Metrics["max_day_hours"] != 3 {
		t.Fatalf("metrics %v", r.Metrics)
	}
}

func TestNoRest(t *testing.T) {
	hobby := func(d, h1, h2 int) Event { return ev(ekb, Hobby, d, h1, 0, d, h2, 0) }
	// Daytime filler so the week is not empty and evenings stay free.
	filler := ev(ekb, Study, 26, 9, 0, 26, 10, 0)
	tests := []struct {
		name    string
		events  []Event
		tracked bool
		want    Color
		rest    float64 // expected rest_hours, -1: metric must be absent
		evening float64
	}{
		{"no events", nil, true, Gray, -1, -1},
		{"rest not tracked, evenings free: green without hours", []Event{filler}, false, Green, -1, 7},
		{"tracked, no rest hours: red", []Event{filler}, true, Red, 0, 7},
		{"tracked, 3h: red", []Event{filler, hobby(26, 10, 13)}, true, Red, 3, 7},
		{"4h is yellow", []Event{filler, hobby(26, 10, 14)}, true, Yellow, 4, 7},
		{"7h is yellow", []Event{filler, hobby(26, 10, 17)}, true, Yellow, 7, 7},
		{"8h is green", []Event{filler, hobby(26, 10, 18)}, true, Green, 8, 7},
		{"rest events make hours known without mapping", []Event{filler, ev(ekb, Rest, 26, 10, 0, 26, 19, 0)}, false, Green, 9, 6},
		{"overlapping hobby and rest counted once", []Event{filler,
			ev(ekb, Hobby, 26, 10, 0, 26, 14, 0), ev(ekb, Rest, 26, 12, 0, 26, 18, 0)}, true, Green, 8, 7},
		{"event ending at 18:00 keeps evening free", []Event{ev(ekb, Study, 26, 16, 0, 26, 18, 0)}, false, Green, -1, 7},
		{"event past 18:00 takes the evening", []Event{ev(ekb, Study, 26, 17, 0, 26, 18, 1)}, false, Green, -1, 6},
		{"any category takes an evening, two left is enough", []Event{
			ev(ekb, Other, 26, 19, 0, 26, 20, 0), ev(ekb, Work, 27, 19, 0, 27, 20, 0),
			ev(ekb, Chores, 28, 19, 0, 28, 20, 0), ev(ekb, Study, 29, 19, 0, 29, 20, 0),
			ev(ekb, Study, 30, 19, 0, 30, 20, 0)}, false, Green, -1, 2},
		{"one free evening is yellow", []Event{
			ev(ekb, Other, 26, 19, 0, 26, 20, 0), ev(ekb, Other, 27, 19, 0, 27, 20, 0),
			ev(ekb, Other, 28, 19, 0, 28, 20, 0), ev(ekb, Other, 29, 19, 0, 29, 20, 0),
			ev(ekb, Other, 30, 19, 0, 30, 20, 0), ev(ekb, Other, 31, 19, 0, 31, 20, 0)}, false, Yellow, -1, 1},
		{"overnight event takes the evening of its start day", []Event{ev(ekb, Other, 26, 23, 0, 27, 1, 0)}, false, Green, -1, 6},
		{"all-day event does not take an evening", []Event{filler,
			{Start: at(ekb, 26, 0, 0), End: at(ekb, 27, 0, 0), AllDay: true}}, false, Green, -1, 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := input(tt.events...)
			in.RestTracked = tt.tracked
			r := Assess(in).NoRest
			if r.Color != tt.want {
				t.Fatalf("color %s, want %s; reasons %v metrics %v", r.Color, tt.want, r.Reasons, r.Metrics)
			}
			got, ok := r.Metrics["rest_hours"]
			if tt.rest < 0 && ok || tt.rest >= 0 && got != tt.rest {
				t.Errorf("rest_hours = %v (present %v), want %v", got, ok, tt.rest)
			}
			if tt.evening >= 0 && r.Metrics["free_evenings"] != tt.evening {
				t.Errorf("free_evenings = %v, want %v", r.Metrics["free_evenings"], tt.evening)
			}
		})
	}
}

func TestMeasuredSleep(t *testing.T) {
	ci := func(date string, sleep *float64) Checkin {
		return Checkin{Date: date, Energy: 3, Mood: 3, SleepHours: sleep}
	}
	tests := []struct {
		name  string
		cs    []Checkin
		sleep []SleepDay
		want  Color
		avg   float64
	}{
		{"measured sleep alone", nil, []SleepDay{{"2026-10-27", 5}}, Red, 5},
		{"measured wins over the check-in of the same day", []Checkin{ci("2026-10-27", f(8))}, []SleepDay{{"2026-10-27", 5}}, Red, 5},
		{"check-in fills the day without a measurement", []Checkin{ci("2026-10-26", f(7))}, []SleepDay{{"2026-10-27", 6}}, Green, 6.5},
		{"old measurement ignored", nil, []SleepDay{{"2026-10-20", 3}}, Gray, 0},
		{"future measurement ignored", nil, []SleepDay{{"2026-10-28", 3}}, Gray, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := input()
			in.Checkins, in.Sleep = tt.cs, tt.sleep
			r := Assess(in).Sleep
			if r.Color != tt.want {
				t.Fatalf("color %s, want %s; %v", r.Color, tt.want, r.Reasons)
			}
			if tt.want != Gray && r.Metrics["avg_sleep_hours"] != tt.avg {
				t.Errorf("avg %v, want %v", r.Metrics["avg_sleep_hours"], tt.avg)
			}
		})
	}
	in := input()
	in.Sleep = []SleepDay{{"2026-10-27", 7}}
	if st := Assess(in).State; st.Color != Gray {
		t.Errorf("measured sleep must not invent a state: %s", st.Color)
	}
}

func TestSleep(t *testing.T) {
	ci := func(date string, sleep *float64) Checkin {
		return Checkin{Date: date, Energy: 3, Mood: 3, SleepHours: sleep}
	}
	tests := []struct {
		name string
		cs   []Checkin
		want Color
		avg  float64
	}{
		{"no check-ins", nil, Gray, 0},
		{"check-in without sleep", []Checkin{ci("2026-10-27", nil)}, Gray, 0},
		{"7h green", []Checkin{ci("2026-10-27", f(7))}, Green, 7},
		{"6.5h green", []Checkin{ci("2026-10-27", f(6.5))}, Green, 6.5},
		{"6.4h yellow", []Checkin{ci("2026-10-27", f(6.4))}, Yellow, 6.4},
		{"5.5h yellow", []Checkin{ci("2026-10-27", f(5.5))}, Yellow, 5.5},
		{"5.4h red", []Checkin{ci("2026-10-27", f(5.4))}, Red, 5.4},
		{"average over days", []Checkin{ci("2026-10-25", f(5)), ci("2026-10-26", f(7)), ci("2026-10-27", f(6.6))}, Yellow, 6.2},
		{"float noise does not tip 6.5", []Checkin{ci("2026-10-26", f(6.4)), ci("2026-10-27", f(6.6))}, Green, 6.5},
		{"days without sleep are skipped", []Checkin{ci("2026-10-26", nil), ci("2026-10-27", f(5))}, Red, 5},
		{"older than 3 days ignored", []Checkin{ci("2026-10-24", f(3))}, Gray, 0},
		{"future ignored", []Checkin{ci("2026-10-28", f(3))}, Gray, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := input()
			in.Checkins = tt.cs
			r := Assess(in).Sleep
			if r.Color != tt.want {
				t.Fatalf("color %s, want %s; %v", r.Color, tt.want, r.Reasons)
			}
			if tt.want != Gray && r.Metrics["avg_sleep_hours"] != tt.avg {
				t.Errorf("avg %v, want %v", r.Metrics["avg_sleep_hours"], tt.avg)
			}
			if tt.want == Gray && len(r.Reasons) == 0 {
				t.Error("gray must say why")
			}
		})
	}
}

func TestState(t *testing.T) {
	ci := func(date string, energy, mood int) Checkin { return Checkin{Date: date, Energy: energy, Mood: mood} }
	tests := []struct {
		name string
		cs   []Checkin
		want Color
		avg  float64
	}{
		{"no check-ins", nil, Gray, 0},
		{"4 and 4 green", []Checkin{ci("2026-10-27", 4, 4)}, Green, 4},
		{"3.5 green", []Checkin{ci("2026-10-27", 3, 4)}, Green, 3.5},
		{"3 yellow", []Checkin{ci("2026-10-27", 3, 3)}, Yellow, 3},
		{"2.5 yellow", []Checkin{ci("2026-10-27", 2, 3)}, Yellow, 2.5},
		{"2 red", []Checkin{ci("2026-10-27", 2, 2)}, Red, 2},
		{"1 red", []Checkin{ci("2026-10-27", 1, 1)}, Red, 1},
		{"mean over 3 days", []Checkin{ci("2026-10-25", 1, 1), ci("2026-10-26", 5, 5), ci("2026-10-27", 3, 3)}, Yellow, 3},
		{"day 4 back ignored", []Checkin{ci("2026-10-24", 1, 1), ci("2026-10-27", 5, 5)}, Green, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := input()
			in.Checkins = tt.cs
			r := Assess(in).State
			if r.Color != tt.want {
				t.Fatalf("color %s, want %s; %v", r.Color, tt.want, r.Reasons)
			}
			if tt.want != Gray && r.Metrics["avg_score"] != tt.avg {
				t.Errorf("avg %v, want %v", r.Metrics["avg_score"], tt.avg)
			}
		})
	}
}

func TestStateWindowUsesUserTimeZone(t *testing.T) {
	// 2026-10-27 22:00 UTC is already 03:00 on the 28th in Yekaterinburg.
	in := input()
	in.Now = time.Date(2026, 10, 27, 22, 0, 0, 0, time.UTC)
	in.Checkins = []Checkin{{Date: "2026-10-25", Energy: 1, Mood: 1}}
	if got := Assess(in).State.Color; got != Gray {
		t.Fatalf("10-25 is 3 days back in the user's zone, want gray, got %s", got)
	}
}

func TestEmptyRisksSerializeAsNonNil(t *testing.T) {
	a := Assess(input())
	for name, r := range map[string]Risk{"sleep": a.Sleep, "overload": a.Overload, "no_rest": a.NoRest, "state": a.State} {
		if r.Reasons == nil || r.Metrics == nil {
			t.Errorf("%s: nil reasons or metrics", name)
		}
	}
}

func TestParseThresholds(t *testing.T) {
	got, err := ParseThresholds([]byte(`{"study_day_red_above": 10}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.StudyDayRedAbove != 10 || got.StudyDayYellowAbove != 6 {
		t.Errorf("partial override: %+v", got)
	}
	for _, bad := range []string{
		`{"nope": 1}`, `{"study_day_red_above": "x"}`, `{"study_day_yellow_above": 9, "study_day_red_above": 6}`,
		`{"sleep_red_below": 7}`, `{"state_days": 0}`, `{"evening_start_hour": 24}`, `[]`,
	} {
		if _, err := ParseThresholds([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestCalendarCategories(t *testing.T) {
	m, err := ParseCalendarCategories([]byte(`{"КВН":"hobby","Расписание":"other"}`))
	if err != nil {
		t.Fatal(err)
	}
	if m.Of("google", "КВН") != Hobby || m.Of("google", "Личный") != Other || m.Of("ics", "Пары") != Study ||
		m.Of("ics", "Расписание") != Other {
		t.Error("category resolution")
	}
	if !m.RestTracked() || (CalendarCategories{"a": Study}).RestTracked() {
		t.Error("RestTracked")
	}
	if _, err := ParseCalendarCategories([]byte(`{"a":"fun"}`)); err == nil {
		t.Error("accepted unknown category")
	}
}

func TestEventCategories(t *testing.T) {
	c := EventCategories{
		Calendars: CalendarCategories{"Работа": Work},
		Memory:    map[string]Category{"тренировка": Hobby},
	}
	tests := []struct {
		source, calendar, title string
		want                    Category
	}{
		{"google", "Личный", "Тренировка в 19:00", Hobby}, // the user's choice, by title
		{"google", "Работа", "Обед", Work},                // the mapped calendar
		{"google", "Личный", "Футбол", Sport},             // the words of the title
		{"ics", "Пары", "Физическая культура", Sport},
		{"ics", "Пары", "Матан", Study},
		{"ics", "Пары", "Непонятное", Study}, // the default of the class schedule
		{"google", "Личный", "Встреча", Other},
	}
	for _, tt := range tests {
		if got := c.Of(tt.source, tt.calendar, tt.title); got != tt.want {
			t.Errorf("Of(%s, %s, %q) = %s, want %s", tt.source, tt.calendar, tt.title, got, tt.want)
		}
	}
}
