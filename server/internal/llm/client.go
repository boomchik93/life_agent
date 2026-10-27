package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"lifeagent/internal/store"
)

// Journal receives one row per model call.
type Journal interface {
	AddLLMCall(store.LLMCall) error
}

// Client routes a task to its model, walks the fallback chain and journals the calls.
type Client struct {
	Provider     Provider
	ProviderName string
	Quota        QuotaProvider // nil: a 429 is always treated as a limit of that model
	Catalog      Catalog
	// Config returns the effective configuration; it is read on every call, so
	// changes in the app apply without a restart.
	Config  func() (Config, error)
	Journal Journal
	Now     func() time.Time
}

type Input struct {
	Task       Task
	Source     Source
	System     string
	User       string
	SchemaName string
	Schema     json.RawMessage
	// Check validates the extracted JSON text. Return Invalid(err) for a
	// malformed answer and Failed(err) for a failed code check; nil if fine.
	Check func(raw string) error
}

type Output struct {
	JSON  string // the validated answer
	Model string
	Calls int // model calls spent on this request, all attempts included
}

// Call asks the task's model and returns a validated answer.
//
// A failing model passes the request to the next model of the chain, except:
// an invalid answer gets one retry on the same model with the error added, and
// the account's daily limit ends the whole call. At most Limits.MaxCallsPerRequest
// calls are made in total.
func (c *Client) Call(ctx context.Context, in Input) (Output, error) {
	cfg, err := c.Config()
	if err != nil {
		return Output{}, err
	}
	tc, ok := cfg.Tasks[in.Task]
	if !ok || tc.Model == "" {
		return Output{}, ErrNotConfigured
	}
	chain := append([]string{tc.Model}, tc.Fallbacks...)
	maxCalls := cfg.Limits.MaxCallsPerRequest
	base := []Message{{Role: "system", Content: in.System}, {Role: "user", Content: in.User}}

	calls := 0
	last := ""
	for pos, model := range chain {
		info := c.Catalog.Info(ctx, model)
		req := ChatRequest{Model: model, MaxTokens: tc.MaxTokens, Schema: in.Schema, SchemaName: in.SchemaName}
		switch {
		case info.Structured && len(in.Schema) > 0:
			req.Format = FormatJSONSchema
		case info.JSONMode:
			req.Format = FormatJSONObject
		}
		if info.Reasoning {
			req.Effort = tc.Effort
		}

		msgs := base
		for attempt := 1; ; attempt++ {
			if calls >= maxCalls {
				return Output{}, &ExhaustedError{Calls: calls, Last: last, Budget: true}
			}
			calls++
			req.Messages = msgs

			start := c.now()
			resp, err := c.Provider.Chat(ctx, req)
			call := store.LLMCall{
				Time: start, Task: string(in.Task), Provider: c.ProviderName, Model: model,
				LatencyMS: c.now().Sub(start).Milliseconds(), Attempt: attempt, ChainPos: pos, Source: string(in.Source),
				PromptTokens: resp.PromptTokens, CompletionTokens: resp.CompletionTokens,
			}

			if err != nil {
				var pe *ProviderError
				if !errors.As(err, &pe) {
					return Output{}, err // the caller's context ended
				}
				call.Result = resultOf(pe.Kind)
				c.record(call)
				last = call.Result
				if pe.Kind == KindAuth {
					return Output{}, ErrAuth
				}
				if pe.Kind == KindLimit && info.Free {
					if q, qerr := c.quota(ctx); qerr == nil && q.Remaining <= 0 {
						return Output{}, &DailyLimitError{ResetAt: q.ResetAt}
					}
				}
				break // next model: a limit is not retried, nor is a timeout or an outage
			}

			raw := ExtractJSON(resp.Content)
			cerr := checkJSON(raw)
			if cerr == nil && in.Check != nil {
				cerr = in.Check(raw)
			}
			if cerr == nil {
				call.Result = "ok"
				c.record(call)
				return Output{JSON: raw, Model: model, Calls: calls}, nil
			}

			retry := Retryable(cerr)
			if retry {
				call.Result = "invalid_json"
			} else {
				call.Result = "check_failed"
			}
			c.record(call)
			last = call.Result
			if !retry || attempt > 1 {
				break
			}
			msgs = append(append([]Message(nil), base...),
				Message{Role: "assistant", Content: resp.Content},
				Message{Role: "user", Content: "Ответ не прошёл проверку: " + errText(cerr) +
					". Верни только исправленный JSON по той же схеме, без пояснений."})
		}
	}
	return Output{}, &ExhaustedError{Calls: calls, Last: last}
}

func checkJSON(raw string) error {
	var v map[string]any
	if err := json.Unmarshal([]byte(raw), &v); err != nil || v == nil {
		return Invalid(fmt.Errorf("ответ не JSON-объект"))
	}
	return nil
}

func errText(err error) string {
	s := err.Error()
	if len(s) > 300 {
		s = s[:300]
	}
	return strings.ReplaceAll(s, "\n", " ")
}

func resultOf(k ErrKind) string {
	switch k {
	case KindLimit:
		return "limit"
	case KindTimeout:
		return "timeout"
	}
	return "error"
}

func (c *Client) quota(ctx context.Context) (Quota, error) {
	if c.Quota == nil {
		return Quota{}, errors.New("no quota source")
	}
	return c.Quota.Quota(ctx)
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// record never fails the request: a journal error is not the user's problem.
func (c *Client) record(call store.LLMCall) {
	if c.Journal != nil {
		_ = c.Journal.AddLLMCall(call)
	}
}
