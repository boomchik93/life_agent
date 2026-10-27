// Package github counts the user's contributions per day (commits, pull
// requests, reviews) from the GraphQL API with a read-only token. They go to
// the "career" category as counts: a contribution is not minutes
// (PLAN.md stage 10.4).
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"lifeagent/internal/activity"
)

// Every is how often the collector runs (PLAN.md section 14: once a day).
const Every = 24 * time.Hour

const query = `query($login: String!, $from: DateTime!, $to: DateTime!) {
  user(login: $login) {
    contributionsCollection(from: $from, to: $to) {
      contributionCalendar { weeks { contributionDays { date contributionCount } } }
      commitContributionsByRepository(maxRepositories: 100) {
        contributions(first: 100) { nodes { occurredAt commitCount } }
      }
      pullRequestContributions(first: 100) { nodes { occurredAt } }
      pullRequestReviewContributions(first: 100) { nodes { occurredAt } }
    }
  }
}`

// Collector implements activity.Collector for GitHub.
type Collector struct {
	Token, Login string // from .env; the token is never logged or stored
	Loc          *time.Location
	HTTP         *http.Client
	URL          string // the GraphQL endpoint; tests replace it
}

func New(token, login string, loc *time.Location) *Collector {
	return &Collector{Token: token, Login: login, Loc: loc, URL: "https://api.github.com/graphql"}
}

func (c *Collector) Source() string { return activity.GitHub }

type nodes struct {
	Nodes []struct {
		OccurredAt  time.Time `json:"occurredAt"`
		CommitCount int       `json:"commitCount"`
	} `json:"nodes"`
}

type response struct {
	Data struct {
		User *struct {
			Collection struct {
				Calendar struct {
					Weeks []struct {
						Days []struct {
							Date  string `json:"date"`
							Count int    `json:"contributionCount"`
						} `json:"contributionDays"`
					} `json:"weeks"`
				} `json:"contributionCalendar"`
				ByRepo []struct {
					Contributions nodes `json:"contributions"`
				} `json:"commitContributionsByRepository"`
				PRs     nodes `json:"pullRequestContributions"`
				Reviews nodes `json:"pullRequestReviewContributions"`
			} `json:"contributionsCollection"`
		} `json:"user"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// Collect reads the last three days. The day's count is the one GitHub's own
// contribution graph shows, so it matches the profile.
func (c *Collector) Collect(ctx context.Context, now time.Time) ([]activity.DayRows, error) {
	to := now.UTC()
	from := time.Date(to.Year(), to.Month(), to.Day()-2, 0, 0, 0, 0, time.UTC)
	body, err := json.Marshal(map[string]any{"query": query, "variables": map[string]any{
		"login": c.Login, "from": from.Format(time.RFC3339), "to": to.Format(time.RFC3339),
	}})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("github is not reachable")
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, errors.New("github rejected the token: it may have expired, issue a new one")
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("github answered %d", resp.StatusCode)
	}
	var r response
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&r); err != nil {
		return nil, errors.New("github returned an unreadable answer")
	}
	if len(r.Errors) > 0 {
		return nil, errors.New("github refused the query")
	}
	if r.Data.User == nil {
		return nil, errors.New("github user was not found")
	}
	col := r.Data.User.Collection

	commits, prs, reviews := map[string]int{}, map[string]int{}, map[string]int{}
	day := func(t time.Time) string { return t.UTC().Format("2006-01-02") }
	for _, repo := range col.ByRepo {
		for _, n := range repo.Contributions.Nodes {
			commits[day(n.OccurredAt)] += n.CommitCount
		}
	}
	for _, n := range col.PRs.Nodes {
		prs[day(n.OccurredAt)]++
	}
	for _, n := range col.Reviews.Nodes {
		reviews[day(n.OccurredAt)]++
	}

	var out []activity.DayRows
	for _, w := range col.Calendar.Weeks {
		for _, d := range w.Days {
			if d.Date < from.Format("2006-01-02") || d.Date > to.Format("2006-01-02") {
				continue
			}
			var rows []activity.Row
			if d.Count > 0 {
				rows = []activity.Row{{
					Category: activity.Career, Count: d.Count, Confidence: 1,
					Extra: map[string]any{"commits": float64(commits[d.Date]), "prs": float64(prs[d.Date]), "reviews": float64(reviews[d.Date])},
				}}
			}
			out = append(out, activity.DayRows{Date: d.Date, Rows: rows})
		}
	}
	return out, nil
}
