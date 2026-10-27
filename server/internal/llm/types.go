// Package llm is the only way the server talks to a language model. It keeps
// the provider behind an interface, walks the chain of fallback models, retries
// invalid answers once, stops on the account's daily limit, and journals every
// call without storing any text.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Task string

const (
	TaskParse  Task = "parse"
	TaskAdvice Task = "advice"
)

func (t Task) Valid() bool { return t == TaskParse || t == TaskAdvice }

type Source string

const (
	SourceUser Source = "user"
	SourceAuto Source = "auto"
	SourceEval Source = "eval"
)

type Message struct {
	Role    string `json:"role"` // system, user, assistant
	Content string `json:"content"`
}

type Format int

const (
	FormatNone       Format = iota
	FormatJSONObject        // {"type":"json_object"}
	FormatJSONSchema        // {"type":"json_schema",...}
)

type ChatRequest struct {
	Model      string
	Messages   []Message
	MaxTokens  int
	Effort     string // empty: no reasoning parameter is sent
	Format     Format
	Schema     json.RawMessage // for FormatJSONSchema
	SchemaName string
}

type ChatResponse struct {
	Content          string
	PromptTokens     *int
	CompletionTokens *int
}

// Provider is an OpenAI-compatible chat endpoint.
type Provider interface {
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
}

type Quota struct {
	Used, Limit, Remaining int
	ResetAt                time.Time
}

// QuotaProvider reports the daily allowance for free models.
type QuotaProvider interface {
	Quota(ctx context.Context) (Quota, error)
}

type ErrKind int

const (
	KindLimit       ErrKind = iota + 1 // 429: model or account rate limit
	KindTimeout                        // no answer in time
	KindUnavailable                    // 5xx, 402, 403, 404, error inside a 200
	KindBadRequest                     // 400
	KindAuth                           // 401: the key itself is wrong
)

// ProviderError is a failed call. Msg is for logs in tests only, never shown or stored.
type ProviderError struct {
	Kind   ErrKind
	Status int
	Msg    string
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("provider error kind=%d status=%d", e.Kind, e.Status)
}

// Verdicts of a Check function.
type verdict struct {
	err   error
	retry bool
}

func (v *verdict) Error() string { return v.err.Error() }
func (v *verdict) Unwrap() error { return v.err }

// Invalid marks an answer that is not valid JSON or breaks the schema. The same
// model is asked once more, with the error text added to the prompt.
func Invalid(err error) error { return &verdict{err: err, retry: true} }

// Failed marks an answer that is well-formed but fails a code check. The chain
// moves on to the next model without a retry.
func Failed(err error) error { return &verdict{err: err} }

// Retryable reports whether a Check error is an Invalid one (asked again on the
// same model) rather than a Failed one (next model).
func Retryable(err error) bool {
	var v *verdict
	return !errors.As(err, &v) || v.retry
}

var (
	ErrNotConfigured = errors.New("llm: no model configured for the task")
	ErrAuth          = errors.New("llm: the provider rejected the API key")
)

// DailyLimitError: the account's free-model allowance for the day is spent.
// Going down the chain would only burn more attempts.
type DailyLimitError struct{ ResetAt time.Time }

func (e *DailyLimitError) Error() string {
	return "daily free-model limit reached, resets at " + e.ResetAt.Format(time.RFC3339)
}

// ExhaustedError: every model of the chain failed, or the call budget ran out.
type ExhaustedError struct {
	Calls  int
	Last   string // result of the last call: limit, timeout, invalid_json, check_failed, error
	Budget bool   // stopped by the per-request call ceiling
}

func (e *ExhaustedError) Error() string {
	return fmt.Sprintf("llm: chain exhausted after %d calls (last: %s)", e.Calls, e.Last)
}

// ExtractJSON pulls one JSON object out of an answer that may be wrapped in a
// code fence or surrounded by a sentence.
func ExtractJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimPrefix(s, "json")
		if i := strings.LastIndex(s, "```"); i >= 0 {
			s = s[:i]
		}
		s = strings.TrimSpace(s)
	}
	if i, j := strings.Index(s, "{"), strings.LastIndex(s, "}"); i >= 0 && j > i {
		return s[i : j+1]
	}
	return s
}
