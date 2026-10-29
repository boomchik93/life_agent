package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"lifeagent/internal/calendar"
	"lifeagent/internal/category"
	"lifeagent/internal/load"
)

const (
	maxTitleRunes = 200
	maxEventSpan  = 24 * time.Hour
)

// slotRequest is an event as the app sends it to /v1/check and /v1/events.
type slotRequest struct {
	Title      string  `json:"title"`
	Start      string  `json:"start"` // RFC 3339
	End        string  `json:"end"`
	Recurrence *string `json:"recurrence"`
	Category   string  `json:"category"`
	// CategoryConfirmed: the user answered "is this study?" or chose the category.
	CategoryConfirmed bool `json:"category_confirmed"`
	// Confirmed: the user saw the warnings and wants the event anyway.
	Confirmed bool `json:"confirmed"`
}

type slot struct {
	title      string
	start, end time.Time
	recurrence string
	category   load.Category
	confirmed  bool // category confirmed by the user
	accepted   bool // warnings accepted by the user
}

func (s *Server) parseSlot(body []byte) (slot, string) {
	var in slotRequest
	if err := json.Unmarshal(body, &in); err != nil {
		return slot{}, "body must be a JSON object"
	}
	title := strings.TrimSpace(in.Title)
	if n := utf8.RuneCountInString(title); n < 1 || n > maxTitleRunes {
		return slot{}, fmt.Sprintf("Название: от 1 до %d символов.", maxTitleRunes)
	}
	start, err1 := s.parseMoment(in.Start)
	end, err2 := s.parseMoment(in.End)
	if err1 != nil || err2 != nil {
		return slot{}, "start and end must be RFC 3339 times or YYYY-MM-DDTHH:MM"
	}
	if !end.After(start) {
		return slot{}, "Конец события должен быть позже начала."
	}
	if end.Sub(start) > maxEventSpan {
		return slot{}, "Событие длиннее суток: раздели его на части."
	}
	cat := load.Category(in.Category)
	if !cat.Valid() {
		return slot{}, "category must be one of study, work, sport, hobby, rest, chores, other"
	}
	sl := slot{title: title, start: start.In(s.loc), end: end.In(s.loc), category: cat,
		confirmed: in.CategoryConfirmed, accepted: in.Confirmed}
	if in.Recurrence != nil && *in.Recurrence != "" {
		if err := calendar.ValidateRRULE(*in.Recurrence, s.loc); err != nil {
			return slot{}, "recurrence: " + err.Error()
		}
		sl.recurrence = *in.Recurrence
	}
	return sl, ""
}

// parseMoment reads an RFC 3339 time, or a wall-clock time in the user's zone
// ("2026-10-30T18:00"), which is what the app sends: the phone's own zone does not count.
func (s *Server) parseMoment(v string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	return time.ParseInLocation("2006-01-02T15:04", v, s.loc)
}

type overlapView struct {
	Title string    `json:"title"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type checkResponse struct {
	// OK: nothing needs a second look. Otherwise the app asks before writing.
	OK             bool            `json:"ok"`
	NeedsConfirm   bool            `json:"needs_confirm"`
	Category       load.Category   `json:"category"`
	CategorySrc    string          `json:"category_source"` // memory, user or guess
	AskCategory    bool            `json:"ask_category"`
	Overlaps       []overlapView   `json:"overlaps"`
	Reasons        []string        `json:"reasons"`
	Worsened       []string        `json:"worsened"` // risks that got worse: sleep, overload, no_rest, state
	Before         load.Assessment `json:"before"`
	After          load.Assessment `json:"after"`
	RecurrenceNote string          `json:"recurrence_note,omitempty"`
}

var riskNames = []struct{ key, name string }{
	{"sleep", "Недосып"}, {"overload", "Перегруз"}, {"no_rest", "Нет отдыха"}, {"state", "Состояние"},
}

func riskOf(a load.Assessment, key string) load.Risk {
	switch key {
	case "sleep":
		return a.Sleep
	case "overload":
		return a.Overload
	case "no_rest":
		return a.NoRest
	}
	return a.State
}

// severity ranks colours for "got worse"; no data counts as green here, because
// a colour that appears with the new event was caused by it.
func severity(c load.Color) int {
	switch c {
	case load.Yellow:
		return 2
	case load.Red:
		return 3
	}
	return 1
}

// worsenedByEvent: the new event pushed the risk to yellow or red. Without a
// baseline week the rest risk says nothing about the event: an empty week
// reads "no rest" simply because nothing is planned yet.
func worsenedByEvent(key string, before, after load.Risk) bool {
	if after.Color != load.Yellow && after.Color != load.Red {
		return false
	}
	if key == "no_rest" && before.Color == load.Gray {
		return false
	}
	return severity(after.Color) > severity(before.Color)
}

var colorWords = map[load.Color]string{load.Yellow: "жёлтым", load.Red: "красным"}

// evaluate answers "can this slot be added": overlaps, the risks before and
// after, and the category. It reads the calendars and writes nothing.
func (s *Server) evaluate(r *http.Request, sl slot) (checkResponse, int, error) {
	out := checkResponse{Overlaps: []overlapView{}, Reasons: []string{}, Worsened: []string{}}

	cat, src, ask, err := s.resolveCategory(sl)
	if err != nil {
		return out, 0, err
	}
	out.Category, out.CategorySrc, out.AskCategory = cat, src, ask

	from, to := calendar.WeekBounds(sl.start, s.loc)
	events, warnings := s.cal.Collect(r.Context(), from, to)
	if len(warnings) > 0 && len(events) == 0 {
		return out, http.StatusBadGateway, fmt.Errorf("calendars unavailable: %s", warnings[0])
	}
	for _, e := range events {
		if e.AllDay || !e.End.After(e.Start) {
			continue
		}
		if e.End.After(sl.start) && e.Start.Before(sl.end) {
			out.Overlaps = append(out.Overlaps, overlapView{Title: e.Title, Start: e.Start, End: e.End})
		}
	}

	if out.Before, err = s.assess(events, from, to); err != nil {
		return out, 0, err
	}
	with := append(append([]calendar.Event(nil), events...), calendar.Event{
		Source: "google", Title: sl.title, Start: sl.start, End: sl.end, Category: string(cat),
	})
	if out.After, err = s.assess(with, from, to); err != nil {
		return out, 0, err
	}

	for _, o := range out.Overlaps {
		out.Reasons = append(out.Reasons, fmt.Sprintf("Пересекается с «%s» (%s–%s).",
			o.Title, o.Start.In(s.loc).Format("15:04"), o.End.In(s.loc).Format("15:04")))
		out.NeedsConfirm = true
	}
	if sl.start.Before(s.now()) {
		out.Reasons = append(out.Reasons, "Это время уже прошло.")
		out.NeedsConfirm = true
	}
	for _, rn := range riskNames {
		before, after := riskOf(out.Before, rn.key), riskOf(out.After, rn.key)
		if !worsenedByEvent(rn.key, before, after) {
			continue
		}
		out.Worsened = append(out.Worsened, rn.key)
		reason := fmt.Sprintf("%s станет %s.", rn.name, colorWords[after.Color])
		if len(after.Reasons) > 0 {
			reason += " " + strings.Join(after.Reasons, " ")
		}
		out.Reasons = append(out.Reasons, reason)
		if after.Color == load.Red {
			out.NeedsConfirm = true
		}
	}
	if sl.recurrence != "" {
		out.RecurrenceNote = "Повтор проверен только для недели первого события."
	}
	out.OK = len(out.Reasons) == 0
	return out, 0, nil
}

// resolveCategory picks the category: the user's own choice, else the memory,
// else the model's guess, which the user still has to confirm.
func (s *Server) resolveCategory(sl slot) (cat load.Category, src string, ask bool, err error) {
	if sl.confirmed {
		return sl.category, "user", false, nil
	}
	mem, ok, err := s.store.CategoryOf(category.Normalize(sl.title))
	if err != nil {
		return "", "", false, err
	}
	if ok && load.Category(mem).Valid() {
		return load.Category(mem), "memory", false, nil
	}
	return sl.category, "guess", true, nil
}

func (s *Server) postCheck(w http.ResponseWriter, r *http.Request) {
	if s.cal == nil {
		writeError(w, http.StatusServiceUnavailable, "calendars are not configured")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "body too large")
		return
	}
	sl, msg := s.parseSlot(body)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	res, status, err := s.evaluate(r, sl)
	if err != nil {
		s.evalFail(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) evalFail(w http.ResponseWriter, status int, err error) {
	if status == http.StatusBadGateway {
		s.logger.Error("check failed", "err", err)
		writeCode(w, status, "calendars_unavailable",
			"Не удалось прочитать календари, поэтому проверить слот нельзя. Попробуй позже.")
		return
	}
	s.fail(w, err)
}
