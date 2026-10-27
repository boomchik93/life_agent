package api

import (
	"net/http"
	"time"

	"lifeagent/internal/calendar"
	"lifeagent/internal/load"
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
	writeJSON(w, http.StatusOK, weekResponse{From: from, To: to, Events: events, Warnings: warnings, Risks: risks})
}

func (s *Server) assess(events []calendar.Event, from, to time.Time) (load.Assessment, error) {
	t, cats, err := s.loadSettings()
	if err != nil {
		return load.Assessment{}, err
	}
	now := s.now().In(s.loc)
	first := time.Date(now.Year(), now.Month(), now.Day()-(t.StateDays-1), 0, 0, 0, 0, s.loc)
	stored, err := s.store.Checkins(first.Format("2006-01-02"), now.Format("2006-01-02"))
	if err != nil {
		return load.Assessment{}, err
	}

	in := load.Input{
		From: from, To: to, Loc: s.loc, Now: now, T: t,
		RestTracked: cats.RestTracked(),
	}
	for _, e := range events {
		cat := cats.Of(e.Source, e.Calendar)
		// An event the agent wrote carries the category the user confirmed.
		if c := load.Category(e.Category); c.Valid() {
			cat = c
		}
		in.Events = append(in.Events, load.Event{Start: e.Start, End: e.End, AllDay: e.AllDay, Category: cat})
	}
	for _, c := range stored {
		in.Checkins = append(in.Checkins, load.Checkin{Date: c.Date, Energy: c.Energy, Mood: c.Mood, SleepHours: c.SleepHours})
	}
	return load.Assess(in), nil
}

// loadSettings reads the thresholds and calendar categories; stored values
// override the defaults.
func (s *Server) loadSettings() (load.Thresholds, load.CalendarCategories, error) {
	kv, err := s.store.Settings()
	if err != nil {
		return load.Thresholds{}, nil, err
	}
	t := load.DefaultThresholds()
	if raw, ok := kv[settingThresholds]; ok {
		if t, err = load.ParseThresholds([]byte(raw)); err != nil {
			return t, nil, err
		}
	}
	var cats load.CalendarCategories
	if raw, ok := kv[settingCalendarCategories]; ok {
		if cats, err = load.ParseCalendarCategories([]byte(raw)); err != nil {
			return t, nil, err
		}
	}
	return t, cats, nil
}
