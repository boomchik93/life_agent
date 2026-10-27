package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func serve(t *testing.T, h http.HandlerFunc) *OpenAI {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	p := NewOpenAI(srv.URL+"/", "sk-secret", 2*time.Second)
	p.Now = func() time.Time { return time.Date(2026, 10, 27, 15, 0, 0, 0, time.UTC) }
	return p
}

func kindOf(t *testing.T, err error) ErrKind {
	t.Helper()
	var pe *ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("not a ProviderError: %v", err)
	}
	return pe.Kind
}

func TestChatRequestShape(t *testing.T) {
	var gotAuth, gotPath string
	var body map[string]any
	p := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &body)
		io.WriteString(w, `{"choices":[{"message":{"content":"{\"a\":1}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":11,"completion_tokens":5}}`)
	})
	resp, err := p.Chat(context.Background(), ChatRequest{
		Model: "a/m:free", MaxTokens: 300, Effort: "medium", Format: FormatJSONSchema,
		Schema: json.RawMessage(`{"type":"object"}`), SchemaName: "parse",
		Messages: []Message{{Role: "system", Content: "s"}, {Role: "user", Content: "u"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != `{"a":1}` || *resp.PromptTokens != 11 || *resp.CompletionTokens != 5 {
		t.Errorf("%+v", resp)
	}
	if gotAuth != "Bearer sk-secret" || gotPath != "/chat/completions" {
		t.Errorf("auth %q path %q", gotAuth, gotPath)
	}
	if body["model"] != "a/m:free" || body["max_tokens"] != float64(300) || body["temperature"] != float64(0) {
		t.Errorf("body %v", body)
	}
	if r, _ := body["reasoning"].(map[string]any); r["effort"] != "medium" {
		t.Errorf("reasoning %v", body["reasoning"])
	}
	rf, _ := body["response_format"].(map[string]any)
	js, _ := rf["json_schema"].(map[string]any)
	if rf["type"] != "json_schema" || js["name"] != "parse" || js["strict"] != true || js["schema"] == nil {
		t.Errorf("response_format %v", rf)
	}
}

func TestChatOmitsOptionalParameters(t *testing.T) {
	var body map[string]any
	p := serve(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &body)
		io.WriteString(w, `{"choices":[{"message":{"content":"{}"}}]}`)
	})
	resp, err := p.Chat(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "u"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := body["reasoning"]; ok {
		t.Error("reasoning sent without an effort")
	}
	if _, ok := body["response_format"]; ok {
		t.Error("response_format sent without a format")
	}
	if resp.PromptTokens != nil {
		t.Error("no usage in the reply means no token counts")
	}

	p2 := serve(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &body)
		io.WriteString(w, `{"choices":[{"message":{"content":"{}"}}]}`)
	})
	p2.Chat(context.Background(), ChatRequest{Model: "m", Format: FormatJSONObject})
	if rf, _ := body["response_format"].(map[string]any); rf["type"] != "json_object" {
		t.Errorf("json_object: %v", body["response_format"])
	}
}

func TestChatErrorMapping(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   ErrKind
	}{
		{"429", 429, `{"error":{"code":429,"message":"Rate limit exceeded"}}`, KindLimit},
		{"401", 401, `{"error":{"code":401,"message":"bad key"}}`, KindAuth},
		{"402", 402, `{"error":{"code":402}}`, KindUnavailable},
		{"403", 403, `{}`, KindUnavailable},
		{"404 no endpoints", 404, `{"error":{"message":"No endpoints found matching your data policy"}}`, KindUnavailable},
		{"408", 408, `{}`, KindTimeout},
		{"400", 400, `{"error":{"message":"bad param"}}`, KindBadRequest},
		{"502", 502, `{}`, KindUnavailable},
		{"503", 503, `not json`, KindUnavailable},
		{"error inside 200", 200, `{"error":{"code":502,"message":"upstream failed"}}`, KindUnavailable},
		{"429 inside 200", 200, `{"error":{"code":429,"message":"slow down"}}`, KindLimit},
		{"200 without choices", 200, `{"choices":[]}`, KindUnavailable},
		{"200 with garbage", 200, `<html>`, KindUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := serve(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				io.WriteString(w, tt.body)
			})
			_, err := p.Chat(context.Background(), ChatRequest{Model: "m"})
			if got := kindOf(t, err); got != tt.want {
				t.Fatalf("kind %d, want %d", got, tt.want)
			}
			if strings.Contains(err.Error(), "sk-secret") {
				t.Error("the key leaked into the error")
			}
		})
	}
}

func TestChatTimeout(t *testing.T) {
	release := make(chan struct{})
	p := serve(t, func(w http.ResponseWriter, r *http.Request) { <-release })
	defer close(release)
	p.Client.Timeout = 50 * time.Millisecond
	_, err := p.Chat(context.Background(), ChatRequest{Model: "m"})
	if kindOf(t, err) != KindTimeout {
		t.Fatalf("err %v", err)
	}
}

func TestChatCallerCancellationIsNotAProviderError(t *testing.T) {
	release := make(chan struct{})
	p := serve(t, func(w http.ResponseWriter, r *http.Request) { <-release })
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := p.Chat(ctx, ChatRequest{Model: "m"})
	var pe *ProviderError
	if errors.As(err, &pe) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err %v", err)
	}
}

func TestQuota(t *testing.T) {
	var path, auth string
	p := serve(t, func(w http.ResponseWriter, r *http.Request) {
		path, auth = r.URL.Path, r.Header.Get("Authorization")
		io.WriteString(w, `{"data":{"label":"x","free_model_daily_requests":{"used":42,"limit":50,"remaining":8}}}`)
	})
	q, err := p.Quota(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if path != "/key" || auth != "Bearer sk-secret" {
		t.Errorf("path %q auth %q", path, auth)
	}
	if q.Used != 42 || q.Limit != 50 || q.Remaining != 8 {
		t.Errorf("%+v", q)
	}
	if want := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC); !q.ResetAt.Equal(want) {
		t.Errorf("reset %v, want next UTC midnight %v", q.ResetAt, want)
	}

	for _, body := range []string{`{"data":{}}`, `nope`} {
		bad := serve(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) })
		if _, err := bad.Quota(context.Background()); err == nil {
			t.Errorf("accepted %q", body)
		}
	}
	denied := serve(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) })
	if _, err := denied.Quota(context.Background()); kindOf(t, err) != KindAuth {
		t.Errorf("401 must map to KindAuth: %v", err)
	}
}

func TestCatalog(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		io.WriteString(w, `{"data":[
		 {"id":"g/gemma:free","name":"Gemma","context_length":1000,"architecture":{"output_modalities":["text"]},"supported_parameters":["reasoning","structured_outputs","response_format"]},
		 {"id":"n/nemo:free","name":"Nemo","architecture":{"output_modalities":["text"]},"supported_parameters":["reasoning"]},
		 {"id":"p/paid","name":"Paid","architecture":{"output_modalities":["text"]},"supported_parameters":["response_format"]},
		 {"id":"i/image:free","name":"Img","architecture":{"output_modalities":["image"]},"supported_parameters":[]}]}`)
	}))
	defer srv.Close()
	c := NewOpenRouterCatalog(srv.URL)
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	c.Now = func() time.Time { return now }

	list, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != "g/gemma:free" || list[1].ID != "n/nemo:free" {
		t.Fatalf("only free text models, sorted: %+v", list)
	}
	g := c.Info(context.Background(), "g/gemma:free")
	if !g.Reasoning || !g.Structured || !g.JSONMode || !g.Free {
		t.Errorf("%+v", g)
	}
	if n := c.Info(context.Background(), "n/nemo:free"); !n.Reasoning || n.Structured || n.JSONMode {
		t.Errorf("%+v", n)
	}
	if p := c.Info(context.Background(), "p/paid"); p.Free || !p.JSONMode {
		t.Errorf("%+v", p)
	}
	if u := c.Info(context.Background(), "x/unknown:free"); u.Reasoning || u.Structured || !u.Free {
		t.Errorf("unknown model has no capabilities: %+v", u)
	}
	if hits != 1 {
		t.Errorf("cache: %d fetches", hits)
	}

	now = now.Add(2 * time.Hour) // expired: refetch
	c.List(context.Background())
	if hits != 2 {
		t.Errorf("expired cache: %d fetches", hits)
	}

	srv.Close() // provider down: stale copy keeps serving
	now = now.Add(2 * time.Hour)
	if list, err := c.List(context.Background()); err != nil || len(list) != 2 {
		t.Errorf("stale: %v %v", list, err)
	}
	if _, err := NewOpenRouterCatalog(srv.URL).List(context.Background()); err == nil {
		t.Error("nothing cached and provider down must fail")
	}
}
