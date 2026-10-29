package api

import (
	"net/http"
	"time"

	"lifeagent/internal/calendar"
	"lifeagent/internal/load"
	"lifeagent/internal/store"
)

const (
	settingThresholds         = "load_thresholds"
	settingCalendarCategories = "calendar_categories"
)

type weekResponse struct {
	From     time.Time        `json:"from"`
	To       time.Time        `json:"to"`
	Events   []calendar.Event `json:"events"`
	Warnings []string         `json:"warnings"`
	Risks    load.Assessment  `json:"risks"`
}

// getWeek returns the events of one Monday-to-Monday week and the burnout
// risks. The optional ?start=YYYY-MM-DD selects the week containing that date;
// default is this week.
func (s *Server) getWeek(w http.ResponseWriter, r *http.Request) {
	if s.cal == nil {
		writeError(w, http.StatusServiceUnavailable, "calendars are not configured")
		return
	}
	day := s.now()
	if v := r.URL.Query().Get("start"); v != "" {
		t, err := time.ParseInLocation("2006-01-02", v, s.loc)
		if err != nil {
			writeError(w, http.StatusBadRequest, "start must be YYYY-MM-DD")
			return
		}
		day = t
	}
	from, to := calendar.WeekBounds(day, s.loc)
	events, warnings := s.cal.Collect(r.Context(), from, to)
	if events == nil {
		events = []calendar.Event{}
	}
	if warnings == nil {
		warnings = []string{}
	}

	risks, err := s.assess(events, from, to)
	if err != nil {
		s.fail(w, err)
		return
	}
	// Every event carries its category, the one the load rules use, so the
	// app can tell study from rest without knowing the calendar mapping.
	_, cats, err := s.loadSettings()
	if err != nil {
		s.fail(w, err)
		return
	}
	view := make([]calendar.Event, len(events))
	for i, e := range events {
		e.Category = string(eventCategory(cats, e))
		view[i] = e
	}
	writeJSON(w, http.StatusOK, weekResponse{From: from, To: to, Events: view, Warnings: warnings, Risks: risks})
}

func (s *Server) assess(events []calendar.Event, from, to time.Time) (load.Assessment, error) {
	return s.assessAt(events, from, to, s.now())
}

// assessAt evaluates the risks as they were at [at]: the history of the home
// screen asks for earlier moments.
func (s *Server) assessAt(events []calendar.Event, from, to, at time.Time) (load.Assessment, error) {
	d, err := s.riskData(at, at)
	if err != nil {
		return load.Assessment{}, err
	}
	return s.assessWith(d, events, from, to, at), nil
}

// riskData is what the risks are made of besides the events: the settings
// and the check-ins and health days the assessments of [first, last] need.
type riskData struct {
	t        load.Thresholds
	cats     load.EventCategories
	checkins []store.Checkin
	health   []store.Health
}

// stateFirst is the first day whose state counts for an assessment at [at].
func (s *Server) stateFirst(t load.Thresholds, at time.Time) time.Time {
	now := at.In(s.loc)
	return time.Date(now.Year(), now.Month(), now.Day()-(t.StateDays-1), 0, 0, 0, 0, s.loc)
}

// riskData reads, in one go, what the assessments at the moments from
// [first] to [last] need.
func (s *Server) riskData(first, last time.Time) (riskData, error) {
	t, cats, err := s.loadSettings()
	if err != nil {
		return riskData{}, err
	}
	from, to := s.stateFirst(t, first).Format("2006-01-02"), last.In(s.loc).Format("2006-01-02")
	stored, err := s.store.Checkins(from, to)
	if err != nil {
		return riskData{}, err
	}
	health, err := s.store.Healths(from, to)
	if err != nil {
		return riskData{}, err
	}
	return riskData{t: t, cats: cats, checkins: stored, health: health}, nil
}

// assessWith evaluates the risks at [at] from data read before; only the
// days of its window are taken.
func (s *Server) assessWith(d riskData, events []calendar.Event, from, to, at time.Time) load.Assessment {
	now := at.In(s.loc)
	first, last := s.stateFirst(d.t, at).Format("2006-01-02"), now.Format("2006-01-02")
	in := load.Input{
		From: from, To: to, Loc: s.loc, Now: now, T: d.t,
		RestTracked: d.cats.RestTracked(),
	}
	for _, h := range d.health {
		if h.SleepHours != nil && h.Date >= first && h.Date <= last {
			in.Sleep = append(in.Sleep, load.SleepDay{Date: h.Date, Hours: *h.SleepHours})
		}
	}
	for _, e := range events {
		in.Events = append(in.Events, load.Event{Start: e.Start, End: e.End, AllDay: e.AllDay, Category: eventCategory(d.cats, e)})
	}
	for _, c := range d.checkins {
		if c.Date >= first && c.Date <= last {
			in.Checkins = append(in.Checkins, load.Checkin{Date: c.Date, Energy: c.Energy, Mood: c.Mood, SleepHours: c.SleepHours})
		}
	}
	return load.Assess(in)
}

// eventCategory is the category of a calendar event. An event the agent wrote
// carries the category the user confirmed; the rest gets it from the memory of
// titles, its calendar or the words of its title.
func eventCategory(cats load.EventCategories, e calendar.Event) load.Category {
	if c := load.Category(e.Category); c.Valid() {
		return c
	}
	return cats.Of(e.Source, e.Calendar, e.Title)
}

// loadSettings reads the thresholds, the calendar categories and the memory
// of titles; stored values override the defaults.
func (s *Server) loadSettings() (load.Thresholds, load.EventCategories, error) {
	kv, err := s.store.Settings()
	if err != nil {
		return load.Thresholds{}, load.EventCategories{}, err
	}
	t := load.DefaultThresholds()
	if raw, ok := kv[settingThresholds]; ok {
		if t, err = load.ParseThresholds([]byte(raw)); err != nil {
			return t, load.EventCategories{}, err
		}
	}
	var cats load.EventCategories
	if raw, ok := kv[settingCalendarCategories]; ok {
		if cats.Calendars, err = load.ParseCalendarCategories([]byte(raw)); err != nil {
			return t, cats, err
		}
	}
	mem, err := s.store.CategoryMemory()
	if err != nil {
		return t, cats, err
	}
	cats.Memory = make(map[string]load.Category, len(mem))
	for name, c := range mem {
		cats.Memory[name] = load.Category(c)
	}
	return t, cats, nil
}
