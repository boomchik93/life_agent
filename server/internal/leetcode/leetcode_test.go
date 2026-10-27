package leetcode

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"lifeagent/internal/activity"
	"lifeagent/internal/store"
)

var yekat, _ = time.LoadLocation("Asia/Yekaterinburg")

var now = time.Date(2026, 10, 28, 12, 0, 0, 0, yekat)

func ts(t time.Time) string { return fmt.Sprint(t.Unix()) }

func server(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSolvedPerDay(t *testing.T) {
	body := fmt.Sprintf(`{"data":{"recentAcSubmissionList":[{"timestamp":%q},{"timestamp":%q},{"timestamp":%q},{"timestamp":%q}]}}`,
		ts(now.Add(-time.Hour)), ts(now.Add(-2*time.Hour)), ts(now.Add(-26*time.Hour)), ts(now.Add(-10*24*time.Hour)))
	c := New("octocat", yekat)
	c.URL = server(t, 200, body).URL
	days, err := c.Collect(context.Background(), now)
	if err != nil || len(days) != 3 {
		t.Fatalf("%+v %v", days, err)
	}
	byDate := map[string]activity.DayRows{}
	for _, d := range days {
		byDate[d.Date] = d
	}
	if r := byDate["2026-10-28"].Rows; len(r) != 1 || r[0].Count != 2 || r[0].Category != "career" {
		t.Errorf("today: %+v", byDate["2026-10-28"])
	}
	if r := byDate["2026-10-27"].Rows; len(r) != 1 || r[0].Count != 1 {
		t.Errorf("yesterday: %+v", byDate["2026-10-27"])
	}
	if byDate["2026-10-26"].Rows != nil {
		t.Errorf("an old submission counted: %+v", byDate["2026-10-26"])
	}
	if err := byDate["2026-10-28"].Rows[0].Validate(activity.LeetCode); err != nil {
		t.Error(err)
	}
}

func TestABrokenPathSwitchesTheSourceOffQuietly(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for name, srv := range map[string]*httptest.Server{
		"blocked":  server(t, 403, `forbidden`),
		"changed":  server(t, 200, `{"errors":[{"message":"Cannot query field"}]}`),
		"not json": server(t, 200, `<html>`),
	} {
		st.SetSourceEnabled(activity.LeetCode, true)
		c := New("octocat", yekat)
		c.URL = srv.URL
		if err := activity.Run(context.Background(), c, now, st); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
		states, _ := st.SourceStates([]string{activity.LeetCode})
		if states[0].Enabled || states[0].LastError == nil {
			t.Errorf("%s: state %+v", name, states[0])
		}
		// A switched-off source is left alone on the next round.
		if err := activity.Run(context.Background(), c, now, st); err != nil {
			t.Errorf("%s: a disabled source ran again: %v", name, err)
		}
	}
	// A working path stays on.
	st.SetSourceEnabled(activity.LeetCode, true)
	ok := New("octocat", yekat)
	ok.URL = server(t, 200, `{"data":{"recentAcSubmissionList":[]}}`).URL
	if err := activity.Run(context.Background(), ok, now, st); err != nil {
		t.Fatal(err)
	}
	if states, _ := st.SourceStates([]string{activity.LeetCode}); !states[0].Enabled || states[0].LastError != nil {
		t.Errorf("%+v", states[0])
	}
}
