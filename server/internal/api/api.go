package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"lifeagent/internal/calendar"
	"lifeagent/internal/llm"
	"lifeagent/internal/load"
	"lifeagent/internal/plan"
	"lifeagent/internal/store"
)

const maxBody = 64 << 10

// Collector gathers the events of all calendar sources for a time window.
type Collector interface {
	Collect(ctx context.Context, from, to time.Time) ([]calendar.Event, []string)
}

// EventWriter creates events in the calendar the agent writes to.
type EventWriter interface {
	Insert(ctx context.Context, e calendar.NewEvent) (created bool, err error)
}

type Server struct {
	store  *store.Store
	token  string
	logger *slog.Logger
	limit  *limiter
	cal    Collector
	writer EventWriter
	loc    *time.Location
	now    func() time.Time

	parser  PhraseParser
	llmInfo *LLMInfo
}

type Options struct {
	Store    *store.Store
	Token    string
	Logger   *slog.Logger
	Calendar Collector   // nil: /v1/week reports calendars as not configured
	Writer   EventWriter // nil: /v1/events reports the write calendar as not configured
	Location *time.Location
	Now      func() time.Time // defaults to time.Now
	Parser   PhraseParser     // nil: /v1/parse reports the LLM as not configured
	LLM      *LLMInfo         // nil: /v1/llm reports the LLM as not configured
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
		writer: o.Writer,
		loc:    o.Location,
		now:    o.Now,

		parser:  o.Parser,
		llmInfo: o.LLM,
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
	v1.HandleFunc("POST /v1/checkin", s.postCheckin)
	v1.HandleFunc("POST /v1/parse", s.postParse)
	v1.HandleFunc("GET /v1/llm", s.getLLM)
	v1.HandleFunc("POST /v1/check", s.postCheck)
	v1.HandleFunc("POST /v1/events", s.postEvents)
	v1.HandleFunc("POST /v1/items", s.postItems)
	v1.HandleFunc("POST /v1/health/daily", s.postHealthDaily)
	v1.HandleFunc("DELETE /v1/health", s.deleteHealth)
	v1.HandleFunc("GET /v1/items", s.getItems)
	v1.HandleFunc("PATCH /v1/items/{id}", s.patchItem)
	v1.HandleFunc("POST /v1/plan/week", s.postPlanWeek)
	v1.HandleFunc("POST /v1/plan/week/apply", s.postPlanApply)
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
		if err := validateSetting(k, v); err != nil {
			writeError(w, http.StatusBadRequest, k+": "+err.Error())
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

// validateSetting rejects values that would break the load rules; other keys are free-form.
func validateSetting(key string, raw json.RawMessage) error {
	var err error
	switch key {
	case settingThresholds:
		_, err = load.ParseThresholds(raw)
	case settingCalendarCategories:
		_, err = load.ParseCalendarCategories(raw)
	case settingPlanParams:
		_, err = plan.ParseParams(raw)
	case llm.SettingKey:
		over, perr := llm.ParseOverrides(raw)
		if perr != nil {
			return perr
		}
		// The overrides must also make a valid chain on top of the defaults.
		var def llm.Config
		if def, err = llm.DefaultConfig(); err == nil {
			_, err = def.Merge(over)
		}
	}
	return err
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
