package macagent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

var loc, _ = time.LoadLocation("Asia/Yekaterinburg")

func at(h, m int) time.Time { return time.Date(2026, 10, 28, h, m, 0, 0, loc) }

func ev(from time.Time, dur time.Duration, data map[string]any) rawEvent {
	return rawEvent{Timestamp: from, Duration: dur.Seconds(), Data: data}
}

func TestDomain(t *testing.T) {
	for in, want := range map[string]string{
		"https://www.GitHub.com/user/repo?tab=x#y": "github.com",
		"http://docs.google.com/document/d/abc":    "docs.google.com",
		"https://localhost:3000/app":               "localhost",
		"chrome://settings":                        "",
		"file:///Users/me/secret.txt":              "",
		"about:blank":                              "",
		"not a url":                                "",
		"https://intranet/page":                    "",
		"":                                         "",
	} {
		if got := Domain(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestBuildSegments(t *testing.T) {
	win := func(app, title string, from time.Time, dur time.Duration) rawEvent {
		return ev(from, dur, map[string]any{"app": app, "title": title})
	}
	afk := func(status string, from time.Time, dur time.Duration) rawEvent {
		return ev(from, dur, map[string]any{"status": status})
	}
	page := func(url string, incognito bool, from time.Time, dur time.Duration) rawEvent {
		return ev(from, dur, map[string]any{"url": url, "title": "TITLE", "incognito": incognito})
	}
	from, to := at(0, 0), at(23, 59)

	t.Run("away time is not counted", func(t *testing.T) {
		segs := BuildSegments(
			[]rawEvent{win("Code", "x", at(10, 0), time.Hour)},
			[]rawEvent{afk("not-afk", at(10, 0), 30*time.Minute), afk("afk", at(10, 30), 30*time.Minute)},
			true, nil, from, to)
		if len(segs) != 1 || segs[0].Key != "code" || segs[0].Span.Minutes() != 30 {
			t.Errorf("%+v", segs)
		}
	})

	t.Run("without the afk watcher all window time counts", func(t *testing.T) {
		segs := BuildSegments([]rawEvent{win("Code", "x", at(10, 0), time.Hour)}, nil, false, nil, from, to)
		if len(segs) != 1 || segs[0].Span.Minutes() != 60 {
			t.Errorf("%+v", segs)
		}
	})

	t.Run("a browser's time goes to the domains it showed", func(t *testing.T) {
		segs := BuildSegments(
			[]rawEvent{win("Google Chrome", "T1", at(10, 0), time.Hour)},
			nil, false,
			[]rawEvent{
				page("https://github.com/me/private-repo?x=1", false, at(10, 10), 20*time.Minute),
				page("https://news.example/a/b", false, at(10, 40), 10*time.Minute),
				page("https://secret.example/incognito", true, at(10, 50), 5*time.Minute),
			}, from, to)
		minutes := map[string]float64{}
		browser := 0.0
		for _, s := range segs {
			if s.Browser {
				browser += s.Span.Minutes()
				continue
			}
			minutes[s.Key] += s.Span.Minutes()
		}
		if minutes["github.com"] != 20 || minutes["news.example"] != 10 || len(minutes) != 2 {
			t.Errorf("domains: %v", minutes)
		}
		if browser != 30 {
			t.Errorf("browser time with no domain: %v, want 30 (10 + 10 + 10 incl. incognito)", browser)
		}
		for _, s := range segs {
			if strings.Contains(s.Key, "secret") || strings.Contains(s.Key, "private") {
				t.Errorf("a path or an incognito page leaked into %q", s.Key)
			}
		}
	})

	t.Run("a window with no app name is skipped", func(t *testing.T) {
		if segs := BuildSegments([]rawEvent{win("", "x", at(10, 0), time.Hour)}, nil, false, nil, from, to); len(segs) != 0 {
			t.Errorf("%+v", segs)
		}
	})
}

type fakeServer struct {
	known map[string]NameCategory
	calls []struct {
		names []string
		llm   bool
	}
}

func (f *fakeServer) categorize(_ context.Context, names []string, llm bool) (map[string]NameCategory, error) {
	f.calls = append(f.calls, struct {
		names []string
		llm   bool
	}{names, llm})
	out := map[string]NameCategory{}
	for _, n := range names {
		if g, ok := f.known[n]; ok {
			out[n] = g
		} else {
			out[n] = NameCategory{Category: "unknown"}
		}
	}
	return out, nil
}

func minutesOf(rows []Row) map[string]int {
	m := map[string]int{}
	for _, r := range rows {
		m[r.Category] = r.Minutes
	}
	return m
}

func TestSummarize(t *testing.T) {
	day := at(0, 0)
	seg := func(key string, from, to time.Time) Seg { return Seg{Key: key, Span: Span{from, to}} }
	ctx := context.Background()

	t.Run("the dictionary answers, the model is not asked", func(t *testing.T) {
		srv := &fakeServer{known: map[string]NameCategory{"code": {Category: "career", Confidence: 1, Resolved: true}}}
		rows, err := Summarize(ctx, day, []Seg{seg("code", at(10, 0), at(12, 0))}, nil, srv.categorize, 23, 7)
		if err != nil || minutesOf(rows)["career"] != 120 || len(srv.calls) != 1 || srv.calls[0].llm {
			t.Fatalf("%v %v %+v", rows, err, srv.calls)
		}
	})

	t.Run("context first, then the model for the rest", func(t *testing.T) {
		srv := &fakeServer{}
		srv.known = map[string]NameCategory{}
		calls := 0
		cat := func(c context.Context, names []string, llm bool) (map[string]NameCategory, error) {
			calls++
			if llm {
				return map[string]NameCategory{"weird": {Category: "leisure", Confidence: 0.8, Resolved: true}}, nil
			}
			return srv.categorize(c, names, llm)
		}
		rows, err := Summarize(ctx, day, []Seg{seg("weird", at(9, 0), at(11, 0))},
			[]ContextEvent{{Span{at(10, 0), at(12, 0)}, "study"}}, cat, 23, 7)
		if err != nil {
			t.Fatal(err)
		}
		if m := minutesOf(rows); m["study"] != 60 || m["leisure"] != 60 || len(m) != 2 || calls != 2 {
			t.Errorf("%v calls %d", rows, calls)
		}
	})

	t.Run("browser time without a domain is unknown and nobody is asked", func(t *testing.T) {
		srv := &fakeServer{}
		rows, _ := Summarize(ctx, day, []Seg{{Key: "safari", Span: Span{at(10, 0), at(10, 30)}, Browser: true}}, nil, srv.categorize, 23, 7)
		if minutesOf(rows)["unknown"] != 30 || len(srv.calls) != 0 {
			t.Errorf("%v %+v", rows, srv.calls)
		}
	})

	t.Run("night minutes and the cut at midnight", func(t *testing.T) {
		srv := &fakeServer{known: map[string]NameCategory{"game": {Category: "leisure", Confidence: 0.9, Resolved: true}}}
		rows, _ := Summarize(ctx, day, []Seg{
			seg("game", at(0, 0).Add(-time.Hour), at(1, 0)),     // 1 h of this day, at night
			seg("game", at(22, 30), at(0, 30).AddDate(0, 0, 1)), // 90 min of this day, 60 at night
		}, nil, srv.categorize, 23, 7)
		if len(rows) != 1 || rows[0].Minutes != 150 || rows[0].NightMinutes != 120 || rows[0].Confidence != 0.9 {
			t.Errorf("%+v", rows)
		}
	})

	t.Run("a name the server answered unknown is not sent to the model", func(t *testing.T) {
		srv := &fakeServer{known: map[string]NameCategory{"odd": {Category: "unknown", Confidence: 0.3, Resolved: true}}}
		rows, _ := Summarize(ctx, day, []Seg{seg("odd", at(10, 0), at(11, 0))}, nil, srv.categorize, 23, 7)
		if minutesOf(rows)["unknown"] != 60 || len(srv.calls) != 1 {
			t.Errorf("%v %+v", rows, srv.calls)
		}
	})
}

func TestFileVaultOn(t *testing.T) {
	if !FileVaultOn("FileVault is On.\n") || FileVaultOn("FileVault is Off.\n") || FileVaultOn("") {
		t.Error("fdesetup output misread")
	}
}

// fakeLife records every request the agent makes.
type fakeLife struct {
	mu     sync.Mutex
	bodies []string
	posts  []map[string]any
	known  map[string]map[string]any
}

func (f *fakeLife) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.bodies = append(f.bodies, r.URL.String()+" "+string(body))
	f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer tok" {
		w.WriteHeader(401)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/v1/week":
		fmt.Fprint(w, `{"events":[{"start":"2026-10-28T10:00:00+05:00","end":"2026-10-28T11:00:00+05:00","all_day":false,"category":"study"}]}`)
	case "/v1/categorize":
		var in struct{ Names []string }
		json.Unmarshal(body, &in)
		out := map[string]any{}
		for _, n := range in.Names {
			if c, ok := f.known[n]; ok {
				out[n] = c
			} else {
				out[n] = map[string]any{"category": "unknown", "confidence": 0, "resolved": false}
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"categories": out})
	case "/v1/activity/daily":
		var m map[string]any
		json.Unmarshal(body, &m)
		f.mu.Lock()
		f.posts = append(f.posts, m)
		f.mu.Unlock()
		fmt.Fprint(w, `{"ok":true}`)
	default:
		w.WriteHeader(404)
	}
}

// fakeAW serves buckets and events that carry titles and URLs with markers.
func fakeAW(t *testing.T, titles ...string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/0/buckets/":
			fmt.Fprint(w, `{"aw-watcher-window_mac":{"type":"currentwindow"},"aw-watcher-afk_mac":{"type":"afkstatus"},"aw-watcher-web-chrome":{"type":"web.tab.current"}}`)
		case strings.HasSuffix(r.URL.Path, "aw-watcher-window_mac/events"):
			fmt.Fprintf(w, `[{"timestamp":"2026-10-28T10:00:00+05:00","duration":3600,"data":{"app":"Google Chrome","title":%q}},
				{"timestamp":"2026-10-28T12:00:00+05:00","duration":1800,"data":{"app":"Visual Studio Code","title":%q}}]`, titles[0], titles[1])
		case strings.HasSuffix(r.URL.Path, "aw-watcher-afk_mac/events"):
			fmt.Fprint(w, `[{"timestamp":"2026-10-28T09:00:00+05:00","duration":18000,"data":{"status":"not-afk"}}]`)
		case strings.HasSuffix(r.URL.Path, "aw-watcher-web-chrome/events"):
			fmt.Fprintf(w, `[{"timestamp":"2026-10-28T10:00:00+05:00","duration":1800,"data":{"url":%q,"title":%q,"incognito":false}}]`, titles[2], titles[0])
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// PLAN.md stage 10.2: the request bodies hold no titles and no full URLs.
func TestNothingButCategoriesLeavesTheMac(t *testing.T) {
	const (
		title1 = "SECRET-TITLE-private-document"
		title2 = "SECRET-TITLE-main.go - confidential-project"
		url    = "https://secret-site.example/private/path?token=SECRET-QUERY"
	)
	aw := fakeAW(t, title1, title2, url)
	life := &fakeLife{known: map[string]map[string]any{
		"visual studio code":  {"category": "career", "confidence": 1, "resolved": true},
		"secret-site.example": {"category": "leisure", "confidence": 0.9, "resolved": true},
	}}
	ls := httptest.NewServer(life)
	defer ls.Close()

	agent := &Agent{
		AW: &AW{BaseURL: aw.URL}, Life: &Life{BaseURL: ls.URL, Token: "tok"}, Loc: loc,
		Now: func() time.Time { return time.Date(2026, 10, 28, 15, 0, 0, 0, loc) },
	}
	rep, err := agent.SyncDay(context.Background(), at(12, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(life.posts) != 1 {
		t.Fatalf("posts: %d", len(life.posts))
	}

	post := life.posts[0]
	if post["source"] != "mac_activitywatch" || post["date"] != "2026-10-28" {
		t.Errorf("post: %v", post)
	}
	rows := post["rows"].([]any)
	got := map[string]float64{}
	for _, r := range rows {
		m := r.(map[string]any)
		got[m["category"].(string)] = m["minutes"].(float64)
		for k := range m {
			if k != "category" && k != "minutes" && k != "night_minutes" && k != "confidence" {
				t.Errorf("unexpected field %q in a row", k)
			}
		}
	}
	// 10:00-10:30 on the secret domain, inside the 10:00-11:00 study event it is
	// still a known category; the other 30 browser minutes have no domain.
	if got["leisure"] != 30 || got["career"] != 30 || got["unknown"] != 30 {
		t.Errorf("minutes: %v", got)
	}
	if rep.UnknownPercent != 33 {
		t.Errorf("unknown share %d", rep.UnknownPercent)
	}

	if len(life.bodies) == 0 {
		t.Fatal("the agent sent nothing")
	}
	for _, b := range life.bodies {
		for _, secret := range []string{"SECRET-TITLE", "SECRET-QUERY", "/private/path", "confidential-project", "https://", "token="} {
			if strings.Contains(b, secret) {
				t.Errorf("%q leaves the Mac in: %s", secret, b)
			}
		}
		if strings.Contains(b, "/v1/activity/daily") {
			for _, name := range []string{"secret-site", "chrome", "visual studio"} {
				if strings.Contains(strings.ToLower(b), name) {
					t.Errorf("a name %q is in the summary: %s", name, b)
				}
			}
		}
	}
}

func TestSyncReportsReadableErrors(t *testing.T) {
	life := &fakeLife{}
	ls := httptest.NewServer(life)
	defer ls.Close()
	now := func() time.Time { return time.Date(2026, 10, 28, 15, 0, 0, 0, loc) }

	down := &Agent{AW: &AW{BaseURL: "http://127.0.0.1:1"}, Life: &Life{BaseURL: ls.URL, Token: "tok"}, Loc: loc, Now: now}
	if _, err := down.SyncDay(context.Background(), at(12, 0)); err == nil || !strings.Contains(err.Error(), "ActivityWatch is not reachable") {
		t.Errorf("AW down: %v", err)
	}

	aw := fakeAW(t, "a", "b", "https://x.example/")
	bad := &Agent{AW: &AW{BaseURL: aw.URL}, Life: &Life{BaseURL: ls.URL, Token: "wrong"}, Loc: loc, Now: now}
	if _, err := bad.SyncDay(context.Background(), at(12, 0)); err == nil {
		t.Error("a wrong token must be reported")
	}
}
