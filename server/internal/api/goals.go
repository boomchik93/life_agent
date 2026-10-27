package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"lifeagent/internal/calendar"
	"lifeagent/internal/load"
	"lifeagent/internal/plan"
	"lifeagent/internal/review"
	"lifeagent/internal/store"
)

const (
	suggestAfterDays = 14 // days of data before the first suggestion
	recalcEveryDays  = 28 // a confirmed norm is looked at again every four weeks
	suggestWeeks     = 4  // completed weeks the suggestion is based on
	maxNormMin       = 7 * 24 * 60
	dryMaxPrecipMM   = 1.0
	goalEventPrefix  = "Цель: "
)

func reviewDays(rows []store.ActivityRow) []review.Day {
	out := make([]review.Day, len(rows))
	for i, r := range rows {
		out[i] = review.Day{Date: r.Date, Source: r.Source, Category: r.Category, Minutes: r.Minutes, NightMinutes: r.NightMinutes, Count: r.Count}
	}
	return out
}

func reviewHealth(rows []store.Health) []review.Health {
	out := make([]review.Health, len(rows))
	for i, h := range rows {
		out[i] = review.Health{Date: h.Date, SleepHours: h.SleepHours, WorkoutMin: h.WorkoutMin}
	}
	return out
}

func ymd(t time.Time) string { return t.Format("2006-01-02") }

// weekFacts reads the facts of the week starting at the given Monday.
func (s *Server) weekData(monday time.Time) ([]review.Day, []review.Health, error) {
	last := monday.AddDate(0, 0, 6)
	act, err := s.store.Activity(ymd(monday), ymd(last), "")
	if err != nil {
		return nil, nil, err
	}
	hl, err := s.store.Healths(ymd(monday), ymd(last))
	if err != nil {
		return nil, nil, err
	}
	return reviewDays(act), reviewHealth(hl), nil
}

func normsOf(goals []store.Goal) map[string]*int {
	out := map[string]*int{}
	for _, g := range goals {
		out[g.Goal] = g.WeeklyNormMin
	}
	return out
}

// getReviewWeek is the weekly review, calculated by code from the stored
// sums. ?start=YYYY-MM-DD selects the week containing that date.
func (s *Server) getReviewWeek(w http.ResponseWriter, r *http.Request) {
	day := s.now()
	if v := r.URL.Query().Get("start"); v != "" {
		t, err := time.ParseInLocation("2006-01-02", v, s.loc)
		if err != nil {
			writeError(w, http.StatusBadRequest, "start must be YYYY-MM-DD")
			return
		}
		day = t
	}
	monday, _ := calendar.WeekBounds(day, s.loc)
	goals, err := s.store.Goals()
	if err != nil {
		s.fail(w, err)
		return
	}
	act, hl, err := s.weekData(monday)
	if err != nil {
		s.fail(w, err)
		return
	}
	res := review.Compute(review.Input{WeekStart: monday, Activity: act, Health: hl, Norms: normsOf(goals)})
	if raw, err := json.Marshal(res); err == nil {
		_ = s.store.SaveReview(res.WeekStart, string(raw), s.nowRFC())
	}
	writeJSON(w, http.StatusOK, res)
}

type goalView struct {
	Goal           string  `json:"goal"`
	Title          string  `json:"title"`
	WeeklyNormMin  *int    `json:"weekly_norm_min"`
	SuggestedMin   *int    `json:"suggested_min"`
	RecalculatedAt *string `json:"recalculated_at"`
}

// getGoals lists the four goals. When the data is old enough, it makes the
// suggestion of a norm first (and keeps it until the user answers).
func (s *Server) getGoals(w http.ResponseWriter, r *http.Request) {
	if err := s.suggestNorms(); err != nil {
		s.fail(w, err)
		return
	}
	goals, err := s.store.Goals()
	if err != nil {
		s.fail(w, err)
		return
	}
	first, err := s.store.FirstActivityDate()
	if err != nil {
		s.fail(w, err)
		return
	}
	days := 0
	if first != "" {
		if t, err := time.ParseInLocation("2006-01-02", first, s.loc); err == nil {
			days = int(s.now().In(s.loc).Sub(t).Hours()/24) + 1
		}
	}
	out := make([]goalView, len(goals))
	for i, g := range goals {
		out[i] = goalView{Goal: g.Goal, Title: review.Title(g.Goal), WeeklyNormMin: g.WeeklyNormMin,
			SuggestedMin: g.SuggestedMin, RecalculatedAt: g.RecalculatedAt}
	}
	writeJSON(w, http.StatusOK, map[string]any{"goals": out, "data_days": days, "suggest_after_days": suggestAfterDays})
}

// suggestNorms makes the proposals of PLAN.md stage 10.6: after 14 days of
// data a goal without a norm gets one (the median of the completed weeks plus
// 10%); a confirmed norm is looked at again every 4 weeks. The user confirms.
func (s *Server) suggestNorms() error {
	first, err := s.store.FirstActivityDate()
	if err != nil || first == "" {
		return err
	}
	firstDay, err := time.ParseInLocation("2006-01-02", first, s.loc)
	if err != nil {
		return nil
	}
	now := s.now().In(s.loc)
	if now.Sub(firstDay) < suggestAfterDays*24*time.Hour {
		return nil
	}
	goals, err := s.store.Goals()
	if err != nil {
		return err
	}
	thisMonday, _ := calendar.WeekBounds(now, s.loc)
	weekly := map[string][]int{}
	for k := 1; k <= suggestWeeks; k++ {
		monday := thisMonday.AddDate(0, 0, -7*k)
		if monday.Before(time.Date(firstDay.Year(), firstDay.Month(), firstDay.Day()-6, 0, 0, 0, 0, s.loc)) {
			break // before the data began
		}
		act, hl, err := s.weekData(monday)
		if err != nil {
			return err
		}
		for g, m := range review.Facts(act, hl) {
			weekly[g] = append(weekly[g], m)
		}
	}
	at := s.nowRFC()
	for _, g := range goals {
		if g.SuggestedMin != nil {
			continue // waits for an answer
		}
		due := g.WeeklyNormMin == nil
		if !due && g.RecalculatedAt != nil {
			if t, err := time.Parse(time.RFC3339, *g.RecalculatedAt); err == nil {
				due = now.Sub(t) >= recalcEveryDays*24*time.Hour
			}
		}
		if !due {
			continue
		}
		v := review.Suggest(weekly[g.Goal])
		if v == 0 || (g.WeeklyNormMin != nil && v == *g.WeeklyNormMin) {
			continue
		}
		if err := s.store.SetGoalSuggestion(g.Goal, v, at); err != nil {
			return err
		}
	}
	return nil
}

// putGoals answers a suggestion or sets a norm by hand:
//
//	{"goal":"study","action":"confirm"}            take the suggested norm
//	{"goal":"study","action":"keep"}               keep the current norm
//	{"goal":"study","action":"set","norm_min":600} a norm of the user's own
//	{"goal":"study","action":"clear"}              back to "fact only"
func (s *Server) putGoals(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "body too large")
		return
	}
	var in struct {
		Goal    string `json:"goal"`
		Action  string `json:"action"`
		NormMin *int   `json:"norm_min"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, `body must be {"goal", "action", "norm_min"}`)
		return
	}
	valid := false
	for _, g := range store.GoalNames {
		valid = valid || g == in.Goal
	}
	if !valid {
		writeError(w, http.StatusBadRequest, "goal must be study, career, sport or work")
		return
	}
	goals, err := s.store.Goals()
	if err != nil {
		s.fail(w, err)
		return
	}
	var cur store.Goal
	for _, g := range goals {
		if g.Goal == in.Goal {
			cur = g
		}
	}
	at := s.nowRFC()
	switch in.Action {
	case "confirm":
		if cur.SuggestedMin == nil {
			writeCode(w, http.StatusConflict, "no_suggestion", "Для этой цели нет предложенной нормы.")
			return
		}
		err = s.store.SetGoalNorm(in.Goal, *cur.SuggestedMin, at)
	case "keep":
		if cur.WeeklyNormMin == nil {
			writeCode(w, http.StatusConflict, "no_norm", "У цели нет нормы, которую можно оставить.")
			return
		}
		err = s.store.KeepGoalNorm(in.Goal, at)
	case "set":
		if in.NormMin == nil || *in.NormMin < 5 || *in.NormMin > maxNormMin {
			writeError(w, http.StatusBadRequest, "norm_min must be 5..10080 minutes")
			return
		}
		err = s.store.SetGoalNorm(in.Goal, *in.NormMin, at)
	case "clear":
		err = s.store.ClearGoalNorm(in.Goal)
	default:
		writeError(w, http.StatusBadRequest, "action must be confirm, keep, set or clear")
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	s.getGoals(w, r)
}

// Goal blocks of a plan carry negative ids: they are not rows of the items table.
var goalIDs = map[string]int64{"study": -1, "career": -2, "sport": -3, "work": -4}

// goalMeta says what a goal block is: how it is titled in the calendar and
// which category the load rules count it under.
type goalMeta struct {
	Goal     string
	Title    string
	Category load.Category
}

func goalCategory(goal string) load.Category {
	switch goal {
	case "study":
		return load.Study
	case "sport":
		return load.Hobby
	}
	return load.Work // career and work
}

type skippedGoal struct {
	Goal   string `json:"goal"`
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

// goalTasks turns the missing minutes of the goals into tasks for the planner.
// A goal with no norm has nothing missing. A block is not put on top of a red
// risk of section 6: red overload takes no more study or work, red sleep takes
// nothing, a red state takes nothing but sport.
func (s *Server) goalTasks(pc *planContext) ([]plan.Task, error) {
	goals, err := s.store.Goals()
	if err != nil {
		return nil, err
	}
	from, to := pc.in.From, pc.in.To
	act, hl, err := s.weekData(from)
	if err != nil {
		return nil, err
	}
	facts := review.Facts(act, hl)
	planned, err := s.store.PlannedGoalMinutes(from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339), pc.exceptKey)
	if err != nil {
		return nil, err
	}
	risks, err := s.assess(pc.events, from, to)
	if err != nil {
		return nil, err
	}
	prefer, err := s.dryLight(from, to)
	if err != nil {
		return nil, err
	}

	pc.goals = map[int64]goalMeta{}
	deadline := to.AddDate(0, 0, -1) // Sunday: the last day of the week
	var out []plan.Task
	for _, g := range goals {
		if g.WeeklyNormMin == nil {
			continue
		}
		missing := *g.WeeklyNormMin - facts[g.Goal] - planned[g.Goal]
		if missing <= 0 {
			continue
		}
		cat := goalCategory(g.Goal)
		meta := goalMeta{Goal: g.Goal, Title: goalEventPrefix + review.Title(g.Goal), Category: cat}
		reason := ""
		switch {
		case risks.Sleep.Color == load.Red:
			reason = "Недосып красный: сейчас нужен отдых, а не цели."
		case risks.State.Color == load.Red && g.Goal != "sport":
			reason = "Состояние красное: кроме спорта, цели подождут."
		case risks.Overload.Color == load.Red && (cat == load.Study || cat == load.Work):
			reason = "Перегруз красный: учёбу и работу больше не добавляем."
		}
		if reason != "" {
			pc.skipped = append(pc.skipped, skippedGoal{Goal: g.Goal, Title: review.Title(g.Goal), Reason: reason})
			continue
		}
		pc.goals[goalIDs[g.Goal]] = meta
		task := plan.Task{ID: goalIDs[g.Goal], Title: meta.Title, Category: cat, RemainingMin: missing, Deadline: deadline}
		if g.Goal == "sport" {
			task.Prefer = prefer
		}
		out = append(out, task)
	}
	return out, nil
}

// dryLight returns the periods of the week that are light and dry, from the
// weather forecast: between sunrise and sunset, with little rain.
func (s *Server) dryLight(from, to time.Time) ([]plan.Window, error) {
	rows, err := s.store.Activity(ymd(from), ymd(to.AddDate(0, 0, -1)), "weather")
	if err != nil {
		return nil, err
	}
	var out []plan.Window
	for _, r := range rows {
		var extra map[string]float64
		if json.Unmarshal([]byte(r.Extra), &extra) != nil {
			continue
		}
		rise, okR := extra["sunrise_h"]
		set, okS := extra["sunset_h"]
		precip, okP := extra["precip_mm"]
		if !okR || !okS || (okP && precip >= dryMaxPrecipMM) {
			continue
		}
		day, err := time.ParseInLocation("2006-01-02", r.Date, s.loc)
		if err != nil {
			continue
		}
		at := func(h float64) time.Time {
			return time.Date(day.Year(), day.Month(), day.Day(), int(h), int((h-float64(int(h)))*60), 0, 0, s.loc)
		}
		out = append(out, plan.Window{Start: at(rise), End: at(set)})
	}
	return out, nil
}

// info says what a chunk's task is called and which category it has, for a
// real task and for a goal block alike.
func (pc planContext) info(id int64) (title string, category string) {
	if m, ok := pc.goals[id]; ok {
		return m.Title, string(m.Category)
	}
	if m, ok := staticGoal(id); ok {
		return m.Title, string(m.Category)
	}
	it := pc.items[id]
	return it.Title, it.Category
}

// staticGoal knows a goal block by its id alone, for the title of a block
// whose goal has no missing minutes any more.
func staticGoal(id int64) (goalMeta, bool) {
	for goal, gid := range goalIDs {
		if gid == id {
			return goalMeta{Goal: goal, Title: goalEventPrefix + review.Title(goal), Category: goalCategory(goal)}, true
		}
	}
	return goalMeta{}, false
}

// goalOf is the goal a block id stands for, or "" for a real task.
func goalOf(id int64) string {
	if m, ok := staticGoal(id); ok {
		return m.Goal
	}
	return ""
}
