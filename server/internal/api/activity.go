package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"lifeagent/internal/activity"
	"lifeagent/internal/store"
)

const (
	activityKeepDays = 400 // the oldest day a client may send
	confirmDeleteAll = "delete-all-data"
)

type activityRowIn struct {
	Category     string   `json:"category"`
	Minutes      int      `json:"minutes"`
	NightMinutes int      `json:"night_minutes"`
	Count        int      `json:"count"`
	Confidence   *float64 `json:"confidence"` // default 1
}

// activityIn is the whole vocabulary of a client's summary: a source, a day,
// categories with numbers. Any other field, a title or a URL among them, makes
// the request fail at decoding, so it never reaches the database or a log.
type activityIn struct {
	Source string          `json:"source"`
	Date   string          `json:"date"`
	Rows   []activityRowIn `json:"rows"`
}

func (s *Server) nowRFC() string { return s.now().UTC().Format(time.RFC3339) }

// postActivityDaily takes the daily sums of the phone or the Mac.
func (s *Server) postActivityDaily(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "body too large")
		return
	}
	var in activityIn
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "body must be {source, date, rows:[{category, minutes, night_minutes, count, confidence}]}")
		return
	}
	if !activity.ClientSource(in.Source) {
		writeError(w, http.StatusBadRequest, "source must be android_usage or mac_activitywatch")
		return
	}
	today := s.now().In(s.loc)
	day, err := time.ParseInLocation("2006-01-02", in.Date, s.loc)
	switch {
	case err != nil:
		writeError(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
		return
	case day.After(today):
		writeError(w, http.StatusBadRequest, "date must not be in the future")
		return
	case today.Sub(day) > activityKeepDays*24*time.Hour:
		writeError(w, http.StatusBadRequest, "date is too old")
		return
	case len(in.Rows) == 0 || len(in.Rows) > len(activity.Categories):
		writeError(w, http.StatusBadRequest, "rows must hold 1..8 categories")
		return
	}
	seen := map[string]bool{}
	rows := make([]store.ActivityRow, 0, len(in.Rows))
	for _, ri := range in.Rows {
		conf := 1.0
		if ri.Confidence != nil {
			conf = *ri.Confidence
		}
		row := activity.Row{Category: ri.Category, Minutes: ri.Minutes, NightMinutes: ri.NightMinutes, Count: ri.Count, Confidence: conf}
		if err := row.Validate(in.Source); err != nil {
			writeError(w, http.StatusBadRequest, "rows: "+err.Error())
			return
		}
		if seen[ri.Category] {
			writeError(w, http.StatusBadRequest, "rows: a category appears twice")
			return
		}
		seen[ri.Category] = true
		rows = append(rows, store.ActivityRow{Category: ri.Category, Minutes: ri.Minutes, NightMinutes: ri.NightMinutes,
			Count: ri.Count, Confidence: conf})
	}

	on, err := s.store.SourceEnabled(in.Source)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !on {
		writeCode(w, http.StatusConflict, "source_disabled", "Этот источник выключен. Включи его на экране «Источники».")
		return
	}
	if err := s.store.ReplaceActivity(in.Date, in.Source, rows); err != nil {
		s.fail(w, err)
		return
	}
	if err := s.store.RecordSync(in.Source, s.nowRFC(), nil); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"source": in.Source, "date": in.Date, "rows": len(rows)})
}

func (s *Server) getSources(w http.ResponseWriter, r *http.Request) {
	states, err := s.store.SourceStates(activity.Sources)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": states})
}

func (s *Server) putSource(w http.ResponseWriter, r *http.Request) {
	source := r.PathValue("source")
	if !activity.ValidSource(source) {
		writeError(w, http.StatusNotFound, "unknown source")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "body too large")
		return
	}
	var in struct {
		Enabled *bool `json:"enabled"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil || in.Enabled == nil {
		writeError(w, http.StatusBadRequest, `body must be {"enabled": true|false}`)
		return
	}
	if err := s.store.SetSourceEnabled(source, *in.Enabled); err != nil {
		s.fail(w, err)
		return
	}
	s.getSources(w, r)
}

// deleteActivity erases the data of one source.
func (s *Server) deleteActivity(w http.ResponseWriter, r *http.Request) {
	source := r.URL.Query().Get("source")
	if !activity.ValidSource(source) {
		writeError(w, http.StatusBadRequest, "source must be one of "+joinComma(activity.Sources))
		return
	}
	n, err := s.store.DeleteActivity(source)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"source": source, "deleted": n})
}

func joinComma(list []string) string {
	out := ""
	for i, x := range list {
		if i > 0 {
			out += ", "
		}
		out += x
	}
	return out
}

// getExport hands over everything the agent stores about the user.
func (s *Server) getExport(w http.ResponseWriter, r *http.Request) {
	data, err := s.store.Export()
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="life-agent-export.json"`)
	writeJSON(w, http.StatusOK, map[string]any{"exported_at": s.nowRFC(), "tables": data})
}

// deleteData erases all of the user's data. It needs the confirmation phrase
// in the body, so a stray request cannot do it.
func (s *Server) deleteData(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "body too large")
		return
	}
	var in struct {
		Confirm string `json:"confirm"`
	}
	if json.Unmarshal(body, &in) != nil || in.Confirm != confirmDeleteAll {
		writeCode(w, http.StatusBadRequest, "confirm_required", `Нужно подтверждение: {"confirm":"`+confirmDeleteAll+`"}.`)
		return
	}
	counts, err := s.store.DeleteAllData()
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": counts})
}
