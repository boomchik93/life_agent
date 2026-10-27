package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

type ModelInfo struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Free          bool   `json:"free"`
	Reasoning     bool   `json:"reasoning"`  // takes the `reasoning` parameter, so effort applies
	Structured    bool   `json:"structured"` // json_schema response format
	JSONMode      bool   `json:"json_mode"`  // json_object response format
	ContextLength int    `json:"context_length"`
}

// Catalog describes the models a provider offers.
type Catalog interface {
	// List returns the free text models.
	List(ctx context.Context) ([]ModelInfo, error)
	// Info returns what is known about a model; an unknown id has no capabilities.
	Info(ctx context.Context, id string) ModelInfo
}

// OpenRouterCatalog reads GET /models and keeps the result for a while.
type OpenRouterCatalog struct {
	BaseURL string
	Client  *http.Client
	TTL     time.Duration
	Now     func() time.Time

	mu      sync.Mutex
	fetched time.Time
	all     map[string]ModelInfo
}

func NewOpenRouterCatalog(baseURL string) *OpenRouterCatalog {
	return &OpenRouterCatalog{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Client:  &http.Client{Timeout: 20 * time.Second},
		TTL:     time.Hour,
		Now:     time.Now,
	}
}

// load refreshes the cache when it is older than the TTL. A failed refresh
// keeps serving the stale copy; it fails only when there is nothing cached.
func (c *OpenRouterCatalog) load(ctx context.Context) (map[string]ModelInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.all != nil && c.Now().Sub(c.fetched) < c.TTL {
		return c.all, nil
	}
	all, err := c.fetch(ctx)
	if err != nil {
		if c.all != nil {
			return c.all, nil
		}
		return nil, err
	}
	c.all, c.fetched = all, c.Now()
	return all, nil
}

func (c *OpenRouterCatalog) fetch(ctx context.Context) (map[string]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models: status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	var body struct {
		Data []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			ContextLength int    `json:"context_length"`
			Architecture  struct {
				Output []string `json:"output_modalities"`
			} `json:"architecture"`
			Supported []string `json:"supported_parameters"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	all := make(map[string]ModelInfo, len(body.Data))
	for _, m := range body.Data {
		if !slices.Contains(m.Architecture.Output, "text") {
			continue
		}
		all[m.ID] = ModelInfo{
			ID: m.ID, Name: m.Name, ContextLength: m.ContextLength,
			Free:       strings.HasSuffix(m.ID, ":free"),
			Reasoning:  slices.Contains(m.Supported, "reasoning"),
			Structured: slices.Contains(m.Supported, "structured_outputs"),
			JSONMode:   slices.Contains(m.Supported, "response_format"),
		}
	}
	return all, nil
}

func (c *OpenRouterCatalog) List(ctx context.Context) ([]ModelInfo, error) {
	all, err := c.load(ctx)
	if err != nil {
		return nil, err
	}
	var out []ModelInfo
	for _, m := range all {
		if m.Free {
			out = append(out, m)
		}
	}
	slices.SortFunc(out, func(a, b ModelInfo) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

func (c *OpenRouterCatalog) Info(ctx context.Context, id string) ModelInfo {
	if all, err := c.load(ctx); err == nil {
		if m, ok := all[id]; ok {
			return m
		}
	}
	return ModelInfo{ID: id, Free: strings.HasSuffix(id, ":free")}
}
