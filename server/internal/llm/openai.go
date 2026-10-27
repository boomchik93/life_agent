package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// OpenAI is a provider for any OpenAI-compatible chat endpoint; OpenRouter is
// one such configuration. A new provider is a new base URL and key, not new code.
type OpenAI struct {
	BaseURL string
	APIKey  string
	Client  *http.Client
	Now     func() time.Time
}

func NewOpenAI(baseURL, apiKey string, timeout time.Duration) *OpenAI {
	return &OpenAI{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		Client:  &http.Client{Timeout: timeout},
		Now:     time.Now,
	}
}

type chatBody struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	Temperature    float64         `json:"temperature"`
	ResponseFormat json.RawMessage `json:"response_format,omitempty"`
	Reasoning      *reasoning      `json:"reasoning,omitempty"`
}

type reasoning struct {
	Effort string `json:"effort"`
}

type chatReply struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Code    json.RawMessage `json:"code"`
		Message string          `json:"message"`
	} `json:"error"`
}

func (p *OpenAI) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	body := chatBody{Model: req.Model, Messages: req.Messages, MaxTokens: req.MaxTokens}
	switch req.Format {
	case FormatJSONObject:
		body.ResponseFormat = json.RawMessage(`{"type":"json_object"}`)
	case FormatJSONSchema:
		rf, _ := json.Marshal(map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name": req.SchemaName, "strict": true, "schema": json.RawMessage(req.Schema),
			},
		})
		body.ResponseFormat = rf
	}
	if req.Effort != "" {
		body.Reasoning = &reasoning{Effort: req.Effort}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return ChatResponse{}, err
	}

	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return ChatResponse{}, err
	}
	hr.Header.Set("Authorization", "Bearer "+p.APIKey)
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set("X-Title", "life-agent")

	resp, err := p.Client.Do(hr)
	if err != nil {
		return ChatResponse{}, transportError(ctx, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return ChatResponse{}, transportError(ctx, err)
	}

	if resp.StatusCode != http.StatusOK {
		return ChatResponse{}, &ProviderError{Kind: statusKind(resp.StatusCode), Status: resp.StatusCode, Msg: errorMessage(raw)}
	}
	var r chatReply
	if err := json.Unmarshal(raw, &r); err != nil {
		return ChatResponse{}, &ProviderError{Kind: KindUnavailable, Status: 200, Msg: "undecodable body"}
	}
	// A failure after the provider accepted the request still arrives as HTTP 200.
	if r.Error != nil {
		kind := KindUnavailable
		if strings.TrimSpace(string(r.Error.Code)) == "429" {
			kind = KindLimit
		}
		return ChatResponse{}, &ProviderError{Kind: kind, Status: 200, Msg: r.Error.Message}
	}
	if len(r.Choices) == 0 {
		return ChatResponse{}, &ProviderError{Kind: KindUnavailable, Status: 200, Msg: "no choices"}
	}
	out := ChatResponse{Content: r.Choices[0].Message.Content}
	if r.Usage != nil {
		pt, ct := r.Usage.PromptTokens, r.Usage.CompletionTokens
		out.PromptTokens, out.CompletionTokens = &pt, &ct
	}
	return out, nil
}

func statusKind(status int) ErrKind {
	switch {
	case status == http.StatusUnauthorized:
		return KindAuth
	case status == http.StatusTooManyRequests:
		return KindLimit
	case status == http.StatusRequestTimeout || status == http.StatusGatewayTimeout:
		return KindTimeout
	case status == http.StatusBadRequest:
		return KindBadRequest
	}
	return KindUnavailable
}

func transportError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err() // the caller gave up: not the model's fault
	}
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return &ProviderError{Kind: KindTimeout, Msg: "timeout"}
	}
	return &ProviderError{Kind: KindUnavailable, Msg: "transport"}
}

func errorMessage(raw []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &e) == nil {
		return e.Error.Message
	}
	return ""
}

// Quota reads the free-model allowance of the key (OpenRouter: GET /key).
// The allowance resets at 00:00 UTC.
func (p *OpenAI) Quota(ctx context.Context) (Quota, error) {
	hr, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+"/key", nil)
	if err != nil {
		return Quota{}, err
	}
	hr.Header.Set("Authorization", "Bearer "+p.APIKey)
	resp, err := p.Client.Do(hr)
	if err != nil {
		return Quota{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Quota{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return Quota{}, &ProviderError{Kind: statusKind(resp.StatusCode), Status: resp.StatusCode}
	}
	var k struct {
		Data struct {
			Free *struct {
				Used      int `json:"used"`
				Limit     int `json:"limit"`
				Remaining int `json:"remaining"`
			} `json:"free_model_daily_requests"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &k); err != nil || k.Data.Free == nil {
		return Quota{}, errors.New("llm: key info has no free_model_daily_requests")
	}
	now := p.Now().UTC()
	reset := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	f := k.Data.Free
	return Quota{Used: f.Used, Limit: f.Limit, Remaining: f.Remaining, ResetAt: reset}, nil
}
