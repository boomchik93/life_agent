package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"lifeagent/internal/activity"
	"lifeagent/internal/appcat"
	"lifeagent/internal/calendar"
	"lifeagent/internal/load"
	"lifeagent/internal/review"
)

const (
	maxRangeDays = 400
)

// designCategories are the categories of the "Время" tab.
var designCategories = []string{"study", "work", "sport", "hobby", "rest", "chores", "other"}

func validDesignCategory(c string) bool {
	for _, d := range designCategories {
		if d == c {
			return true
		}
	}
	return false
}

// screenCategory maps the category of an app to a category of the tab.
// Video, music and games on a screen are rest, not a hobby. Unknown and sleep
// say nothing about what the time was spent on.
func screenCategory(c string) (string, bool) {
	switch c {
	case activity.Study:
		return "study", true
	case activity.Work, activity.Career:
		return "work", true
	case activity.Sport:
		return "sport", true
	case activity.Leisure:
		return "rest", true
	case activity.Social:
		return "other", true
	}
	return "", false
}

// tspan is a stretch of time with a category; src is "timer" or "calendar".
type tspan struct {
	s, e  time.Time
	cat   string
	title string
	src   string
}

func (p tspan) minutes() float64 { return p.e.Sub(p.s).Minutes() }

// spansIn collects what filled the time in [from, to): the timed events of
// the calendar, each with its category.
func (s *Server) spansIn(ctx context.Context, from, to time.Time) ([]tspan, error) {
	var out []tspan
	if s.cal == nil {
		return out, nil
	}
	_, cats, err := s.loadSettings()
	if err != nil {
		return nil, err
	}
	events, _ := s.cal.Collect(ctx, from, to)
	for _, ev := range events {
		if ev.AllDay || !ev.End.After(ev.Start) {
			continue
		}
		out = append(out, tspan{s: ev.Start, e: ev.End, cat: string(eventCategory(cats, ev)), title: ev.Title, src: "calendar"})
	}
	return out, nil
}

// perDay sums the spans by local day and category, clipped to [from, to).
func (s *Server) perDay(spans []tspan, from, to time.Time) map[string]map[string]float64 {
	out := map[string]map[string]float64{}
	for _, p := range spans {
		st, en := p.s.In(s.loc), p.e.In(s.loc)
		if st.Before(from) {
			st = from
		}
		if en.After(to) {
			en = to
		}
		for st.Before(en) {
			next := time.Date(st.Year(), st.Month(), st.Day()+1, 0, 0, 0, 0, s.loc)
			end := en
			if next.Before(end) {
				end = next
			}
			d := st.Format("2006-01-02")
			if out[d] == nil {
				out[d] = map[string]float64{}
			}
			out[d][p.cat] += end.Sub(st).Minutes()
			st = end
		}
	}
	return out
}

func sumByCategory(days map[string]map[string]float64) map[string]int {
	f := map[string]float64{}
	for _, m := range days {
		for c, v := range m {
			f[c] += v
		}
	}
	out := map[string]int{}
	for c, v := range f {
		out[c] = int(math.Round(v))
	}
	return out
}

func total(m map[string]int) int {
	t := 0
	for _, v := range m {
		t += v
	}
	return t
}

// parseRange reads ?from=&to= (to is exclusive); the default is the week of today.
func (s *Server) parseRange(r *http.Request) (from, to time.Time, ok bool) {
	q := r.URL.Query()
	parse := func(v string) (time.Time, bool) {
		t, err := time.ParseInLocation("2006-01-02", v, s.loc)
		return t, err == nil
	}
	if q.Get("from") == "" && q.Get("to") == "" {
		now := s.now().In(s.loc)
		wd := (int(now.Weekday()) + 6) % 7
		from = time.Date(now.Year(), now.Month(), now.Day()-wd, 0, 0, 0, 0, s.loc)
		return from, from.AddDate(0, 0, 7), true
	}
	var ok1, ok2 bool
	from, ok1 = parse(q.Get("from"))
	to, ok2 = parse(q.Get("to"))
	if !ok1 || !ok2 || !to.After(from) || to.Sub(from) > maxRangeDays*24*time.Hour {
		return from, to, false
	}
	return from, to, true
}

func (s *Server) screenByCategory(from, to time.Time) (map[string]int, int, error) {
	rows, err := s.store.Activity(ymd(from), ymd(to.AddDate(0, 0, -1)), "")
	if err != nil {
		return nil, 0, err
	}
	out := map[string]int{}
	unknown := 0
	for _, r := range rows {
		if r.Source != activity.AndroidUsage && r.Source != activity.MacActivityWatch {
			continue
		}
		if r.Category == activity.Unknown {
			unknown += r.Minutes
			continue
		}
		if c, ok := screenCategory(r.Category); ok {
			out[c] += r.Minutes
		}
	}
	return out, unknown, nil
}

type dayView struct {
	Date       string         `json:"date"`
	ByCategory map[string]int `json:"by_category"`
}

type overviewResponse struct {
	From       string         `json:"from"`
	To         string         `json:"to"`
	TotalMin   int            `json:"total_min"`
	ByCategory map[string]int `json:"by_category"`
	Previous   map[string]int `json:"previous"` // the same length just before; null for none
	Days       []dayView      `json:"days"`
	// Screen is the part of the time spent in apps, by category, and Unknown
	// the screen time no category was found for.
	Screen        map[string]int `json:"screen"`
	ScreenUnknown int            `json:"screen_unknown_min"`
}

// getTimeOverview answers "where does the time go" for a range: timer entries
// and the calendar, by category, by day, and the same for the period before.
func (s *Server) getTimeOverview(w http.ResponseWriter, r *http.Request) {
	from, to, ok := s.parseRange(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "from and to must be YYYY-MM-DD, to after from, at most 400 days")
		return
	}
	prevFrom := from.Add(-(to.Sub(from)))
	// The calendar and the timer for the period and the one before in one pass.
	spans, err := s.spansIn(r.Context(), prevFrom, to)
	if err != nil {
		s.fail(w, err)
		return
	}
	cur := s.perDay(spans, from, to)
	resp := overviewResponse{From: ymd(from), To: ymd(to), ByCategory: sumByCategory(cur)}
	resp.TotalMin = total(resp.ByCategory)
	if r.URL.Query().Get("compare") == "1" {
		resp.Previous = sumByCategory(s.perDay(spans, prevFrom, from))
	}
	resp.Days = []dayView{}
	for d := from; d.Before(to); d = d.AddDate(0, 0, 1) {
		resp.Days = append(resp.Days, dayView{Date: ymd(d), ByCategory: sumByCategory(map[string]map[string]float64{ymd(d): cur[ymd(d)]})})
	}
	if resp.Screen, resp.ScreenUnknown, err = s.screenByCategory(from, to); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

type partView struct {
	Name    string `json:"name"`
	Minutes int    `json:"minutes"`
}

type recordView struct {
	Title   string `json:"title"`
	Start   string `json:"start"`
	End     string `json:"end"`
	Minutes int    `json:"minutes"`
	Source  string `json:"source"` // timer or calendar
}

type categoryResponse struct {
	Category   string       `json:"category"`
	TotalMin   int          `json:"total_min"`
	Previous   int          `json:"previous_min"`
	SharePct   int          `json:"share_pct"`
	Days       []dayView    `json:"days"`
	Parts      []partView   `json:"parts"`
	Records    []recordView `json:"records"`
	ScreenMin  int          `json:"screen_min"`
	ScreenUnkn int          `json:"screen_unknown_min"`
}

// getTimeCategory is the detail of one category: by day, by title, the records.
func (s *Server) getTimeCategory(w http.ResponseWriter, r *http.Request) {
	cat := r.URL.Query().Get("category")
	if !validDesignCategory(cat) {
		writeError(w, http.StatusBadRequest, "category must be one of study, work, sport, hobby, rest, chores, other")
		return
	}
	from, to, ok := s.parseRange(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "from and to must be YYYY-MM-DD, to after from, at most 400 days")
		return
	}
	prevFrom := from.Add(-(to.Sub(from)))
	spans, err := s.spansIn(r.Context(), prevFrom, to)
	if err != nil {
		s.fail(w, err)
		return
	}
	all := s.perDay(spans, from, to)
	allTotals := sumByCategory(all)
	prevTotals := sumByCategory(s.perDay(spans, prevFrom, from))

	resp := categoryResponse{Category: cat, TotalMin: allTotals[cat], Previous: prevTotals[cat], Days: []dayView{}, Parts: []partView{}, Records: []recordView{}}
	if t := total(allTotals); t > 0 {
		resp.SharePct = int(math.Round(float64(allTotals[cat]) * 100 / float64(t)))
	}
	for d := from; d.Before(to); d = d.AddDate(0, 0, 1) {
		m := all[ymd(d)]
		resp.Days = append(resp.Days, dayView{Date: ymd(d), ByCategory: map[string]int{cat: int(math.Round(m[cat]))}})
	}

	parts := map[string]float64{}
	display := map[string]string{}
	var recs []tspan
	for _, p := range spans {
		if p.cat != cat || !p.e.After(from) || !p.s.Before(to) {
			continue
		}
		st, en := p.s, p.e
		if st.Before(from) {
			st = from
		}
		if en.After(to) {
			en = to
		}
		key := strings.ToLower(strings.TrimSpace(p.title))
		parts[key] += en.Sub(st).Minutes()
		if _, ok := display[key]; !ok {
			display[key] = p.title
		}
		recs = append(recs, p)
	}
	for k, v := range parts {
		if m := int(math.Round(v)); m > 0 {
			resp.Parts = append(resp.Parts, partView{Name: display[k], Minutes: m})
		}
	}
	sort.Slice(resp.Parts, func(i, j int) bool {
		if resp.Parts[i].Minutes != resp.Parts[j].Minutes {
			return resp.Parts[i].Minutes > resp.Parts[j].Minutes
		}
		return resp.Parts[i].Name < resp.Parts[j].Name
	})
	if len(resp.Parts) > 8 {
		resp.Parts = resp.Parts[:8]
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].s.After(recs[j].s) })
	if len(recs) > 30 {
		recs = recs[:30]
	}
	for _, p := range recs {
		resp.Records = append(resp.Records, recordView{
			Title: p.title, Start: p.s.In(s.loc).Format(time.RFC3339), End: p.e.In(s.loc).Format(time.RFC3339),
			Minutes: int(math.Round(p.minutes())), Source: p.src,
		})
	}
	screen, unknown, err := s.screenByCategory(from, to)
	if err != nil {
		s.fail(w, err)
		return
	}
	resp.ScreenMin, resp.ScreenUnkn = screen[cat], unknown
	writeJSON(w, http.StatusOK, resp)
}

// ---- activities and the timer ----

type heatDay struct {
	Date    string `json:"date"`
	Minutes int    `json:"minutes"`
	// Active: something happened that day even when it has no minutes
	// (GitHub contributions, LeetCode solutions).
	Active bool `json:"active"`
	Future bool `json:"future,omitempty"`
}

// activityView is one of the four goals followed by the numbers the phone, the
// Mac, the watch, the calendar and the services already send: nobody marks anything.
type activityView struct {
	Key         string    `json:"key"` // study, career, sport, work
	Name        string    `json:"name"`
	Category    string    `json:"category"`
	GoalMinutes int       `json:"goal_minutes"` // the weekly norm; 0: none yet
	Streak      int       `json:"streak"`       // days in a row up to today
	ActiveDays  int       `json:"active_days"`  // days with something in the range
	GoalPercent int       `json:"goal_percent"` // of the norm for the range; -1 without a norm
	TotalMin    int       `json:"total_min"`    // in the range
	Heat        []heatDay `json:"heat"`         // Monday-first weeks, the last one is this week
}

// calendarCategoryOf: the calendar categories that count as a goal.
var calendarCategoryOf = map[string]string{"study": "study", "work": "work", "sport": "sport"}

// getActivities follows the four goals by themselves. A day's minutes are
// the larger of two readings: what the screens and the watch saw (apps by
// category, workouts) and what the calendar and the timer say, so a lecture
// that is also on the phone is not counted twice. ?range= is week, month or
// year: the span of the numbers and of the calendar.
func (s *Server) getActivities(w http.ResponseWriter, r *http.Request) {
	rng := r.URL.Query().Get("range")
	weeks, days := 12, 28
	switch rng {
	case "week":
		days = 7
	case "year":
		weeks, days = 53, 365
	case "month", "":
	default:
		writeError(w, http.StatusBadRequest, "range must be week, month or year")
		return
	}
	now := s.now().In(s.loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.loc)
	wd := (int(today.Weekday()) + 6) % 7
	heatFrom := today.AddDate(0, 0, -wd-7*(weeks-1))
	heatTo := today.AddDate(0, 0, 7-wd) // the end of this week

	goals, err := s.store.Goals()
	if err != nil {
		s.fail(w, err)
		return
	}
	act, err := s.store.Activity(ymd(heatFrom), ymd(today), "")
	if err != nil {
		s.fail(w, err)
		return
	}
	hl, err := s.store.Healths(ymd(heatFrom), ymd(today))
	if err != nil {
		s.fail(w, err)
		return
	}
	spans, err := s.spansIn(r.Context(), heatFrom, heatTo)
	if err != nil {
		s.fail(w, err)
		return
	}
	calDays := s.perDay(spans, heatFrom, heatTo)

	// The screens and the watch, by day.
	byDay := map[string][]review.Day{}
	counted := map[string]map[string]bool{} // goal -> days with a count and no minutes
	for _, d := range act {
		if d.Source == activity.Weather {
			continue
		}
		byDay[d.Date] = append(byDay[d.Date], review.Day{Date: d.Date, Source: d.Source, Category: d.Category, Minutes: d.Minutes, Count: d.Count})
		if d.Count > 0 {
			if counted[d.Category] == nil {
				counted[d.Category] = map[string]bool{}
			}
			counted[d.Category][d.Date] = true
		}
	}
	healthByDay := map[string][]review.Health{}
	for _, h := range hl {
		healthByDay[h.Date] = append(healthByDay[h.Date], review.Health{Date: h.Date, SleepHours: h.SleepHours, WorkoutMin: h.WorkoutMin})
	}

	rangeFrom := today.AddDate(0, 0, -(days - 1))
	out := make([]activityView, 0, len(goals))
	for _, g := range goals {
		v := activityView{Key: g.Goal, Name: review.Title(g.Goal), Category: g.Goal, GoalPercent: -1, Heat: []heatDay{}}
		if g.WeeklyNormMin != nil {
			v.GoalMinutes = *g.WeeklyNormMin
		}
		minutes := map[string]int{}
		active := map[string]bool{}
		for d := heatFrom; !d.After(today); d = d.AddDate(0, 0, 1) {
			k := ymd(d)
			m := review.Facts(byDay[k], healthByDay[k])[g.Goal]
			if c, ok := calendarCategoryOf[g.Goal]; ok {
				if cm := int(math.Round(calDays[k][c])); cm > m {
					m = cm
				}
			}
			minutes[k] = m
			active[k] = m > 0 || counted[g.Goal][k]
		}
		for d := heatFrom; d.Before(heatTo); d = d.AddDate(0, 0, 1) {
			k := ymd(d)
			v.Heat = append(v.Heat, heatDay{Date: k, Minutes: minutes[k], Active: active[k], Future: d.After(today)})
			if !d.Before(rangeFrom) && !d.After(today) {
				v.TotalMin += minutes[k]
				if active[k] {
					v.ActiveDays++
				}
			}
		}
		// A day that has not produced anything yet today does not break the streak.
		d := today
		if !active[ymd(d)] {
			d = d.AddDate(0, 0, -1)
		}
		for !d.Before(heatFrom) && active[ymd(d)] {
			v.Streak++
			d = d.AddDate(0, 0, -1)
		}
		if v.GoalMinutes > 0 {
			v.GoalPercent = int(math.Round(float64(v.TotalMin) * 100 / (float64(v.GoalMinutes) * float64(days) / 7)))
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"range": rngOrMonth(rng), "weeks": weeks, "activities": out})
}

func rngOrMonth(r string) string {
	if r == "" {
		return "month"
	}
	return r
}

func readBody(w http.ResponseWriter, r *http.Request, v any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "body too large")
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "bad body: "+err.Error())
		return false
	}
	return true
}

func cleanTitle(s string) (string, bool) {
	t := strings.TrimSpace(s)
	if t == "" || len([]rune(t)) > 80 {
		return "", false
	}
	return t, true
}

// putAppCategory records the category the user chose for an app.
func (s *Server) putAppCategory(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string `json:"name"`
		Category string `json:"category"`
	}
	if !readBody(w, r, &in) {
		return
	}
	key, ok := appcat.NormalizeKey(in.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, "name does not look like the name of an app")
		return
	}
	if !activity.ValidCategory(in.Category) || in.Category == activity.Sleep {
		writeError(w, http.StatusBadRequest, "category must be study, career, sport, work, leisure, social or unknown")
		return
	}
	if err := s.store.SetUserAppCategory(key, in.Category, s.nowRFC()); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": key, "category": in.Category})
}

// ---- risk history ----

type riskDay struct {
	Date     string     `json:"date"`
	Sleep    load.Color `json:"sleep"`
	Overload load.Color `json:"overload"`
	NoRest   load.Color `json:"no_rest"`
	State    load.Color `json:"state"`
	Worst    load.Color `json:"worst"`
}

// getRiskHistory assesses each of the last days as it looked at the end of
// that day, for the sparklines and the strip of the week on the home screen.
func (s *Server) getRiskHistory(w http.ResponseWriter, r *http.Request) {
	if s.cal == nil {
		writeError(w, http.StatusServiceUnavailable, "calendars are not configured")
		return
	}
	days := 7
	if v := r.URL.Query().Get("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 14 {
			writeError(w, http.StatusBadRequest, "days must be 1..14")
			return
		}
		days = n
	}
	now := s.now().In(s.loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.loc)
	first := today.AddDate(0, 0, -(days - 1))
	// Events of every week the days touch; the calendar cache keeps them by
	// week, so /v1/week reads the same ones.
	wd := func(t time.Time) time.Time {
		from, _ := calendar.WeekBounds(t, s.loc)
		return from
	}
	events, _ := s.cal.Collect(r.Context(), wd(first), wd(today).AddDate(0, 0, 7))
	data, err := s.riskData(first, now)
	if err != nil {
		s.fail(w, err)
		return
	}

	out := make([]riskDay, 0, days)
	for d := first; !d.After(today); d = d.AddDate(0, 0, 1) {
		wFrom := wd(d)
		at := d.Add(24*time.Hour - time.Minute)
		if at.After(now) {
			at = now
		}
		a := s.assessWith(data, events, wFrom, wFrom.AddDate(0, 0, 7), at)
		rd := riskDay{Date: ymd(d), Sleep: a.Sleep.Color, Overload: a.Overload.Color, NoRest: a.NoRest.Color, State: a.State.Color}
		rd.Worst = rd.Sleep
		for _, c := range []load.Color{rd.Overload, rd.NoRest, rd.State} {
			rd.Worst = worstOf(rd.Worst, c)
		}
		out = append(out, rd)
	}
	writeJSON(w, http.StatusOK, map[string]any{"days": out})
}

// worstOf ranks red above yellow above green above gray.
func worstOf(a, b load.Color) load.Color {
	rank := func(c load.Color) int {
		switch c {
		case load.Red:
			return 3
		case load.Yellow:
			return 2
		case load.Green:
			return 1
		}
		return 0
	}
	if rank(b) > rank(a) {
		return b
	}
	return a
}
