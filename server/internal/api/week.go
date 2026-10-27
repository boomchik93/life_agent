package api

import (
	"net/http"
	"time"

	"lifeagent/internal/calendar"
)

type weekResponse struct {
	From     time.Time        `json:"from"`
	To       time.Time        `json:"to"`
	Events   []calendar.Event `json:"events"`
	Warnings []string         `json:"warnings"`
}

// getWeek returns the events of one Monday-to-Monday week. The optional
// ?start=YYYY-MM-DD selects the week containing that date; default is this week.
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
	writeJSON(w, http.StatusOK, weekResponse{From: from, To: to, Events: events, Warnings: warnings})
}
