package store

import "time"

// LLMCall is one journal row. It carries no request or response text.
type LLMCall struct {
	Time             time.Time `json:"time"`
	Task             string    `json:"task"`
	Provider         string    `json:"provider"`
	Model            string    `json:"model"`
	LatencyMS        int64     `json:"latency_ms"`
	PromptTokens     *int      `json:"prompt_tokens"`
	CompletionTokens *int      `json:"completion_tokens"`
	Result           string    `json:"result"` // ok, invalid_json, check_failed, timeout, limit, error
	Attempt          int       `json:"attempt"`
	ChainPos         int       `json:"chain_pos"`
	Source           string    `json:"source"` // user, auto, eval
}

const tsLayout = "2006-01-02T15:04:05.000Z"

func (s *Store) AddLLMCall(c LLMCall) error {
	_, err := s.DB.Exec(`INSERT INTO llm_calls(ts, task, provider, model, latency_ms, prompt_tokens,
		completion_tokens, result, attempt, chain_pos, source) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		c.Time.UTC().Format(tsLayout), c.Task, c.Provider, c.Model, c.LatencyMS, c.PromptTokens,
		c.CompletionTokens, c.Result, c.Attempt, c.ChainPos, c.Source)
	return err
}

// CountLLMCalls returns the number of journal rows with ts >= since.
func (s *Store) CountLLMCalls(since time.Time) (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM llm_calls WHERE ts >= ?`, since.UTC().Format(tsLayout)).Scan(&n)
	return n, err
}

// RecentLLMCalls returns the newest rows first.
func (s *Store) RecentLLMCalls(limit int) ([]LLMCall, error) {
	rows, err := s.DB.Query(`SELECT ts, task, provider, model, latency_ms, prompt_tokens, completion_tokens,
		result, attempt, chain_pos, source FROM llm_calls ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LLMCall
	for rows.Next() {
		var c LLMCall
		var ts string
		if err := rows.Scan(&ts, &c.Task, &c.Provider, &c.Model, &c.LatencyMS, &c.PromptTokens,
			&c.CompletionTokens, &c.Result, &c.Attempt, &c.ChainPos, &c.Source); err != nil {
			return nil, err
		}
		if c.Time, err = time.Parse(tsLayout, ts); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
