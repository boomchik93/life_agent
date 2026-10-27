package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"lifeagent/internal/store"
)

type healthRequest struct {
	Date       string   `json:"date"` // optional, default today
	SleepHours *float64 `json:"sleep_hours"`
	AvgHR      *float64 `json:"avg_hr"`
	Stress     *float64 `json:"stress"`
}

func inRange(v *float64, lo, hi float64) bool { return v == nil || (*v >= lo && *v <= hi) }

// postHealthDaily stores one day's summary from Health Connect. Raw samples
// never reach the server, and nothing here is logged.
func (s *Server) postHealthDaily(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "body too large")
		return
	}
	var in healthRequest
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "body must be a JSON object with date and sleep_hours, avg_hr, stress")
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
	case in.SleepHours == nil && in.AvgHR == nil && in.Stress == nil:
		writeError(w, http.StatusBadRequest, "at least one of sleep_hours, avg_hr, stress is required")
		return
	case !inRange(in.SleepHours, 0, 24):
		writeError(w, http.StatusBadRequest, "sleep_hours must be 0..24")
		return
	case !inRange(in.AvgHR, 20, 250):
		writeError(w, http.StatusBadRequest, "avg_hr must be 20..250")
		return
	case !inRange(in.Stress, 0, 100):
		writeError(w, http.StatusBadRequest, "stress must be 0..100")
		return
	}
	h := store.Health{Date: in.Date, SleepHours: in.SleepHours, AvgHR: in.AvgHR, Stress: in.Stress}
	if err := s.store.PutHealth(h); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h)
}

// deleteHealth wipes all health summaries.
func (s *Server) deleteHealth(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.DeleteHealth()
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}
