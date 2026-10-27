package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"lifeagent/internal/calendar"
	"lifeagent/internal/category"
	"lifeagent/internal/load"
	"lifeagent/internal/store"
)

const writeTimeout = 25 * time.Second

// idempotencyKey reads the required header; it keeps a double tap from writing twice.
func idempotencyKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	k := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if n := utf8.RuneCountInString(k); n < 8 || n > 100 || strings.ContainsAny(k, " \t\r\n") {
		writeError(w, http.StatusBadRequest, "Idempotency-Key header (8..100 characters, no spaces) is required")
		return "", false
	}
	return k, true
}

type writtenResponse struct {
	Item      store.Item `json:"item"`
	Duplicate bool       `json:"duplicate"`
}

// postEvents writes a confirmed event to the calendar. The server checks the
// slot again: a risk that turns red or an overlap is written only when the
// user accepted the warning.
func (s *Server) postEvents(w http.ResponseWriter, r *http.Request) {
	key, ok := idempotencyKey(w, r)
	if !ok {
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
	// A repeat of a finished request returns what it made.
	if it, found, err := s.store.ItemByKey(key); err != nil {
		s.fail(w, err)
		return
	} else if found {
		writeJSON(w, http.StatusOK, writtenResponse{Item: it, Duplicate: true})
		return
	}
	if s.cal == nil || s.writer == nil {
		writeCode(w, http.StatusServiceUnavailable, "write_not_configured",
			"На сервере не настроен календарь для записи.")
		return
	}

	res, status, err := s.evaluate(r, sl)
	if err != nil {
		s.evalFail(w, status, err)
		return
	}
	if res.AskCategory {
		writeCode(w, http.StatusConflict, "category_unconfirmed", "Сначала подтверди категорию события.")
		return
	}
	if res.NeedsConfirm && !sl.accepted {
		writeJSON(w, http.StatusConflict, map[string]any{
			"code": "needs_confirm", "error": "Событие требует подтверждения.", "check": res,
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), writeTimeout)
	defer cancel()
	ne := calendar.NewEvent{
		ID: calendar.EventID(key), Title: sl.title, Category: string(res.Category),
		Recurrence: sl.recurrence, Start: sl.start, End: sl.end,
	}
	if _, err := s.writer.Insert(ctx, ne); err != nil {
		if errors.Is(err, calendar.ErrNoWriteAccess) {
			writeCode(w, http.StatusBadGateway, "no_write_access",
				"У сервера нет права записи в этот календарь. Дай сервисному аккаунту доступ «Вносить изменения в мероприятия».")
			return
		}
		s.logger.Error("calendar insert failed", "err", err)
		writeCode(w, http.StatusBadGateway, "calendar_error", "Календарь не принял событие. Попробуй ещё раз.")
		return
	}

	start, end := sl.start.Format(time.RFC3339), sl.end.Format(time.RFC3339)
	item := store.Item{
		Kind: "event", Title: sl.title, Category: string(res.Category), Start: &start, End: &end,
		Status: "written", IdempotencyKey: key, EventID: &ne.ID,
	}
	if sl.recurrence != "" {
		item.Recurrence = &sl.recurrence
	}
	saved, created, err := s.store.AddItem(item)
	if err != nil {
		// The event is in the calendar already; a retry with the same key
		// finds the same event id and only repeats this bookkeeping.
		s.fail(w, err)
		return
	}
	if err := s.store.RememberCategory(category.Normalize(sl.title), string(res.Category), "user"); err != nil {
		s.logger.Error("remember category failed", "err", err)
	}
	writeJSON(w, http.StatusOK, writtenResponse{Item: saved, Duplicate: !created})
}

type taskRequest struct {
	Title             string `json:"title"`
	Category          string `json:"category"`
	CategoryConfirmed bool   `json:"category_confirmed"`
	DurationMin       int    `json:"duration_min"`
	Deadline          string `json:"deadline"` // YYYY-MM-DD
	Priority          int    `json:"priority"`
}

// postItems saves a task with a deadline. Nothing is written to the calendar:
// the week planner places tasks later and asks first.
func (s *Server) postItems(w http.ResponseWriter, r *http.Request) {
	key, ok := idempotencyKey(w, r)
	if !ok {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "body too large")
		return
	}
	var in taskRequest
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "body must be a JSON object")
		return
	}
	title := strings.TrimSpace(in.Title)
	switch {
	case utf8.RuneCountInString(title) < 1 || utf8.RuneCountInString(title) > maxTitleRunes:
		writeError(w, http.StatusBadRequest, "Название: от 1 до 200 символов.")
		return
	case !load.Category(in.Category).Valid():
		writeError(w, http.StatusBadRequest, "category must be one of study, work, hobby, rest, chores, other")
		return
	case in.DurationMin < 5 || in.DurationMin > 7*24*60:
		writeError(w, http.StatusBadRequest, "Длительность: от 5 минут до недели.")
		return
	case in.Priority < 0 || in.Priority > 3:
		writeError(w, http.StatusBadRequest, "priority must be 0..3")
		return
	}
	if _, err := time.ParseInLocation("2006-01-02", in.Deadline, s.loc); err != nil {
		writeError(w, http.StatusBadRequest, "deadline must be YYYY-MM-DD")
		return
	}

	it := store.Item{
		Kind: "task", Title: title, Category: in.Category, DurationMin: &in.DurationMin,
		Deadline: &in.Deadline, Priority: in.Priority, Status: "confirmed", IdempotencyKey: key,
	}
	saved, created, err := s.store.AddItem(it)
	if err != nil {
		s.fail(w, err)
		return
	}
	if created && in.CategoryConfirmed {
		if err := s.store.RememberCategory(category.Normalize(title), in.Category, "user"); err != nil {
			s.logger.Error("remember category failed", "err", err)
		}
	}
	writeJSON(w, http.StatusOK, writtenResponse{Item: saved, Duplicate: !created})
}
