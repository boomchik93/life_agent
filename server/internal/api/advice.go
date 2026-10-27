package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"lifeagent/internal/advice"
	"lifeagent/internal/calendar"
	"lifeagent/internal/llm"
	"lifeagent/internal/load"
	"lifeagent/internal/store"
)

const (
	settingAdvice = "advice"
	adviceTimeout = 85 * time.Second
	historyLimit  = 50
)

// AdviceGenerator words an advice for a summary of the week.
type AdviceGenerator interface {
	Generate(ctx context.Context, in advice.Input, src llm.Source) (advice.Result, error)
}

func (s *Server) adviceSettings() (advice.Settings, error) {
	kv, err := s.store.Settings()
	if err != nil {
		return advice.Settings{}, err
	}
	if raw, ok := kv[settingAdvice]; ok {
		return advice.ParseSettings([]byte(raw))
	}
	return advice.DefaultSettings(), nil
}

// weekSummary reads the current week and reduces it to what the advice needs.
func (s *Server) weekSummary(ctx context.Context) (events []calendar.Event, cats load.CalendarCategories, a load.Assessment, err error) {
	from, to := calendar.WeekBounds(s.now(), s.loc)
	var warnings []string
	events, warnings = s.cal.Collect(ctx, from, to)
	if len(warnings) > 0 && len(events) == 0 {
		return nil, nil, a, errCalendars
	}
	_, cats, err = s.loadSettings()
	if err != nil {
		return nil, nil, a, err
	}
	a, err = s.assess(events, from, to)
	return events, cats, a, err
}

// adviceInput builds the model's view of the week. Sleep and check-in numbers
// join it only when the user allowed that.
func (s *Server) adviceInput(events []calendar.Event, cats load.CalendarCategories, a load.Assessment, reasons []string, set advice.Settings) (advice.Input, int) {
	in := advice.Input{
		Reasons:         reasons,
		HoursByCategory: map[load.Category]float64{},
		Colors:          map[string]load.Color{"overload": a.Overload.Color, "no_rest": a.NoRest.Color},
	}
	timed := 0
	for _, e := range events {
		if e.AllDay || !e.End.After(e.Start) {
			continue
		}
		timed++
		in.HoursByCategory[eventCategory(cats, e)] += e.End.Sub(e.Start).Hours()
	}
	if set.IncludePrivate {
		p := &advice.Private{}
		if v, ok := a.Sleep.Metrics["avg_sleep_hours"]; ok {
			p.AvgSleepHours = &v
		}
		if v, ok := a.State.Metrics["avg_score"]; ok {
			p.AvgStateScore = &v
		}
		in.Private = p
	}
	return in, timed
}

func (s *Server) saveAdvice(res advice.Result, reasons []string, source, status string) (store.Advice, error) {
	return s.store.AddAdvice(store.Advice{
		At: s.now().UTC().Format(time.RFC3339), Reason: strings.Join(reasons, ","),
		Text: res.Text, Model: res.Model, Source: source, Status: status,
	})
}

// postAdvice gives an advice on request. It ignores the interval and the
// reasons of the automatic one: the user asked.
func (s *Server) postAdvice(w http.ResponseWriter, r *http.Request) {
	if s.advisor == nil {
		writeCode(w, http.StatusServiceUnavailable, "llm_not_configured",
			"Совет не настроен: на сервере нет ключа OpenRouter.")
		return
	}
	if s.cal == nil {
		writeError(w, http.StatusServiceUnavailable, "calendars are not configured")
		return
	}
	set, err := s.adviceSettings()
	if err != nil {
		s.fail(w, err)
		return
	}
	events, cats, a, err := s.weekSummary(r.Context())
	if errors.Is(err, errCalendars) {
		writeCode(w, http.StatusBadGateway, "calendars_unavailable",
			"Не удалось прочитать календари, поэтому совет дать нельзя. Попробуй позже.")
		return
	} else if err != nil {
		s.fail(w, err)
		return
	}
	reasons := []string{advice.ReasonAsked}
	if d := advice.Decide(s.now(), nil, set, a, countTimed(events)); d.Due {
		reasons = append(reasons, d.Reasons...)
	}
	in, _ := s.adviceInput(events, cats, a, reasons, set)

	ctx, cancel := context.WithTimeout(r.Context(), adviceTimeout)
	defer cancel()
	res, err := s.advisor.Generate(ctx, in, llm.SourceUser)
	if err != nil {
		s.llmFailFor(w, r, err, adviceWords)
		return
	}
	saved, err := s.saveAdvice(res, reasons, "user", "delivered")
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func countTimed(events []calendar.Event) int {
	n := 0
	for _, e := range events {
		if !e.AllDay && e.End.After(e.Start) {
			n++
		}
	}
	return n
}

// PrepareAdvice makes an automatic advice when the rules of PLAN.md section 5
// say one is due. It returns nil when nothing was made. The phone fetches the
// result with GET /v1/advice/pending.
func (s *Server) PrepareAdvice(ctx context.Context) (*store.Advice, error) {
	if s.advisor == nil || s.cal == nil {
		return nil, nil
	}
	if pending, err := s.store.HasPendingAdvice(); err != nil || pending {
		return nil, err
	}
	set, err := s.adviceSettings()
	if err != nil {
		return nil, err
	}
	last, err := s.store.LastAdviceAt()
	if err != nil {
		return nil, err
	}
	events, cats, a, err := s.weekSummary(ctx)
	if err != nil {
		return nil, err
	}
	d := advice.Decide(s.now(), last, set, a, countTimed(events))
	if !d.Due {
		return nil, nil
	}
	// The free-model quota belongs to phrase parsing first.
	if s.llmInfo == nil || s.llmInfo.Quota == nil {
		return nil, nil
	}
	q, err := s.llmInfo.Quota.Quota(ctx)
	if err != nil || q.Remaining < set.MinQuota {
		return nil, err
	}
	in, _ := s.adviceInput(events, cats, a, d.Reasons, set)
	ctx, cancel := context.WithTimeout(ctx, adviceTimeout)
	defer cancel()
	res, err := s.advisor.Generate(ctx, in, llm.SourceAuto)
	if err != nil {
		var dl *llm.DailyLimitError
		var ex *llm.ExhaustedError
		if errors.As(err, &dl) || errors.As(err, &ex) || errors.Is(err, llm.ErrNotConfigured) {
			return nil, nil // try again at the next round
		}
		return nil, err
	}
	saved, err := s.saveAdvice(res, d.Reasons, "auto", "pending")
	if err != nil {
		return nil, err
	}
	return &saved, nil
}

// getAdvicePending hands over the prepared advice once.
func (s *Server) getAdvicePending(w http.ResponseWriter, r *http.Request) {
	a, ok, err := s.store.TakePendingAdvice()
	if err != nil {
		s.fail(w, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"advice": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"advice": a})
}

// getAdvice is the history, newest first.
func (s *Server) getAdvice(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.Advices(historyLimit)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"advice": list})
}
