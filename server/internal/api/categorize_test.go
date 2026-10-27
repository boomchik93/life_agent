package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lifeagent/internal/calendar"
	"lifeagent/internal/llm"
	"lifeagent/internal/store"
)

type fakeCaller struct {
	answer string
	calls  int
	user   string
}

func (f *fakeCaller) Call(_ context.Context, in llm.Input) (llm.Output, error) {
	f.calls++
	f.user = in.User
	if err := in.Check(f.answer); err != nil {
		return llm.Output{}, &llm.ExhaustedError{Calls: 1, Last: "check_failed"}
	}
	return llm.Output{JSON: f.answer, Model: "m", Calls: 1}, nil
}

func categorizeServer(t *testing.T, c *fakeCaller) (*Server, *store.Store, *logged) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	log := &logged{}
	opts := Options{
		Store: st, Token: testToken, Logger: slog.New(slog.NewTextHandler(log, nil)),
		RatePerSec: 100, Burst: 100, Location: yekat,
		LLM: &LLMInfo{Quota: fakeQuota{q: llm.Quota{Remaining: 40}}},
		Now: func() time.Time { return at(28, 12, 0) },
	}
	if c != nil {
		opts.Caller = c
	}
	return NewServer(opts), st, log
}

type categorizeBody struct {
	Categories map[string]struct {
		Category string  `json:"category"`
		Origin   string  `json:"origin"`
		Resolved bool    `json:"resolved"`
		Invalid  bool    `json:"invalid"`
		Conf     float64 `json:"confidence"`
	} `json:"categories"`
}

func TestCategorizeLayers(t *testing.T) {
	c := &fakeCaller{answer: `{"items":[{"key":"com.weird.app","category":"sport","confidence":0.85}]}`}
	srv, st, log := categorizeServer(t, c)
	h := srv.Handler()

	code, body := send(h, "POST", "/v1/categorize", `{"names":["GitHub.com","com.weird.app","https://secret.example/page?token=abc"]}`)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	var res categorizeBody
	json.Unmarshal([]byte(body), &res)
	if g := res.Categories["GitHub.com"]; g.Category != "career" || g.Origin != "dictionary" || !g.Resolved {
		t.Errorf("dictionary: %+v", g)
	}
	if w := res.Categories["com.weird.app"]; w.Category != "sport" || w.Origin != "llm" {
		t.Errorf("model: %+v", w)
	}
	if u := res.Categories["https://secret.example/page?token=abc"]; !u.Invalid || u.Category != "unknown" {
		t.Errorf("a URL is not a name: %+v", u)
	}
	if strings.Contains(c.user, "secret.example") || strings.Contains(c.user, "github") {
		t.Errorf("the model got something it must not see:\n%s", c.user)
	}
	cached, _ := st.AppCategories([]string{"github.com", "com.weird.app"})
	if len(cached) != 2 {
		t.Errorf("cache: %+v", cached)
	}
	if strings.Contains(log.String(), "secret.example") || strings.Contains(log.String(), "weird") {
		t.Errorf("names reached the log: %s", log.String())
	}
	if all := dumpDB(t, st); strings.Contains(all, "secret.example") {
		t.Error("a URL reached the database")
	}
}

func TestCategorizeSwitchKeepsNamesAtHome(t *testing.T) {
	c := &fakeCaller{answer: `{"items":[{"key":"com.weird.app","category":"sport","confidence":0.85}]}`}
	srv, _, _ := categorizeServer(t, c)
	h := srv.Handler()
	if code, _ := send(h, "PUT", "/v1/settings", `{"categorize":{"llm":false}}`); code != 200 {
		t.Fatalf("settings: %d", code)
	}
	code, body := send(h, "POST", "/v1/categorize", `{"names":["com.weird.app","github.com"]}`)
	var res categorizeBody
	json.Unmarshal([]byte(body), &res)
	if code != 200 || c.calls != 0 || res.Categories["com.weird.app"].Resolved || res.Categories["github.com"].Category != "career" {
		t.Errorf("%d calls %d %s", code, c.calls, body)
	}
	if code, _ := send(h, "PUT", "/v1/settings", `{"categorize":{"x":1}}`); code != 400 {
		t.Errorf("bad setting accepted: %d", code)
	}
}

func TestCategorizeRequestShape(t *testing.T) {
	srv, _, _ := categorizeServer(t, nil)
	h := srv.Handler()
	many := make([]string, 101)
	for i := range many {
		many[i] = "app"
	}
	manyJSON, _ := json.Marshal(map[string]any{"names": many})
	for name, body := range map[string]string{
		"empty": `{"names":[]}`, "not json": `[`, "extra": `{"names":["a.b"],"usage":[1]}`,
		"too many": string(manyJSON), "numbers": `{"names":[1]}`,
	} {
		if code, _ := send(h, "POST", "/v1/categorize", body); code != 400 {
			t.Errorf("%s: %d", name, code)
		}
	}
	// Without a model the dictionary still answers.
	code, body := send(h, "POST", "/v1/categorize", `{"names":["youtube.com","com.unknown.thing"]}`)
	var res categorizeBody
	json.Unmarshal([]byte(body), &res)
	if code != 200 || res.Categories["youtube.com"].Category != "leisure" || res.Categories["com.unknown.thing"].Resolved {
		t.Errorf("%d %s", code, body)
	}
}

func TestWeekEventsCarryTheirCategory(t *testing.T) {
	events := []calendar.Event{
		{ID: "1", Source: "ics", Calendar: "Расписание", Title: "Пара", Start: at(28, 9, 0), End: at(28, 10, 30)},
		{ID: "2", Source: "google", Calendar: "КВН", Title: "Репетиция", Start: at(28, 18, 0), End: at(28, 20, 0)},
		{ID: "3", Source: "google", Calendar: "Другое", Title: "Что-то", Start: at(28, 21, 0), End: at(28, 22, 0)},
	}
	h := writeServer(t, events, &fakeWriter{})
	request(h, "PUT", "/v1/settings", `{"calendar_categories":{"КВН":"hobby"}}`)
	var week weekResponse
	decodeJSON(t, request(h, "GET", "/v1/week", ""), &week)
	got := map[string]string{}
	for _, e := range week.Events {
		got[e.ID] = e.Category
	}
	if got["1"] != "study" || got["2"] != "hobby" || got["3"] != "other" {
		t.Errorf("categories: %v", got)
	}
}
