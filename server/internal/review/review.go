// Package review turns a week of daily sums into the weekly review of
// PLAN.md stage 10.6: for four equal goals the fact against the norm, then
// leisure, social, sleep, the share of "unknown", the night minutes and the
// three biggest gaps. It is pure code: the same data always gives the same
// review, and the text comes from templates, not from a model.
package review

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Goals are the four equal goals, in the order they are shown. None is ranked.
var Goals = []string{"study", "career", "sport", "work"}

var goalTitles = map[string]string{"study": "Учёба", "career": "Карьера", "sport": "Спорт", "work": "Работа"}

// Title is the Russian name of a goal.
func Title(goal string) string { return goalTitles[goal] }

// UnknownWarnPercent is the share of "unknown" above which the review warns
// (PLAN.md section 14).
const UnknownWarnPercent = 20

// Day is one daily sum of a source.
type Day struct {
	Date         string
	Source       string
	Category     string
	Minutes      int
	NightMinutes int
	Count        int
}

// Health is the health summary of a day.
type Health struct {
	Date       string
	SleepHours *float64
	WorkoutMin *int
}

type Input struct {
	WeekStart time.Time // local Monday 00:00
	Activity  []Day
	Health    []Health
	// Norms is the weekly norm in minutes per goal; a missing or nil entry means none.
	Norms map[string]*int
}

type GoalResult struct {
	Goal    string `json:"goal"`
	Title   string `json:"title"`
	FactMin int    `json:"fact_min"`
	NormMin *int   `json:"norm_min"`
	Percent *int   `json:"percent"`
}

type Gap struct {
	Goal       string `json:"goal"`
	Title      string `json:"title"`
	MissingMin int    `json:"missing_min"`
}

// Balance is the lowest percent among the goals with a norm and how far the
// highest is above it.
type Balance struct {
	MinPercent    int `json:"min_percent"`
	SpreadPercent int `json:"spread_percent"`
}

type Result struct {
	WeekStart string       `json:"week_start"`
	WeekEnd   string       `json:"week_end"` // the Sunday
	Goals     []GoalResult `json:"goals"`
	// LeisureMin, SocialMin: time on the screens. SleepMin is measured sleep
	// only (Health Connect); SleepDays how many nights it covers.
	LeisureMin int `json:"leisure_min"`
	SocialMin  int `json:"social_min"`
	SleepMin   int `json:"sleep_min"`
	SleepDays  int `json:"sleep_days"`
	// UnknownMin and its share of all the time the sources saw.
	UnknownMin     int `json:"unknown_min"`
	UnknownPercent int `json:"unknown_percent"`
	TotalMin       int `json:"total_min"`
	// NightMin: screen time inside the sleep window.
	NightMin int `json:"night_min"`
	// Contributions: GitHub contributions and LeetCode solutions of the week.
	Contributions int      `json:"contributions"`
	Gaps          []Gap    `json:"gaps"`
	Balance       *Balance `json:"balance"`
	DaysWithData  int      `json:"days_with_data"`
	Warnings      []string `json:"warnings"`
	Text          []string `json:"text"`
}

// Facts returns the minutes per goal of the activity and health given: screen
// time by category, and for sport also the workouts of the watch. Sources that
// report counts, not time, add nothing here.
func Facts(activity []Day, health []Health) map[string]int {
	out := map[string]int{}
	for _, d := range activity {
		for _, g := range Goals {
			if d.Category == g {
				out[g] += d.Minutes
			}
		}
	}
	for _, h := range health {
		if h.WorkoutMin != nil {
			out["sport"] += *h.WorkoutMin
		}
	}
	return out
}

// Compute builds the review of one week. Rows outside the week are ignored.
func Compute(in Input) Result {
	from := in.WeekStart.Format("2006-01-02")
	last := in.WeekStart.AddDate(0, 0, 6).Format("2006-01-02")
	within := func(d string) bool { return d >= from && d <= last }

	var act []Day
	var hlt []Health
	days := map[string]bool{}
	for _, d := range in.Activity {
		if within(d.Date) && d.Source != "weather" {
			act = append(act, d)
			if d.Minutes > 0 || d.Count > 0 {
				days[d.Date] = true
			}
		}
	}
	for _, h := range in.Health {
		if within(h.Date) {
			hlt = append(hlt, h)
		}
	}

	r := Result{WeekStart: from, WeekEnd: last, Goals: []GoalResult{}, Gaps: []Gap{}, Warnings: []string{}, Text: []string{}}
	facts := Facts(act, hlt)
	for _, d := range act {
		switch d.Category {
		case "leisure":
			r.LeisureMin += d.Minutes
		case "social":
			r.SocialMin += d.Minutes
		case "unknown":
			r.UnknownMin += d.Minutes
		}
		if d.Category != "sleep" { // sleep is what the watch measured, nothing else
			r.TotalMin += d.Minutes
		}
		r.NightMin += d.NightMinutes
		if d.Source == "github" || d.Source == "leetcode" {
			r.Contributions += d.Count
		}
	}
	for _, h := range hlt {
		if h.SleepHours != nil {
			r.SleepMin += int(math.Round(*h.SleepHours * 60))
			r.SleepDays++
			days[h.Date] = true
		}
	}
	r.DaysWithData = len(days)
	if r.TotalMin > 0 {
		r.UnknownPercent = int(math.Round(100 * float64(r.UnknownMin) / float64(r.TotalMin)))
	}

	var percents []int
	for _, g := range Goals {
		gr := GoalResult{Goal: g, Title: goalTitles[g], FactMin: facts[g]}
		if n := in.Norms[g]; n != nil && *n > 0 {
			norm := *n
			gr.NormMin = &norm
			p := int(math.Round(100 * float64(gr.FactMin) / float64(norm)))
			gr.Percent = &p
			percents = append(percents, p)
			if gr.FactMin < norm {
				r.Gaps = append(r.Gaps, Gap{Goal: g, Title: goalTitles[g], MissingMin: norm - gr.FactMin})
			}
		}
		r.Goals = append(r.Goals, gr)
	}
	// The biggest gaps first; the order of the goals breaks ties, so the
	// result does not depend on anything but the data.
	sort.SliceStable(r.Gaps, func(i, j int) bool { return r.Gaps[i].MissingMin > r.Gaps[j].MissingMin })
	if len(r.Gaps) > 3 {
		r.Gaps = r.Gaps[:3]
	}
	if len(percents) >= 2 {
		lo, hi := percents[0], percents[0]
		for _, p := range percents {
			lo, hi = min(lo, p), max(hi, p)
		}
		r.Balance = &Balance{MinPercent: lo, SpreadPercent: hi - lo}
	}

	if r.DaysWithData == 0 {
		r.Warnings = append(r.Warnings, "За эту неделю нет данных ни из одного источника.")
	}
	if r.TotalMin > 0 && r.UnknownPercent > UnknownWarnPercent {
		r.Warnings = append(r.Warnings, fmt.Sprintf("Неопознано %d%% времени: больше %d%%, обзор неполный.", r.UnknownPercent, UnknownWarnPercent))
	}
	r.Text = text(r)
	return r
}

// Duration writes minutes as "2 ч 30 мин".
func Duration(min int) string {
	h, m := min/60, min%60
	switch {
	case h == 0:
		return fmt.Sprintf("%d мин", m)
	case m == 0:
		return fmt.Sprintf("%d ч", h)
	}
	return fmt.Sprintf("%d ч %d мин", h, m)
}

func text(r Result) []string {
	var out []string
	for _, g := range r.Goals {
		if g.NormMin != nil {
			out = append(out, fmt.Sprintf("%s: %s из %s (%d%%).", g.Title, Duration(g.FactMin), Duration(*g.NormMin), *g.Percent))
		} else {
			out = append(out, fmt.Sprintf("%s: %s (норма не задана).", g.Title, Duration(g.FactMin)))
		}
	}
	if r.Balance != nil {
		out = append(out, fmt.Sprintf("Баланс целей: от %d%% до %d%%, разброс %d п. п.",
			r.Balance.MinPercent, r.Balance.MinPercent+r.Balance.SpreadPercent, r.Balance.SpreadPercent))
	}
	if len(r.Gaps) > 0 {
		parts := make([]string, len(r.Gaps))
		for i, g := range r.Gaps {
			parts[i] = fmt.Sprintf("%s — %s", g.Title, Duration(g.MissingMin))
		}
		out = append(out, "Больше всего не хватает: "+strings.Join(parts, "; ")+".")
	}
	out = append(out, fmt.Sprintf("Отдых и развлечения: %s. Общение: %s.", Duration(r.LeisureMin), Duration(r.SocialMin)))
	if r.SleepDays > 0 {
		out = append(out, fmt.Sprintf("Сон по данным часов: %s за %d дн., в среднем %s.",
			Duration(r.SleepMin), r.SleepDays, Duration(r.SleepMin/r.SleepDays)))
	} else {
		out = append(out, "Сон: данных часов нет.")
	}
	if r.NightMin > 0 {
		out = append(out, fmt.Sprintf("Ночью за экраном: %s.", Duration(r.NightMin)))
	}
	if r.Contributions > 0 {
		out = append(out, fmt.Sprintf("Вклад в GitHub и LeetCode: %d.", r.Contributions))
	}
	return out
}

// Suggest proposes a weekly norm: the median of the weekly minutes plus 10%,
// rounded to 5 minutes. It returns 0 when there is nothing to base it on.
func Suggest(weekly []int) int {
	var w []int
	for _, m := range weekly {
		if m > 0 {
			w = append(w, m)
		}
	}
	if len(w) == 0 {
		return 0
	}
	sort.Ints(w)
	var median float64
	if n := len(w); n%2 == 1 {
		median = float64(w[n/2])
	} else {
		median = float64(w[n/2-1]+w[n/2]) / 2
	}
	v := int(math.Round(median*1.1/5) * 5)
	return max(v, 5)
}
