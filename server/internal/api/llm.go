package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"lifeagent/internal/llm"
	"lifeagent/internal/parse"
)

// parseTimeout bounds one /v1/parse request: up to four model calls.
const parseTimeout = 85 * time.Second

// PhraseParser turns a phrase into a card.
type PhraseParser interface {
	Parse(ctx context.Context, text string, src llm.Source) (parse.Parsed, error)
}

// LLMInfo is what the settings screen needs to know about the models.
type LLMInfo struct {
	Defaults llm.Config
	Catalog  llm.Catalog
	Quota    llm.QuotaProvider // nil: the quota is not shown
}

type parseRequest struct {
	Text string `json:"text"`
}

// postParse: text to card. The client keeps the typed text on any error.
func (s *Server) postParse(w http.ResponseWriter, r *http.Request) {
	if s.parser == nil {
		writeCode(w, http.StatusServiceUnavailable, "llm_not_configured",
			"Разбор фраз не настроен: на сервере нет ключа OpenRouter.")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "body too large")
		return
	}
	var in parseRequest
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, `body must be {"text": "..."}`)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), parseTimeout)
	defer cancel()
	res, err := s.parser.Parse(ctx, in.Text, llm.SourceUser)
	if err != nil {
		s.llmFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) llmFail(w http.ResponseWriter, r *http.Request, err error) {
	var dl *llm.DailyLimitError
	var ex *llm.ExhaustedError
	switch {
	case errors.Is(err, parse.ErrBadText):
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Фраза должна быть от 1 до %d символов.", parse.MaxTextRunes))
	case errors.Is(err, llm.ErrNotConfigured):
		writeCode(w, http.StatusServiceUnavailable, "llm_not_configured",
			"Модель для разбора не выбрана. Выбери её в настройках.")
	case errors.As(err, &dl):
		reset := dl.ResetAt.In(s.loc)
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"code":     "daily_limit",
			"error":    "Дневной лимит бесплатных моделей исчерпан. Сброс в " + reset.Format("15:04") + ".",
			"reset_at": reset.Format(time.RFC3339),
		})
	case errors.Is(err, llm.ErrAuth):
		writeCode(w, http.StatusBadGateway, "llm_auth", "Провайдер отклонил ключ OpenRouter. Проверь ключ на сервере.")
	case errors.As(err, &ex):
		writeCode(w, http.StatusServiceUnavailable, "models_unavailable",
			"Не удалось разобрать фразу: модели не ответили или ответили неверно. Попробуй ещё раз позже.")
	case errors.Is(err, context.DeadlineExceeded):
		writeCode(w, http.StatusGatewayTimeout, "timeout", "Разбор занял слишком много времени. Попробуй ещё раз.")
	case errors.Is(err, context.Canceled):
		// The client went away; nobody reads the answer.
	default:
		s.fail(w, err)
	}
}

func writeCode(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"code": code, "error": msg})
}

type taskView struct {
	Model     string   `json:"model"`
	Effort    string   `json:"effort"`
	Fallbacks []string `json:"fallbacks"`
	// Reasoning: the chosen model takes an effort, so the app shows the control.
	Reasoning bool `json:"reasoning"`
}

type quotaView struct {
	Used      int    `json:"used"`
	Limit     int    `json:"limit"`
	Remaining int    `json:"remaining"`
	ResetAt   string `json:"reset_at"`
}

// getLLM describes the models, the chosen configuration and the quota for the settings screen.
func (s *Server) getLLM(w http.ResponseWriter, r *http.Request) {
	if s.llmInfo == nil {
		writeCode(w, http.StatusServiceUnavailable, "llm_not_configured",
			"Разбор фраз не настроен: на сервере нет ключа OpenRouter.")
		return
	}
	kv, err := s.store.Settings()
	if err != nil {
		s.fail(w, err)
		return
	}
	cfg, err := llm.Effective(s.llmInfo.Defaults, kv)
	if err != nil {
		s.fail(w, err)
		return
	}
	warnings := []string{}
	models, err := s.llmInfo.Catalog.List(r.Context())
	if err != nil {
		warnings = append(warnings, "Список моделей не загрузился. Id модели можно ввести вручную.")
		models = []llm.ModelInfo{}
	}

	tasks := map[llm.Task]taskView{}
	for t, tc := range cfg.Tasks {
		fb := tc.Fallbacks
		if fb == nil {
			fb = []string{}
		}
		tasks[t] = taskView{Model: tc.Model, Effort: tc.Effort, Fallbacks: fb,
			Reasoning: tc.Model != "" && s.llmInfo.Catalog.Info(r.Context(), tc.Model).Reasoning}
	}

	var quota *quotaView
	if s.llmInfo.Quota != nil {
		if q, err := s.llmInfo.Quota.Quota(r.Context()); err == nil {
			quota = &quotaView{Used: q.Used, Limit: q.Limit, Remaining: q.Remaining,
				ResetAt: q.ResetAt.In(s.loc).Format(time.RFC3339)}
		} else {
			warnings = append(warnings, "Остаток квоты не удалось узнать.")
		}
	}
	recent, err := s.store.RecentLLMCalls(20)
	if err != nil {
		s.fail(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"tasks":        tasks,
		"models":       models,
		"quota":        quota,
		"max_fallback": llm.MaxFallbacks,
		"recent_calls": orEmpty(recent),
		"warnings":     warnings,
	})
}

func orEmpty[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}
