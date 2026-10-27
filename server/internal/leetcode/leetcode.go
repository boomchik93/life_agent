// Package leetcode is the optional, unofficial path: the number of accepted
// solutions per day from the public GraphQL of leetcode.com. The main source
// of LeetCode time is the browser (stage 10.2). This path may break at any
// moment; then the source switches itself off and keeps the error in its
// state (PLAN.md stage 10.4).
package leetcode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"lifeagent/internal/activity"
)

const Every = 24 * time.Hour

const query = `query($username: String!) { recentAcSubmissionList(username: $username, limit: 50) { timestamp } }`

type Collector struct {
	User string
	Loc  *time.Location
	HTTP *http.Client
	URL  string
}

func New(user string, loc *time.Location) *Collector {
	return &Collector{User: user, Loc: loc, URL: "https://leetcode.com/graphql"}
}

func (c *Collector) Source() string { return activity.LeetCode }

// DisableOnError: an unofficial path that fails is switched off quietly.
func (c *Collector) DisableOnError() bool { return true }

func (c *Collector) Collect(ctx context.Context, now time.Time) ([]activity.DayRows, error) {
	body, err := json.Marshal(map[string]any{"query": query, "variables": map[string]any{"username": c.User}})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Referer", "https://leetcode.com")
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("leetcode is not reachable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("leetcode answered %d: the unofficial path no longer works", resp.StatusCode)
	}
	var r struct {
		Data struct {
			List []struct {
				Timestamp string `json:"timestamp"` // unix seconds, as text
			} `json:"recentAcSubmissionList"`
		} `json:"data"`
		Errors []struct{ Message string } `json:"errors"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&r); err != nil || len(r.Errors) > 0 {
		return nil, errors.New("leetcode returned an answer the collector does not understand")
	}

	// Only the last three days: older ones are final and already stored.
	local := now.In(c.Loc)
	first := time.Date(local.Year(), local.Month(), local.Day()-2, 0, 0, 0, 0, c.Loc)
	perDay := map[string]int{}
	for _, s := range r.Data.List {
		sec, err := strconv.ParseInt(s.Timestamp, 10, 64)
		if err != nil {
			continue
		}
		t := time.Unix(sec, 0).In(c.Loc)
		if t.Before(first) || t.After(local) {
			continue
		}
		perDay[t.Format("2006-01-02")]++
	}
	var out []activity.DayRows
	for back := 2; back >= 0; back-- {
		d := time.Date(local.Year(), local.Month(), local.Day()-back, 0, 0, 0, 0, c.Loc).Format("2006-01-02")
		var rows []activity.Row
		if n := perDay[d]; n > 0 {
			rows = []activity.Row{{Category: activity.Career, Count: n, Confidence: 1, Extra: map[string]any{"solved": float64(n)}}}
		}
		out = append(out, activity.DayRows{Date: d, Rows: rows})
	}
	return out, nil
}
