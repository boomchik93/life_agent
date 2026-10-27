package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lifeagent/internal/advice"
	"lifeagent/internal/calendar"
	"lifeagent/internal/llm"
	"lifeagent/internal/store"
)

type fakeAdvisor struct {
	in    []advice.Input
	srcs  []llm.Source
	err   error
	text  string
	calls int
}

func (f *fakeAdvisor) Generate(_ context.Context, in advice.Input, src llm.Source) (advice.Result, error) {
	f.calls++
	f.in = append(f.in, in)
	f.srcs = append(f.srcs, src)
	if f.err != nil {
		return advice.Result{}, f.err
	}
	text := f.text
	if text == "" {
		text = "Сдвинь сдачу на среду. Ляг до полуночи."
	}
	return advice.Result{Text: text, Model: "a/m:free", Calls: 1}, nil
}

// heavyWeek: 12 hours of lessons on Monday make the overload risk red.
func heavyWeek() []calendar.Event {
	return []calendar.Event{{ID: "1", Source: "ics", Calendar: "Расписание", Title: "Пары", Start: at(26, 9, 0), End: at(26, 21, 0)}}
}

func lightWeek() []calendar.Event {
	return []calendar.Event{{ID: "1", Source: "google", Calendar: "c", Title: "Кофе", Start: at(28, 10, 0), End: at(28, 11, 0)}}
}

func adviceServer(t *testing.T, events []calendar.Event, adv AdviceGenerator, quota llm.Quota, col *fakeCollector) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if col == nil {
		col = &fakeCollector{events: events}
	}
	return NewServer(Options{
		Store: st, Token: testToken, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		RatePerSec: 100, Burst: 100, Calendar: col, Location: yekat, Advisor: adv,
		LLM: &LLMInfo{Quota: fakeQuota{q: quota}},
		Now: func() time.Time { return at(27, 12, 0) },
	}), st
}

var okQuota = llm.Quota{Used: 5, Limit: 50, Remaining: 45}

func TestAdviceOnRequest(t *testing.T) {
	adv := &fakeAdvisor{}
	srv, _ := adviceServer(t, heavyWeek(), adv, okQuota, nil)
	h := srv.Handler()

	rec := post(h, "/v1/advice", "", "")
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	var got store.Advice
	decodeJSON(t, rec, &got)
	if got.Source != "user" || got.Status != "delivered" || got.Text == "" || !strings.HasPrefix(got.Reason, "asked") {
		t.Errorf("advice: %+v", got)
	}
	if adv.srcs[0] != llm.SourceUser {
		t.Errorf("source %v", adv.srcs[0])
	}
	in := adv.in[0]
	if in.Private != nil {
		t.Errorf("private data left the server without the toggle: %+v", in.Private)
	}
	if in.HoursByCategory["study"] != 12 || in.Colors["overload"] != "red" {
		t.Errorf("summary: %+v", in)
	}

	var hist struct{ Advice []store.Advice }
	decodeJSON(t, request(h, "GET", "/v1/advice", ""), &hist)
	if len(hist.Advice) != 1 || hist.Advice[0].ID != got.ID {
		t.Errorf("history: %+v", hist)
	}
}

func TestAdviceSendsPrivateDataOnlyWhenAllowed(t *testing.T) {
	adv := &fakeAdvisor{}
	srv, _ := adviceServer(t, heavyWeek(), adv, okQuota, nil)
	h := srv.Handler()
	post(h, "/v1/checkin", "", `{"energy":2,"mood":2,"sleep_hours":5}`)

	if rec := request(h, "PUT", "/v1/settings", `{"advice":{"include_private":true}}`); rec.Code != 200 {
		t.Fatalf("settings: %d %s", rec.Code, rec.Body)
	}
	if rec := post(h, "/v1/advice", "", ""); rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	p := adv.in[0].Private
	if p == nil || p.AvgSleepHours == nil || *p.AvgSleepHours != 5 || p.AvgStateScore == nil || *p.AvgStateScore != 2 {
		t.Errorf("private = %+v", p)
	}
}

func TestPrepareAdviceFollowsTheRules(t *testing.T) {
	adv := &fakeAdvisor{}
	srv, st := adviceServer(t, heavyWeek(), adv, okQuota, nil)
	ctx := context.Background()

	a, err := srv.PrepareAdvice(ctx)
	if err != nil || a == nil || a.Status != "pending" || a.Source != "auto" || a.Reason != advice.ReasonOverload {
		t.Fatalf("prepared: %+v err %v", a, err)
	}
	if adv.srcs[0] != llm.SourceAuto {
		t.Errorf("source %v", adv.srcs[0])
	}
	// One prepared advice waits at a time.
	if again, err := srv.PrepareAdvice(ctx); err != nil || again != nil || adv.calls != 1 {
		t.Errorf("second round: %+v %v calls %d", again, err, adv.calls)
	}

	h := srv.Handler()
	var pend struct{ Advice *store.Advice }
	decodeJSON(t, request(h, "GET", "/v1/advice/pending", ""), &pend)
	if pend.Advice == nil || pend.Advice.ID != a.ID || pend.Advice.Status != "delivered" {
		t.Errorf("pending: %+v", pend.Advice)
	}
	pend.Advice = nil
	decodeJSON(t, request(h, "GET", "/v1/advice/pending", ""), &pend)
	if pend.Advice != nil {
		t.Errorf("a notification must come once: %+v", pend.Advice)
	}
	// A day later it is still too early: the interval is 7 days.
	if again, _ := srv.PrepareAdvice(ctx); again != nil || adv.calls != 1 {
		t.Errorf("interval ignored: %+v calls %d", again, adv.calls)
	}
	last, _ := st.LastAdviceAt()
	if last == nil || !last.Equal(at(27, 12, 0).UTC()) {
		t.Errorf("last advice at %v", last)
	}
}

func TestPrepareAdviceStaysQuietWhenNothingIsWrong(t *testing.T) {
	adv := &fakeAdvisor{}
	srv, _ := adviceServer(t, lightWeek(), adv, okQuota, nil)
	if a, err := srv.PrepareAdvice(context.Background()); err != nil || a != nil || adv.calls != 0 {
		t.Errorf("a calm week got advice: %+v %v calls %d", a, err, adv.calls)
	}
}

func TestPrepareAdviceLeavesTheQuotaToParsing(t *testing.T) {
	adv := &fakeAdvisor{}
	srv, _ := adviceServer(t, heavyWeek(), adv, llm.Quota{Used: 41, Limit: 50, Remaining: 9}, nil)
	if a, err := srv.PrepareAdvice(context.Background()); err != nil || a != nil || adv.calls != 0 {
		t.Errorf("advice ran with 9 requests left: %+v %v calls %d", a, err, adv.calls)
	}
	srv2, _ := adviceServer(t, heavyWeek(), adv, llm.Quota{Used: 40, Limit: 50, Remaining: 10}, nil)
	if a, _ := srv2.PrepareAdvice(context.Background()); a == nil {
		t.Error("10 requests left is enough")
	}
	// The user's own request ignores the reserve.
	srv3, _ := adviceServer(t, heavyWeek(), adv, llm.Quota{Remaining: 1}, nil)
	if rec := post(srv3.Handler(), "/v1/advice", "", ""); rec.Code != 200 {
		t.Errorf("manual advice: %d", rec.Code)
	}
}

func TestPrepareAdviceSurvivesModelTrouble(t *testing.T) {
	for name, err := range map[string]error{
		"daily limit": &llm.DailyLimitError{ResetAt: at(28, 5, 0)},
		"exhausted":   &llm.ExhaustedError{Calls: 3, Last: "limit"},
		"no model":    llm.ErrNotConfigured,
	} {
		srv, st := adviceServer(t, heavyWeek(), &fakeAdvisor{err: err}, okQuota, nil)
		if a, perr := srv.PrepareAdvice(context.Background()); a != nil || perr != nil {
			t.Errorf("%s: %+v %v", name, a, perr)
		}
		if last, _ := st.LastAdviceAt(); last != nil {
			t.Errorf("%s: a failed round must not count as an advice", name)
		}
	}
	srv, _ := adviceServer(t, heavyWeek(), &fakeAdvisor{err: errors.New("boom")}, okQuota, nil)
	if _, err := srv.PrepareAdvice(context.Background()); err == nil {
		t.Error("an unknown error must be reported")
	}
}

func TestAdviceErrors(t *testing.T) {
	// No advisor: no key.
	srv, _ := adviceServer(t, nil, nil, okQuota, nil)
	if rec := post(srv.Handler(), "/v1/advice", "", ""); rec.Code != 503 || !strings.Contains(rec.Body.String(), "llm_not_configured") {
		t.Errorf("no advisor: %d %s", rec.Code, rec.Body)
	}
	// The daily limit shows the reset time.
	srv, _ = adviceServer(t, heavyWeek(), &fakeAdvisor{err: &llm.DailyLimitError{ResetAt: at(28, 5, 0)}}, okQuota, nil)
	if rec := post(srv.Handler(), "/v1/advice", "", ""); rec.Code != 429 || !strings.Contains(rec.Body.String(), "05:00") {
		t.Errorf("daily limit: %d %s", rec.Code, rec.Body)
	}
	// Models failing says "совет", not "фразу".
	srv, _ = adviceServer(t, heavyWeek(), &fakeAdvisor{err: &llm.ExhaustedError{Calls: 2, Last: "limit"}}, okQuota, nil)
	if rec := post(srv.Handler(), "/v1/advice", "", ""); rec.Code != 503 || !strings.Contains(rec.Body.String(), "получить совет") {
		t.Errorf("exhausted: %d %s", rec.Code, rec.Body)
	}
	// Calendars down: no advice from a blind summary.
	srv, _ = adviceServer(t, nil, &fakeAdvisor{}, okQuota, &fakeCollector{warnings: []string{"google: down"}})
	if rec := post(srv.Handler(), "/v1/advice", "", ""); rec.Code != 502 {
		t.Errorf("calendars down: %d %s", rec.Code, rec.Body)
	}
}

func TestAdviceSettingsAreValidated(t *testing.T) {
	srv, _ := adviceServer(t, nil, &fakeAdvisor{}, okQuota, nil)
	h := srv.Handler()
	for _, bad := range []string{`{"advice":{"interval_days":0}}`, `{"advice":{"x":1}}`, `{"advice":[]}`} {
		if rec := request(h, "PUT", "/v1/settings", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, rec.Code)
		}
	}
}
