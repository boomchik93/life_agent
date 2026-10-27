package macagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Source is the name the server knows this client by.
const Source = "mac_activitywatch"

// Life is a client of the life-agent server.
type Life struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

func (l *Life) do(ctx context.Context, method, path string, q url.Values, body, out any) error {
	u := strings.TrimRight(l.BaseURL, "/") + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+l.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := l.HTTP
	if client == nil {
		client = &http.Client{Timeout: 100 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("server is not reachable")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Error == "" {
			e.Error = http.StatusText(resp.StatusCode)
		}
		return &ServerError{Status: resp.StatusCode, Code: e.Code, Message: e.Error}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// ServerError is a refusal of the server with its own message.
type ServerError struct {
	Status  int
	Code    string
	Message string
}

func (e *ServerError) Error() string { return fmt.Sprintf("server: %d %s", e.Status, e.Message) }

// Categorize asks the server for the categories of app names and domains.
func (l *Life) Categorize(ctx context.Context, names []string, llm bool) (map[string]NameCategory, error) {
	out := map[string]NameCategory{}
	for i := 0; i < len(names); i += 100 {
		end := min(i+100, len(names))
		var resp struct {
			Categories map[string]struct {
				Category   string  `json:"category"`
				Confidence float64 `json:"confidence"`
				Resolved   bool    `json:"resolved"`
			} `json:"categories"`
		}
		if err := l.do(ctx, http.MethodPost, "/v1/categorize", nil,
			map[string]any{"names": names[i:end], "llm": llm}, &resp); err != nil {
			return nil, err
		}
		for k, v := range resp.Categories {
			out[k] = NameCategory{Category: v.Category, Confidence: v.Confidence, Resolved: v.Resolved}
		}
	}
	return out, nil
}

// ContextEvents returns the timed calendar events of the day that colour the
// time inside them.
func (l *Life) ContextEvents(ctx context.Context, day time.Time) ([]ContextEvent, error) {
	var resp struct {
		Events []struct {
			Start    time.Time `json:"start"`
			End      time.Time `json:"end"`
			AllDay   bool      `json:"all_day"`
			Category string    `json:"category"`
		} `json:"events"`
	}
	if err := l.do(ctx, http.MethodGet, "/v1/week", url.Values{"start": {Date(day)}}, nil, &resp); err != nil {
		return nil, err
	}
	var out []ContextEvent
	for _, e := range resp.Events {
		if c := ContextCategory(e.Category); c != "" && !e.AllDay {
			out = append(out, ContextEvent{Span: Span{e.Start.In(day.Location()), e.End.In(day.Location())}, Category: c})
		}
	}
	return out, nil
}

// SendDay sends the sums of one day: categories and numbers only.
func (l *Life) SendDay(ctx context.Context, day time.Time, rows []Row) error {
	return l.do(ctx, http.MethodPost, "/v1/activity/daily", nil, map[string]any{
		"source": Source, "date": Date(day), "rows": rows,
	}, nil)
}
