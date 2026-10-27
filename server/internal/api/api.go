package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"lifeagent/internal/calendar"
	"lifeagent/internal/store"
)

const maxBody = 64 << 10

// Collector gathers the events of all calendar sources for a time window.
type Collector interface {
	Collect(ctx context.Context, from, to time.Time) ([]calendar.Event, []string)
}

type Server struct {
	store  *store.Store
	token  string
	logger *slog.Logger
	limit  *limiter
	cal    Collector
	loc    *time.Location
	now    func() time.Time
}

type Options struct {
	Store    *store.Store
	Token    string
	Logger   *slog.Logger
	Calendar Collector // nil: /v1/week reports calendars as not configured
	Location *time.Location
	Now      func() time.Time // defaults to time.Now
	// RatePerSec and Burst limit requests per client on /v1/*.
	RatePerSec float64
	Burst      int
}

func New(o Options) http.Handler {
	s := &Server{
		store:  o.Store,
		token:  o.Token,
		logger: o.Logger,
		limit:  newLimiter(o.RatePerSec, o.Burst),
		cal:    o.Calendar,
		loc:    o.Location,
		now:    o.Now,
	}
	if s.loc == nil {
		s.loc = time.UTC
	}
	if s.now == nil {
		s.now = time.Now
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)

	v1 := http.NewServeMux()
	v1.HandleFunc("GET /v1/settings", s.getSettings)
	v1.HandleFunc("PUT /v1/settings", s.putSettings)
	v1.HandleFunc("GET /v1/week", s.getWeek)
	mux.Handle("/v1/", s.rateLimit(s.auth(v1)))

	return mux
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	kv, err := s.store.Settings()
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make(map[string]json.RawMessage, len(kv))
	for k, v := range kv {
		out[k] = json.RawMessage(v)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "body too large")
		return
	}
	var in map[string]json.RawMessage
	if err := json.Unmarshal(body, &in); err != nil || in == nil {
		writeError(w, http.StatusBadRequest, "body must be a JSON object")
		return
	}
	kv := make(map[string]string, len(in))
	for k, v := range in {
		if k == "" {
			writeError(w, http.StatusBadRequest, "empty key")
			return
		}
		kv[k] = string(v)
	}
	if err := s.store.PutSettings(kv); err != nil {
		s.fail(w, err)
		return
	}
	s.getSettings(w, r)
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	s.logger.Error("request failed", "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
