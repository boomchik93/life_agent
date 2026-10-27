package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"lifeagent/internal/store"
)

type checkinRequest struct {
	Date       string   `json:"date"` // optional, default today
	Energy     int      `json:"energy"`
	Mood       int      `json:"mood"`
	SleepHours *float64 `json:"sleep_hours"` // optional
}

// postCheckin saves the daily check-in; a repeat for the same date replaces it.
func (s *Server) postCheckin(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "body too large")
		return
	}
	var in checkinRequest
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "body must be a JSON object with energy, mood and optional date, sleep_hours")
		return
	}

	today := s.now().In(s.loc).Format("2006-01-02")
	if in.Date == "" {
		in.Date = today
	}
	if _, err := time.ParseInLocation("2006-01-02", in.Date, s.loc); err != nil {
		writeError(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
		return
	}
	switch {
	case in.Date > today:
		writeError(w, http.StatusBadRequest, "date must not be in the future")
		return
	case in.Energy < 1 || in.Energy > 5:
		writeError(w, http.StatusBadRequest, "energy must be 1..5")
		return
	case in.Mood < 1 || in.Mood > 5:
		writeError(w, http.StatusBadRequest, "mood must be 1..5")
		return
	case in.SleepHours != nil && (*in.SleepHours < 0 || *in.SleepHours > 24):
		writeError(w, http.StatusBadRequest, "sleep_hours must be 0..24")
		return
	}

	c := store.Checkin{Date: in.Date, Energy: in.Energy, Mood: in.Mood, SleepHours: in.SleepHours}
	if err := s.store.PutCheckin(c); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}
