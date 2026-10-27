package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lifeagent/internal/llm"
	"lifeagent/internal/parse"
	"lifeagent/internal/store"
)

type fakeParser struct {
	res  parse.Parsed
	err  error
	text string
	src  llm.Source
}

func (f *fakeParser) Parse(_ context.Context, text string, src llm.Source) (parse.Parsed, error) {
	f.text, f.src = text, src
	return f.res, f.err
}

type fakeCatalog struct{ err error }

func (f fakeCatalog) List(context.Context) ([]llm.ModelInfo, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []llm.ModelInfo{{ID: "a/m:free", Name: "A", Free: true, Reasoning: true}, {ID: "b/m:free", Name: "B", Free: true}}, nil
}

func (f fakeCatalog) Info(_ context.Context, id string) llm.ModelInfo {
	return llm.ModelInfo{ID: id, Free: true, Reasoning: id == "a/m:free"}
}

type fakeQuota struct {
	q   llm.Quota
	err error
}

func (f fakeQuota) Quota(context.Context) (llm.Quota, error) { return f.q, f.err }

func llmServer(t *testing.T, p PhraseParser, info *LLMInfo) (http.Handler, *store.Store) {
	t.Helper()
	loc, _ := time.LoadLocation("Asia/Yekaterinburg")
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(Options{
		Store: st, Token: testToken, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		RatePerSec: 100, Burst: 100, Location: loc, Parser: p, LLM: info,
		Now: func() time.Time { return time.Date(2026, 10, 28, 12, 0, 0, 0, loc) },
	}), st
}

func decode(t *testing.T, body string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	return m
}

func TestParseReturnsTheCard(t *testing.T) {
	start := "2026-10-30T18:00:00+05:00"
	fp := &fakeParser{res: parse.Parsed{
		Result: parse.Result{Kind: "event", Title: "КВН", CategoryGuess: "hobby", Start: &start, Missing: []string{}, Problems: []parse.Problem{}},
		Model:  "a/m:free", Calls: 2,
	}}
	h, _ := llmServer(t, fp, nil)
	rec := do(h, "POST", "/v1/parse", "Bearer "+testToken, `{"text":"КВН в пятницу с 18 до 20"}`)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	m := decode(t, rec.Body.String())
	if m["kind"] != "event" || m["title"] != "КВН" || m["start"] != start || m["model"] != "a/m:free" || m["calls"] != float64(2) {
		t.Errorf("%v", m)
	}
	if !strings.Contains(rec.Body.String(), `"missing":[]`) || !strings.Contains(rec.Body.String(), `"problems":[]`) {
		t.Errorf("empty lists must not be null: %s", rec.Body)
	}
	if fp.text != "КВН в пятницу с 18 до 20" || fp.src != llm.SourceUser {
		t.Errorf("parser got %q %v", fp.text, fp.src)
	}
}

func TestParseErrors(t *testing.T) {
	reset := time.Date(2026, 10, 28, 19, 0, 0, 0, time.UTC) // 00:00 on the 29th in Yekaterinburg
	tests := []struct {
		name   string
		err    error
		status int
		code   string
		inMsg  string
	}{
		{"bad text", parse.ErrBadText, 400, "", "от 1 до 500"},
		{"no model", llm.ErrNotConfigured, 503, "llm_not_configured", "Выбери её в настройках"},
		{"daily limit", &llm.DailyLimitError{ResetAt: reset}, 429, "daily_limit", "Сброс в 00:00"},
		{"chain exhausted", &llm.ExhaustedError{Calls: 3, Last: "limit"}, 503, "models_unavailable", "Попробуй"},
		{"call budget", &llm.ExhaustedError{Calls: 4, Budget: true}, 503, "models_unavailable", ""},
		{"bad key", llm.ErrAuth, 502, "llm_auth", "ключ"},
		{"deadline", context.DeadlineExceeded, 504, "timeout", "слишком много"},
		{"unknown", errors.New("boom with secret details"), 500, "", "internal error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, _ := llmServer(t, &fakeParser{err: tt.err}, nil)
			rec := do(h, "POST", "/v1/parse", "Bearer "+testToken, `{"text":"x"}`)
			if rec.Code != tt.status {
				t.Fatalf("status %d, want %d: %s", rec.Code, tt.status, rec.Body)
			}
			m := decode(t, rec.Body.String())
			if tt.code != "" && m["code"] != tt.code {
				t.Errorf("code %v, want %s", m["code"], tt.code)
			}
			if msg, _ := m["error"].(string); !strings.Contains(msg, tt.inMsg) || msg == "" {
				t.Errorf("message %q lacks %q", msg, tt.inMsg)
			}
			if strings.Contains(rec.Body.String(), "secret details") {
				t.Error("an internal error text leaked to the client")
			}
			if tt.code == "daily_limit" && m["reset_at"] != "2026-10-29T00:00:00+05:00" {
				t.Errorf("reset_at %v must be in the user's zone", m["reset_at"])
			}
		})
	}
}

func TestParseRequestValidation(t *testing.T) {
	h, _ := llmServer(t, &fakeParser{}, nil)
	for _, body := range []string{`nope`, `[]`, `{"text":5}`} {
		if rec := do(h, "POST", "/v1/parse", "Bearer "+testToken, body); rec.Code != 400 {
			t.Errorf("%s: %d", body, rec.Code)
		}
	}
	if rec := do(h, "POST", "/v1/parse", "", `{"text":"x"}`); rec.Code != 401 {
		t.Errorf("no token: %d", rec.Code)
	}
	if rec := do(h, "GET", "/v1/parse", "Bearer "+testToken, ""); rec.Code != 405 {
		t.Errorf("GET: %d", rec.Code)
	}
}

func TestParseWithoutLLM(t *testing.T) {
	h, _ := llmServer(t, nil, nil)
	rec := do(h, "POST", "/v1/parse", "Bearer "+testToken, `{"text":"x"}`)
	if rec.Code != 503 || decode(t, rec.Body.String())["code"] != "llm_not_configured" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/v1/llm", "Bearer "+testToken, ""); rec.Code != 503 {
		t.Fatalf("/v1/llm: %d", rec.Code)
	}
}

func info(q fakeQuota, cat fakeCatalog) *LLMInfo {
	def, _ := llm.DefaultConfig()
	return &LLMInfo{Defaults: def, Catalog: cat, Quota: q}
}

func TestLLMStatusAndOverrides(t *testing.T) {
	q := fakeQuota{q: llm.Quota{Used: 12, Limit: 50, Remaining: 38, ResetAt: time.Date(2026, 10, 28, 19, 0, 0, 0, time.UTC)}}
	h, st := llmServer(t, &fakeParser{}, info(q, fakeCatalog{}))
	auth := "Bearer " + testToken
	get := func() map[string]any {
		rec := do(h, "GET", "/v1/llm", auth, "")
		if rec.Code != 200 {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		return decode(t, rec.Body.String())
	}

	m := get()
	tasks := m["tasks"].(map[string]any)
	parseTask := tasks["parse"].(map[string]any)
	if parseTask["model"] != "" || parseTask["effort"] != "low" || parseTask["reasoning"] != false {
		t.Errorf("defaults: %v", parseTask)
	}
	if tasks["advice"].(map[string]any)["effort"] != "medium" {
		t.Errorf("advice default effort: %v", tasks["advice"])
	}
	quota := m["quota"].(map[string]any)
	if quota["remaining"] != float64(38) || quota["limit"] != float64(50) || quota["reset_at"] != "2026-10-29T00:00:00+05:00" {
		t.Errorf("quota %v", quota)
	}
	if len(m["models"].([]any)) != 2 || m["max_fallback"] != float64(3) || len(m["recent_calls"].([]any)) != 0 {
		t.Errorf("%v", m)
	}

	// A change made in the app is visible at once, without a restart.
	put := `{"llm_tasks":{"parse":{"model":"a/m:free","effort":"high","fallbacks":["b/m:free"]}}}`
	if rec := do(h, "PUT", "/v1/settings", auth, put); rec.Code != 200 {
		t.Fatalf("put %d %s", rec.Code, rec.Body)
	}
	parseTask = get()["tasks"].(map[string]any)["parse"].(map[string]any)
	if parseTask["model"] != "a/m:free" || parseTask["effort"] != "high" || parseTask["reasoning"] != true ||
		len(parseTask["fallbacks"].([]any)) != 1 {
		t.Errorf("after override: %v", parseTask)
	}

	// The journal shows the calls, without texts.
	st.AddLLMCall(store.LLMCall{Time: time.Now(), Task: "parse", Provider: "openrouter", Model: "a/m:free", Result: "limit", Attempt: 1, Source: "user"})
	recent := get()["recent_calls"].([]any)
	if len(recent) != 1 || recent[0].(map[string]any)["result"] != "limit" {
		t.Errorf("recent %v", recent)
	}
}

func TestLLMStatusSurvivesProviderTroubles(t *testing.T) {
	h, _ := llmServer(t, &fakeParser{}, info(fakeQuota{err: errors.New("down")}, fakeCatalog{err: errors.New("down")}))
	rec := do(h, "GET", "/v1/llm", "Bearer "+testToken, "")
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	m := decode(t, rec.Body.String())
	if m["quota"] != nil || len(m["models"].([]any)) != 0 || len(m["warnings"].([]any)) != 2 {
		t.Errorf("%v", m)
	}
}

func TestSettingsValidatesLLMTasks(t *testing.T) {
	h, _ := llmServer(t, nil, nil)
	auth := "Bearer " + testToken
	for _, bad := range []string{
		`{"llm_tasks":{"parse":{"effort":"max"}}}`,
		`{"llm_tasks":{"parse":{"model":"no-slash"}}}`,
		`{"llm_tasks":{"parse":{"fallbacks":["a/1","b/2","c/3","d/4"]}}}`,
		`{"llm_tasks":{"parse":{"model":"a/1","fallbacks":["a/1"]}}}`,
		`{"llm_tasks":{"chat":{"model":"a/1"}}}`,
		`{"llm_tasks":"x"}`,
	} {
		if rec := do(h, "PUT", "/v1/settings", auth, bad); rec.Code != 400 {
			t.Errorf("%s: %d", bad, rec.Code)
		}
	}
	if rec := do(h, "PUT", "/v1/settings", auth, `{"llm_tasks":{"parse":{"model":"a/1:free","effort":"medium","fallbacks":[]}}}`); rec.Code != 200 {
		t.Errorf("valid: %d %s", rec.Code, rec.Body)
	}
}
