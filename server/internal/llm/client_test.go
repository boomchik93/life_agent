package llm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"lifeagent/internal/store"
)

type step struct {
	content string
	err     error
}

// stub answers each call with the next step of a script and records the requests.
type stub struct {
	script []step
	reqs   []ChatRequest
}

func (s *stub) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	if err := ctx.Err(); err != nil {
		return ChatResponse{}, err
	}
	s.reqs = append(s.reqs, req)
	if len(s.reqs) > len(s.script) {
		panic("stub: more calls than scripted")
	}
	st := s.script[len(s.reqs)-1]
	if st.err != nil {
		return ChatResponse{}, st.err
	}
	n := 7
	return ChatResponse{Content: st.content, PromptTokens: &n, CompletionTokens: &n}, nil
}

type fakeQuota struct {
	q     Quota
	err   error
	calls int
}

func (f *fakeQuota) Quota(context.Context) (Quota, error) { f.calls++; return f.q, f.err }

type memJournal struct {
	mu    sync.Mutex
	calls []store.LLMCall
}

func (j *memJournal) AddLLMCall(c store.LLMCall) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.calls = append(j.calls, c)
	return nil
}

type fakeCatalog map[string]ModelInfo

func (f fakeCatalog) List(context.Context) ([]ModelInfo, error) { return nil, nil }
func (f fakeCatalog) Info(_ context.Context, id string) ModelInfo {
	if m, ok := f[id]; ok {
		return m
	}
	return ModelInfo{ID: id, Free: strings.HasSuffix(id, ":free")}
}

const good = `{"ok":true}`

var (
	limit429 = &ProviderError{Kind: KindLimit, Status: 429}
	timeout  = &ProviderError{Kind: KindTimeout}
	outage   = &ProviderError{Kind: KindUnavailable, Status: 502}
)

func newClient(s *stub, chain ...string) (*Client, *memJournal, *fakeQuota) {
	j := &memJournal{}
	q := &fakeQuota{q: Quota{Limit: 50, Remaining: 30, ResetAt: time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)}}
	cfg, _ := DefaultConfig()
	cfg.Tasks[TaskParse] = TaskConfig{Model: chain[0], Effort: "low", Fallbacks: chain[1:], MaxTokens: 500}
	return &Client{
		Provider: s, ProviderName: "openrouter", Quota: q, Journal: j,
		Catalog: fakeCatalog{},
		Config:  func() (Config, error) { return cfg, nil },
	}, j, q
}

func in(check func(string) error) Input {
	return Input{Task: TaskParse, Source: SourceUser, System: "sys", User: "usr", Check: check}
}

func results(j *memJournal) []string {
	var out []string
	for _, c := range j.calls {
		out = append(out, c.Result)
	}
	return out
}

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestOKFirstTry(t *testing.T) {
	s := &stub{script: []step{{content: good}}}
	c, j, _ := newClient(s, "a/m:free")
	out, err := c.Call(context.Background(), in(nil))
	if err != nil {
		t.Fatal(err)
	}
	if out.JSON != good || out.Model != "a/m:free" || out.Calls != 1 {
		t.Fatalf("%+v", out)
	}
	eq(t, results(j), []string{"ok"})
	row := j.calls[0]
	if row.Task != "parse" || row.Provider != "openrouter" || row.Source != "user" || row.Attempt != 1 || row.ChainPos != 0 ||
		row.PromptTokens == nil || *row.PromptTokens != 7 {
		t.Fatalf("journal row %+v", row)
	}
}

func TestInvalidJSONRetriesOnceWithTheErrorText(t *testing.T) {
	s := &stub{script: []step{{content: "sorry, no json"}, {content: good}}}
	c, j, _ := newClient(s, "a/m:free", "b/m:free")
	out, err := c.Call(context.Background(), in(nil))
	if err != nil || out.Model != "a/m:free" || out.Calls != 2 {
		t.Fatalf("%+v %v", out, err)
	}
	eq(t, results(j), []string{"invalid_json", "ok"})
	if j.calls[1].Attempt != 2 || j.calls[1].ChainPos != 0 {
		t.Errorf("retry stays on the same model: %+v", j.calls[1])
	}
	msgs := s.reqs[1].Messages
	if len(msgs) != 4 || msgs[2].Role != "assistant" || msgs[2].Content != "sorry, no json" ||
		!strings.Contains(msgs[3].Content, "не прошёл проверку") {
		t.Fatalf("retry prompt %+v", msgs)
	}
}

func TestInvalidTwiceMovesToTheNextModel(t *testing.T) {
	s := &stub{script: []step{{content: "x"}, {content: "y"}, {content: good}}}
	c, j, _ := newClient(s, "a/m:free", "b/m:free")
	out, err := c.Call(context.Background(), in(nil))
	if err != nil || out.Model != "b/m:free" || out.Calls != 3 {
		t.Fatalf("%+v %v", out, err)
	}
	eq(t, results(j), []string{"invalid_json", "invalid_json", "ok"})
	if j.calls[2].ChainPos != 1 || j.calls[2].Attempt != 1 {
		t.Errorf("%+v", j.calls[2])
	}
}

func TestSchemaCheckInvalidRetriesFailedDoesNot(t *testing.T) {
	check := func(raw string) error {
		if strings.Contains(raw, "schema") {
			return Invalid(errors.New("поле kind обязательно"))
		}
		if strings.Contains(raw, "code") {
			return Failed(errors.New("RRULE не разобран"))
		}
		return nil
	}
	// Invalid: retried on the same model, the error text reaches the model.
	s := &stub{script: []step{{content: `{"schema":1}`}, {content: good}}}
	c, j, _ := newClient(s, "a/m:free")
	if _, err := c.Call(context.Background(), in(check)); err != nil {
		t.Fatal(err)
	}
	eq(t, results(j), []string{"invalid_json", "ok"})
	if !strings.Contains(s.reqs[1].Messages[3].Content, "поле kind обязательно") {
		t.Errorf("error text missing: %q", s.reqs[1].Messages[3].Content)
	}

	// Failed code check: straight to the next model, no retry.
	s = &stub{script: []step{{content: `{"code":1}`}, {content: good}}}
	c, j, _ = newClient(s, "a/m:free", "b/m:free")
	out, err := c.Call(context.Background(), in(check))
	if err != nil || out.Model != "b/m:free" {
		t.Fatalf("%+v %v", out, err)
	}
	eq(t, results(j), []string{"check_failed", "ok"})
	if j.calls[1].Attempt != 1 || j.calls[1].ChainPos != 1 {
		t.Errorf("%+v", j.calls[1])
	}
}

func TestModelLimitMovesOnWithoutRetry(t *testing.T) {
	s := &stub{script: []step{{err: limit429}, {content: good}}}
	c, j, q := newClient(s, "a/m:free", "b/m:free")
	out, err := c.Call(context.Background(), in(nil))
	if err != nil || out.Model != "b/m:free" || out.Calls != 2 {
		t.Fatalf("%+v %v", out, err)
	}
	eq(t, results(j), []string{"limit", "ok"})
	if q.calls != 1 {
		t.Errorf("quota is checked once to tell the two limits apart, got %d", q.calls)
	}
	if j.calls[0].PromptTokens != nil {
		t.Error("a failed call has no token counts")
	}
}

func TestDailyLimitStopsTheChain(t *testing.T) {
	s := &stub{script: []step{{err: limit429}}} // a second call would panic
	c, j, q := newClient(s, "a/m:free", "b/m:free", "c/m:free")
	q.q = Quota{Limit: 50, Used: 50, Remaining: 0, ResetAt: time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)}
	_, err := c.Call(context.Background(), in(nil))
	var dl *DailyLimitError
	if !errors.As(err, &dl) || !dl.ResetAt.Equal(q.q.ResetAt) {
		t.Fatalf("err %v", err)
	}
	if len(s.reqs) != 1 {
		t.Fatalf("calls %d, the chain must not be walked", len(s.reqs))
	}
	eq(t, results(j), []string{"limit"})
}

func TestQuotaIsNotConsultedForPaidModels(t *testing.T) {
	s := &stub{script: []step{{err: limit429}, {content: good}}}
	c, _, q := newClient(s, "a/paid", "b/m:free")
	q.q.Remaining = 0
	out, err := c.Call(context.Background(), in(nil))
	if err != nil || out.Model != "b/m:free" || q.calls != 0 {
		t.Fatalf("%+v %v quota calls %d", out, err, q.calls)
	}
}

func TestQuotaFailureFallsBackToTheNextModel(t *testing.T) {
	s := &stub{script: []step{{err: limit429}, {content: good}}}
	c, _, q := newClient(s, "a/m:free", "b/m:free")
	q.err = errors.New("network")
	if out, err := c.Call(context.Background(), in(nil)); err != nil || out.Model != "b/m:free" {
		t.Fatalf("%+v %v", out, err)
	}
	c.Quota = nil
	s.script, s.reqs = []step{{err: limit429}, {content: good}}, nil
	if out, err := c.Call(context.Background(), in(nil)); err != nil || out.Model != "b/m:free" {
		t.Fatalf("no quota source: %+v %v", out, err)
	}
}

func TestTimeoutAndOutageMoveOn(t *testing.T) {
	s := &stub{script: []step{{err: timeout}, {err: outage}, {content: good}}}
	c, j, _ := newClient(s, "a/m:free", "b/m:free", "c/m:free")
	out, err := c.Call(context.Background(), in(nil))
	if err != nil || out.Model != "c/m:free" {
		t.Fatalf("%+v %v", out, err)
	}
	eq(t, results(j), []string{"timeout", "error", "ok"})
}

func TestChainExhausted(t *testing.T) {
	s := &stub{script: []step{{err: limit429}, {err: timeout}}}
	c, j, _ := newClient(s, "a/m:free", "b/m:free")
	_, err := c.Call(context.Background(), in(nil))
	var ex *ExhaustedError
	if !errors.As(err, &ex) || ex.Calls != 2 || ex.Last != "timeout" || ex.Budget {
		t.Fatalf("err %v", err)
	}
	eq(t, results(j), []string{"limit", "timeout"})
}

func TestCallCeilingPerRequest(t *testing.T) {
	// Three models, every answer invalid: 2 + 2 calls hit the ceiling of 4; the third model is never asked.
	s := &stub{script: []step{{content: "x"}, {content: "x"}, {content: "x"}, {content: "x"}}}
	c, j, _ := newClient(s, "a/m:free", "b/m:free", "c/m:free")
	_, err := c.Call(context.Background(), in(nil))
	var ex *ExhaustedError
	if !errors.As(err, &ex) || !ex.Budget || ex.Calls != 4 {
		t.Fatalf("err %v", err)
	}
	if len(s.reqs) != 4 || len(j.calls) != 4 {
		t.Fatalf("requests %d, journal %d", len(s.reqs), len(j.calls))
	}
}

func TestAuthErrorStopsEverything(t *testing.T) {
	s := &stub{script: []step{{err: &ProviderError{Kind: KindAuth, Status: 401}}}}
	c, _, _ := newClient(s, "a/m:free", "b/m:free")
	if _, err := c.Call(context.Background(), in(nil)); !errors.Is(err, ErrAuth) {
		t.Fatalf("err %v", err)
	}
	if len(s.reqs) != 1 {
		t.Errorf("calls %d", len(s.reqs))
	}
}

func TestNotConfigured(t *testing.T) {
	s := &stub{}
	c, _, _ := newClient(s, "")
	if _, err := c.Call(context.Background(), in(nil)); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err %v", err)
	}
	if _, err := c.Call(context.Background(), Input{Task: TaskAdvice}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("advice has no model either: %v", err)
	}
}

func TestCallerCancellationIsNotAModelFailure(t *testing.T) {
	s := &stub{script: []step{{content: good}}}
	c, j, _ := newClient(s, "a/m:free", "b/m:free")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Call(ctx, in(nil)); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	if len(j.calls) != 0 {
		t.Errorf("journal %v", j.calls)
	}
}

func TestConfigChangesApplyWithoutRestart(t *testing.T) {
	s := &stub{script: []step{{content: good}, {content: good}}}
	c, _, _ := newClient(s, "a/m:free")
	if _, err := c.Call(context.Background(), in(nil)); err != nil {
		t.Fatal(err)
	}
	cfg, _ := DefaultConfig()
	cfg.Tasks[TaskParse] = TaskConfig{Model: "z/new:free", Effort: "high", MaxTokens: 100}
	c.Config = func() (Config, error) { return cfg, nil }
	if _, err := c.Call(context.Background(), in(nil)); err != nil {
		t.Fatal(err)
	}
	if s.reqs[0].Model != "a/m:free" || s.reqs[1].Model != "z/new:free" {
		t.Fatalf("models %q then %q", s.reqs[0].Model, s.reqs[1].Model)
	}
}

func TestEffortAndFormatFollowTheModelCapabilities(t *testing.T) {
	schema := json.RawMessage(`{"type":"object"}`)
	run := func(info ModelInfo) ChatRequest {
		s := &stub{script: []step{{content: good}}}
		c, _, _ := newClient(s, "a/m:free")
		c.Catalog = fakeCatalog{"a/m:free": info}
		input := in(nil)
		input.Schema, input.SchemaName = schema, "parse"
		if _, err := c.Call(context.Background(), input); err != nil {
			t.Fatal(err)
		}
		return s.reqs[0]
	}
	plain := run(ModelInfo{})
	if plain.Effort != "" || plain.Format != FormatNone || plain.MaxTokens != 500 {
		t.Errorf("plain model: %+v", plain)
	}
	if r := run(ModelInfo{Reasoning: true}); r.Effort != "low" {
		t.Errorf("reasoning model must get the effort: %+v", r)
	}
	if r := run(ModelInfo{JSONMode: true}); r.Format != FormatJSONObject {
		t.Errorf("json mode: %+v", r)
	}
	if r := run(ModelInfo{JSONMode: true, Structured: true}); r.Format != FormatJSONSchema || string(r.Schema) != string(schema) {
		t.Errorf("structured: %+v", r)
	}
}

func TestSourceIsJournaled(t *testing.T) {
	s := &stub{script: []step{{content: good}}}
	c, j, _ := newClient(s, "a/m:free")
	input := in(nil)
	input.Source = SourceEval
	if _, err := c.Call(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if j.calls[0].Source != "eval" {
		t.Errorf("%+v", j.calls[0])
	}
}

func TestJournalNeverHoldsTexts(t *testing.T) {
	s := &stub{script: []step{{content: `{"secret":"SECRET-ANSWER"}`}}}
	c, j, _ := newClient(s, "a/m:free")
	input := in(nil)
	input.User = "SECRET-PHRASE"
	if _, err := c.Call(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	dump, _ := json.Marshal(j.calls)
	if strings.Contains(string(dump), "SECRET") {
		t.Fatalf("texts leaked into the journal: %s", dump)
	}
}

func TestExtractJSON(t *testing.T) {
	tests := map[string]string{
		`{"a":1}`:                 `{"a":1}`,
		"  {\"a\":1}\n":           `{"a":1}`,
		"```json\n{\"a\":1}\n```": `{"a":1}`,
		"```\n{\"a\":1}\n```":     `{"a":1}`,
		"Вот ответ: {\"a\":{\"b\":2}} готово": `{"a":{"b":2}}`,
		"нет json": "нет json",
		"":         "",
	}
	for in, want := range tests {
		if got := ExtractJSON(in); got != want {
			t.Errorf("ExtractJSON(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCheckJSONRejectsNonObjects(t *testing.T) {
	for _, bad := range []string{"", "nope", "[1]", "null", "42", `"x"`} {
		if checkJSON(ExtractJSON(bad)) == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}
