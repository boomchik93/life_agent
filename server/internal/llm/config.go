package llm

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

//go:embed defaults.json
var defaultsJSON []byte

const (
	MaxFallbacks = 3
	// SettingKey is where the user's overrides live in the settings table.
	SettingKey = "llm_tasks"
)

type TaskConfig struct {
	Model     string   `json:"model"`
	Effort    string   `json:"effort"` // low, medium, high; sent only to models with reasoning
	Fallbacks []string `json:"fallbacks"`
	MaxTokens int      `json:"max_tokens"`
}

type Config struct {
	Provider struct {
		Name    string `json:"name"`
		BaseURL string `json:"base_url"`
	} `json:"provider"`
	Tasks  map[Task]TaskConfig `json:"tasks"`
	Limits struct {
		MaxCallsPerRequest int `json:"max_calls_per_request"`
		TimeoutSec         int `json:"timeout_sec"`
	} `json:"limits"`
}

// Override is what the user changes in the app; absent fields keep the default.
type Override struct {
	Model     *string   `json:"model"`
	Effort    *string   `json:"effort"`
	Fallbacks *[]string `json:"fallbacks"`
}

// DefaultConfig parses the config file embedded in the binary.
func DefaultConfig() (Config, error) {
	var c Config
	if err := json.Unmarshal(defaultsJSON, &c); err != nil {
		return c, fmt.Errorf("llm defaults: %w", err)
	}
	return c, nil
}

func validEffort(e string) bool { return e == "low" || e == "medium" || e == "high" }

// ParseOverrides reads the stored `llm_tasks` object and validates it.
func ParseOverrides(raw []byte) (map[Task]Override, error) {
	var m map[Task]Override
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	for task, o := range m {
		if !task.Valid() {
			return nil, fmt.Errorf("unknown task %q", task)
		}
		if o.Model != nil && !validModelID(*o.Model) {
			return nil, fmt.Errorf("%s: bad model id %q", task, *o.Model)
		}
		if o.Effort != nil && !validEffort(*o.Effort) {
			return nil, fmt.Errorf("%s: effort must be low, medium or high", task)
		}
		if o.Fallbacks != nil {
			if err := validateChain(*o.Fallbacks, o.Model); err != nil {
				return nil, fmt.Errorf("%s: %w", task, err)
			}
		}
	}
	return m, nil
}

func validModelID(id string) bool {
	return id == "" || (len(id) <= 120 && !strings.ContainsAny(id, " \t\r\n\"'<>") && strings.Contains(id, "/"))
}

func validateChain(fallbacks []string, model *string) error {
	if len(fallbacks) > MaxFallbacks {
		return fmt.Errorf("at most %d fallback models", MaxFallbacks)
	}
	seen := map[string]bool{}
	if model != nil && *model != "" {
		seen[*model] = true
	}
	for _, f := range fallbacks {
		if f == "" || !validModelID(f) {
			return fmt.Errorf("bad fallback model id %q", f)
		}
		if seen[f] {
			return fmt.Errorf("model %q is in the chain twice", f)
		}
		seen[f] = true
	}
	return nil
}

// Merge applies the user's overrides on top of the defaults.
func (c Config) Merge(over map[Task]Override) (Config, error) {
	out := c
	out.Tasks = map[Task]TaskConfig{}
	for t, tc := range c.Tasks {
		tc.Fallbacks = append([]string(nil), tc.Fallbacks...)
		if o, ok := over[t]; ok {
			if o.Model != nil {
				tc.Model = *o.Model
			}
			if o.Effort != nil {
				tc.Effort = *o.Effort
			}
			if o.Fallbacks != nil {
				tc.Fallbacks = append([]string(nil), *o.Fallbacks...)
			}
		}
		// The merged chain must stay valid: a new main model may equal an old fallback.
		if err := validateChain(tc.Fallbacks, &tc.Model); err != nil {
			return out, fmt.Errorf("%s: %w", t, err)
		}
		out.Tasks[t] = tc
	}
	if out.Limits.MaxCallsPerRequest < 1 {
		return out, errors.New("llm: max_calls_per_request must be at least 1")
	}
	return out, nil
}

// Effective merges the defaults with the user's overrides stored in settings
// (value of SettingKey, if present).
func Effective(defaults Config, settings map[string]string) (Config, error) {
	var over map[Task]Override
	if raw, ok := settings[SettingKey]; ok {
		var err error
		if over, err = ParseOverrides([]byte(raw)); err != nil {
			return Config{}, fmt.Errorf("setting %s: %w", SettingKey, err)
		}
	}
	return defaults.Merge(over)
}
