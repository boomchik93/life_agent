package review

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

var monday = time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)

func ip(v int) *int         { return &v }
func fp(v float64) *float64 { return &v }

func week() Input {
	return Input{
		WeekStart: monday,
		Activity: []Day{
			{Date: "2026-10-26", Source: "android_usage", Category: "study", Minutes: 120, NightMinutes: 0},
			{Date: "2026-10-26", Source: "mac_activitywatch", Category: "study", Minutes: 180},
			{Date: "2026-10-27", Source: "mac_activitywatch", Category: "career", Minutes: 90},
			{Date: "2026-10-27", Source: "android_usage", Category: "leisure", Minutes: 200, NightMinutes: 35},
			{Date: "2026-10-27", Source: "steam", Category: "leisure", Minutes: 100, Count: 2},
			{Date: "2026-10-28", Source: "android_usage", Category: "social", Minutes: 60},
			{Date: "2026-10-28", Source: "android_usage", Category: "unknown", Minutes: 50},
			{Date: "2026-10-28", Source: "github", Category: "career", Count: 7},
			{Date: "2026-10-28", Source: "leetcode", Category: "career", Count: 2},
			{Date: "2026-10-28", Source: "weather", Category: "unknown", Minutes: 0},
			{Date: "2026-11-03", Source: "android_usage", Category: "study", Minutes: 999}, // next week
			{Date: "2026-10-20", Source: "android_usage", Category: "study", Minutes: 999}, // last week
		},
		Health: []Health{
			{Date: "2026-10-27", SleepHours: fp(7.5), WorkoutMin: ip(45)},
			{Date: "2026-10-28", SleepHours: fp(6), WorkoutMin: ip(30)},
		},
		Norms: map[string]*int{"study": ip(600), "career": ip(300), "sport": ip(180), "work": nil},
	}
}

func goal(r Result, g string) GoalResult {
	for _, x := range r.Goals {
		if x.Goal == g {
			return x
		}
	}
	return GoalResult{}
}

func TestFactsAgainstNorms(t *testing.T) {
	r := Compute(week())
	if r.WeekStart != "2026-10-26" || r.WeekEnd != "2026-11-01" {
		t.Errorf("week: %s..%s", r.WeekStart, r.WeekEnd)
	}
	if len(r.Goals) != 4 || r.Goals[0].Goal != "study" || r.Goals[3].Goal != "work" {
		t.Fatalf("all four goals, equal, in a fixed order: %+v", r.Goals)
	}
	study := goal(r, "study")
	if study.FactMin != 300 || *study.NormMin != 600 || *study.Percent != 50 {
		t.Errorf("study: %+v", study)
	}
	if c := goal(r, "career"); c.FactMin != 90 || *c.Percent != 30 {
		t.Errorf("career counts only minutes, not contributions: %+v", c)
	}
	if s := goal(r, "sport"); s.FactMin != 75 || *s.Percent != 42 { // the watch's workouts are sport
		t.Errorf("sport: %+v", s)
	}
	w := goal(r, "work")
	if w.FactMin != 0 || w.NormMin != nil || w.Percent != nil {
		t.Errorf("without a norm only the fact is shown: %+v", w)
	}
}

func TestTheRestOfTheWeek(t *testing.T) {
	r := Compute(week())
	if r.LeisureMin != 300 || r.SocialMin != 60 || r.SleepMin != 810 || r.SleepDays != 2 || r.NightMin != 35 {
		t.Errorf("rest: %+v", r)
	}
	if r.Contributions != 9 {
		t.Errorf("contributions: %d", r.Contributions)
	}
	// 50 unknown of 120+180+90+200+100+60+50 = 800 minutes.
	if r.UnknownMin != 50 || r.TotalMin != 800 || r.UnknownPercent != 6 {
		t.Errorf("unknown: %d of %d = %d%%", r.UnknownMin, r.TotalMin, r.UnknownPercent)
	}
	if r.DaysWithData != 3 || len(r.Warnings) != 0 {
		t.Errorf("days %d warnings %v", r.DaysWithData, r.Warnings)
	}
}

func TestGapsAndBalance(t *testing.T) {
	r := Compute(week())
	want := []Gap{{"study", "Учёба", 300}, {"career", "Карьера", 210}, {"sport", "Спорт", 105}}
	if !reflect.DeepEqual(r.Gaps, want) {
		t.Errorf("gaps: %+v", r.Gaps)
	}
	if r.Balance == nil || r.Balance.MinPercent != 30 || r.Balance.SpreadPercent != 20 {
		t.Errorf("balance: %+v", r.Balance)
	}

	// Four norms, four gaps: only the three biggest.
	in := week()
	in.Norms["work"] = ip(500)
	if g := Compute(in).Gaps; len(g) != 3 || g[0].Goal != "work" && g[0].MissingMin != 500 {
		t.Errorf("gaps: %+v", g)
	}
	// A goal that reached its norm has no gap.
	in = week()
	in.Norms = map[string]*int{"study": ip(100)}
	r = Compute(in)
	if len(r.Gaps) != 0 || r.Balance != nil {
		t.Errorf("gaps %+v balance %+v", r.Gaps, r.Balance)
	}
	// Equal gaps keep the order of the goals.
	in = week()
	in.Activity = nil
	in.Health = nil
	in.Norms = map[string]*int{"study": ip(60), "career": ip(60), "sport": ip(60), "work": ip(60)}
	g := Compute(in).Gaps
	if len(g) != 3 || g[0].Goal != "study" || g[1].Goal != "career" || g[2].Goal != "sport" {
		t.Errorf("tie order: %+v", g)
	}
}

func TestNoNormsNoGaps(t *testing.T) {
	in := week()
	in.Norms = nil
	r := Compute(in)
	if len(r.Gaps) != 0 || r.Balance != nil {
		t.Errorf("%+v", r)
	}
	for _, g := range r.Goals {
		if g.NormMin != nil || g.Percent != nil {
			t.Errorf("goal %s", g.Goal)
		}
	}
	if !strings.Contains(strings.Join(r.Text, "\n"), "Учёба: 5 ч (норма не задана).") {
		t.Errorf("text: %v", r.Text)
	}
}

func TestUnknownWarning(t *testing.T) {
	in := week()
	in.Activity = []Day{
		{Date: "2026-10-26", Source: "android_usage", Category: "study", Minutes: 60},
		{Date: "2026-10-26", Source: "android_usage", Category: "unknown", Minutes: 40},
	}
	r := Compute(in)
	if r.UnknownPercent != 40 || len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "40%") {
		t.Errorf("%d %v", r.UnknownPercent, r.Warnings)
	}
	in.Activity[1].Minutes = 15 // 20% exactly is allowed
	in.Activity[0].Minutes = 60
	in.Activity = append(in.Activity, Day{Date: "2026-10-26", Source: "android_usage", Category: "leisure", Minutes: 5})
	if r := Compute(in); len(r.Warnings) != 0 {
		t.Errorf("%d%% %v", r.UnknownPercent, r.Warnings)
	}
}

func TestAWeekWithoutDataSaysSo(t *testing.T) {
	r := Compute(Input{WeekStart: monday})
	if r.DaysWithData != 0 || len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "нет данных") {
		t.Errorf("%+v", r)
	}
	if len(r.Goals) != 4 || len(r.Text) == 0 {
		t.Errorf("the four goals are shown even with no data: %+v", r)
	}
}

// The same data always gives the same review.
func TestDeterministic(t *testing.T) {
	in := week()
	a := Compute(in)
	// Shuffle the input rows.
	for i, j := 0, len(in.Activity)-1; i < j; i, j = i+1, j-1 {
		in.Activity[i], in.Activity[j] = in.Activity[j], in.Activity[i]
	}
	in.Health[0], in.Health[1] = in.Health[1], in.Health[0]
	b := Compute(in)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("different reviews for the same data:\n%+v\n%+v", a, b)
	}
}

func TestSuggestIsTheMedianPlusTenPercent(t *testing.T) {
	for _, tc := range []struct {
		weeks []int
		want  int
	}{
		{nil, 0}, {[]int{0, 0}, 0},
		{[]int{600}, 660},
		{[]int{600, 300, 900}, 660},       // median 600
		{[]int{600, 800}, 770},            // median 700 * 1.1
		{[]int{0, 600, 0, 900, 300}, 660}, // empty weeks are not data
		{[]int{1}, 5},
		{[]int{333}, 365},
	} {
		if got := Suggest(tc.weeks); got != tc.want {
			t.Errorf("%v -> %d, want %d", tc.weeks, got, tc.want)
		}
	}
}

func TestDuration(t *testing.T) {
	for in, want := range map[int]string{0: "0 мин", 45: "45 мин", 60: "1 ч", 150: "2 ч 30 мин"} {
		if got := Duration(in); got != want {
			t.Errorf("%d -> %q", in, got)
		}
	}
}
