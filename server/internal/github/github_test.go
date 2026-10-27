package github

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lifeagent/internal/activity"
	"lifeagent/internal/store"
)

var yekat, _ = time.LoadLocation("Asia/Yekaterinburg")

const answer = `{"data":{"user":{"contributionsCollection":{
  "contributionCalendar":{"weeks":[{"contributionDays":[
    {"date":"2026-10-20","contributionCount":9},
    {"date":"2026-10-26","contributionCount":0},
    {"date":"2026-10-27","contributionCount":7},
    {"date":"2026-10-28","contributionCount":2}]}]},
  "commitContributionsByRepository":[
    {"contributions":{"nodes":[{"occurredAt":"2026-10-27T10:00:00Z","commitCount":4},{"occurredAt":"2026-10-28T09:00:00Z","commitCount":1}]}},
    {"contributions":{"nodes":[{"occurredAt":"2026-10-27T15:00:00Z","commitCount":1}]}}],
  "pullRequestContributions":{"nodes":[{"occurredAt":"2026-10-27T12:00:00Z"}]},
  "pullRequestReviewContributions":{"nodes":[{"occurredAt":"2026-10-27T13:00:00Z"},{"occurredAt":"2026-10-28T08:00:00Z"}]}
}}}}`

func server(t *testing.T, status int, body string, gotAuth *string, gotBody *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gotAuth != nil {
			*gotAuth = r.Header.Get("Authorization")
		}
		if gotBody != nil {
			b, _ := io.ReadAll(r.Body)
			*gotBody = string(b)
		}
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDaysMatchTheContributionGraph(t *testing.T) {
	var auth, sent string
	srv := server(t, 200, answer, &auth, &sent)
	c := New("SECRET-TOKEN", "octocat", yekat)
	c.URL = srv.URL
	days, err := c.Collect(context.Background(), time.Date(2026, 10, 28, 12, 0, 0, 0, yekat))
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer SECRET-TOKEN" || !strings.Contains(sent, `"login":"octocat"`) {
		t.Errorf("request: %q %q", auth, sent)
	}
	got := map[string]activity.DayRows{}
	for _, d := range days {
		got[d.Date] = d
	}
	// The window is the last three days: the 20th is out, the 26th has no work.
	if len(got) != 3 || got["2026-10-26"].Rows != nil {
		t.Fatalf("days: %+v", got)
	}
	y := got["2026-10-27"].Rows[0]
	if y.Category != "career" || y.Count != 7 || y.Minutes != 0 {
		t.Errorf("yesterday: %+v", y)
	}
	if y.Extra["commits"] != 5.0 || y.Extra["prs"] != 1.0 || y.Extra["reviews"] != 1.0 {
		t.Errorf("breakdown: %v", y.Extra)
	}
	if err := y.Validate(activity.GitHub); err != nil {
		t.Errorf("refused by the rules: %v", err)
	}
	if got["2026-10-28"].Rows[0].Count != 2 {
		t.Errorf("today: %+v", got["2026-10-28"])
	}
}

func TestErrorsAreReadableAndKeepTheTokenOut(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, yekat)
	for name, tc := range map[string]struct {
		status int
		body   string
		want   string
	}{
		"expired token": {401, `{"message":"Bad credentials"}`, "may have expired"},
		"server error":  {502, `x`, "answered 502"},
		"graphql error": {200, `{"errors":[{"message":"Could not resolve to a User with the login SECRET-TOKEN"}]}`, "refused the query"},
		"no user":       {200, `{"data":{"user":null}}`, "was not found"},
		"garbage":       {200, `<html>`, "unreadable"},
	} {
		srv := server(t, tc.status, tc.body, nil, nil)
		c := New("SECRET-TOKEN", "octocat", yekat)
		c.URL = srv.URL
		_, err := c.Collect(context.Background(), now)
		if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "SECRET") {
			t.Errorf("%s: %v", name, err)
		}
	}
	c := New("SECRET-TOKEN", "octocat", yekat)
	c.URL = "http://127.0.0.1:1"
	if _, err := c.Collect(context.Background(), now); err == nil || strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("outage: %v", err)
	}
}

func TestRunnerStoresTheCounts(t *testing.T) {
	srv := server(t, 200, answer, nil, nil)
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	c := New("T", "octocat", yekat)
	c.URL = srv.URL
	if err := activity.Run(context.Background(), c, time.Date(2026, 10, 28, 12, 0, 0, 0, yekat), st); err != nil {
		t.Fatal(err)
	}
	rows, _ := st.Activity("2026-10-01", "2026-10-31", "github")
	if len(rows) != 2 || rows[0].Count != 7 || rows[0].Minutes != 0 {
		t.Errorf("rows: %+v", rows)
	}
}
