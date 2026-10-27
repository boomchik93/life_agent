package steam

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"lifeagent/internal/activity"
	"lifeagent/internal/store"
)

var yekat, _ = time.LoadLocation("Asia/Yekaterinburg")

func at(day, h, m int) time.Time { return time.Date(2026, 10, day, h, m, 0, 0, yekat) }

// fakeSteam serves the two Steam hosts. games is replaced between polls.
type fakeSteam struct {
	mu        sync.Mutex
	games     map[int64]int
	private   bool
	genres    map[int64][]string
	keys      []string // the keys it saw, to prove they stay out of storage
	genreHits int
}

func (f *fakeSteam) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.keys = append(f.keys, r.URL.Query().Get("key"))
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "GetOwnedGames"):
			if f.private {
				fmt.Fprint(w, `{"response":{}}`)
				return
			}
			var list []string
			for id, m := range f.games {
				list = append(list, fmt.Sprintf(`{"appid":%d,"playtime_forever":%d}`, id, m))
			}
			fmt.Fprintf(w, `{"response":{"game_count":%d,"games":[%s]}}`, len(list), strings.Join(list, ","))
		case strings.Contains(r.URL.Path, "GetRecentlyPlayedGames"):
			fmt.Fprint(w, `{"response":{"total_count":0}}`)
		case r.URL.Path == "/api/appdetails":
			f.genreHits++
			id := r.URL.Query().Get("appids")
			var appid int64
			fmt.Sscan(id, &appid)
			var gs []string
			for _, g := range f.genres[appid] {
				gs = append(gs, fmt.Sprintf(`{"description":%q}`, g))
			}
			fmt.Fprintf(w, `{%q:{"success":true,"data":{"genres":[%s]}}}`, id, strings.Join(gs, ","))
		default:
			w.WriteHeader(404)
		}
	})
}

func setup(t *testing.T) (*Collector, *fakeSteam, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	f := &fakeSteam{games: map[int64]int{}, genres: map[int64][]string{570: {"Action", "Free To Play"}, 440: {"RPG"}}}
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	c := New("SECRET-STEAM-KEY", "76561198000000000", st, yekat)
	c.APIBase, c.StoreBase = srv.URL, srv.URL
	return c, f, st
}

func collect(t *testing.T, c *Collector, now time.Time) map[string]activity.Row {
	t.Helper()
	days, err := c.Collect(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]activity.Row{}
	for _, d := range days {
		for _, r := range d.Rows {
			out[d.Date] = r
		}
	}
	return out
}

func TestDailyHoursAreTheDifferenceOfCumulativeTime(t *testing.T) {
	c, f, _ := setup(t)

	// The first poll is a starting point: old play time is not today's.
	f.games = map[int64]int{570: 1000, 730: 500}
	if rows := collect(t, c, at(27, 10, 0)); len(rows) != 0 {
		t.Fatalf("the first poll counted old play time: %+v", rows)
	}

	f.games[570] = 1090
	rows := collect(t, c, at(27, 10, 30))
	r := rows["2026-10-27"]
	if r.Category != "leisure" || r.Minutes != 90 || r.Count != 1 || r.Confidence != 1 {
		t.Fatalf("today after 90 minutes: %+v", rows)
	}

	// Polling again with no change adds nothing; a smaller number is ignored.
	f.games[730] = 400
	if again := collect(t, c, at(27, 11, 0))["2026-10-27"]; again.Minutes != 90 {
		t.Errorf("a glitch changed the day: %+v", again)
	}

	// The next morning: yesterday is final, a new game starts from nothing.
	f.games[570] = 1100
	f.games[440] = 30
	rows = collect(t, c, at(28, 9, 0))
	if rows["2026-10-27"].Minutes != 90 {
		t.Errorf("yesterday: %+v", rows["2026-10-27"])
	}
	today := rows["2026-10-28"]
	if today.Minutes != 40 || today.Count != 2 {
		t.Errorf("today: %+v", today)
	}
}

func TestGenresAreKeptShortAndAllowed(t *testing.T) {
	c, f, _ := setup(t)
	f.games = map[int64]int{570: 100, 440: 100}
	collect(t, c, at(27, 10, 0))
	f.games[570], f.games[440] = 160, 130
	r := collect(t, c, at(27, 11, 0))["2026-10-27"]
	genres, _ := r.Extra["genres"].([]any)
	if len(genres) != 3 || genres[0] != "Action" { // 570 was played longer
		t.Errorf("genres: %v", r.Extra)
	}
	if err := activity.ValidateExtra(r.Extra); err != nil {
		t.Errorf("the row would be refused: %v", err)
	}
	hits := f.genreHits
	f.games[570] = 170
	collect(t, c, at(27, 12, 0))
	if f.genreHits != hits {
		t.Errorf("genres are cached: %d lookups became %d", hits, f.genreHits)
	}
}

func TestPrivateProfileIsAnError(t *testing.T) {
	c, f, _ := setup(t)
	f.private = true
	_, err := c.Collect(context.Background(), at(27, 10, 0))
	if err != ErrPrivate {
		t.Fatalf("err = %v", err)
	}
}

func TestTheKeyNeverLeaksIntoErrorsOrStorage(t *testing.T) {
	c, f, st := setup(t)
	f.games = map[int64]int{570: 100}
	collect(t, c, at(27, 10, 0))
	f.games[570] = 200
	collect(t, c, at(27, 11, 0))

	// The API is down: the error must not carry the URL with the key.
	down := New("SECRET-STEAM-KEY", "76561198000000000", st, yekat)
	down.APIBase = "http://127.0.0.1:1"
	_, err := down.Collect(context.Background(), at(27, 12, 0))
	if err == nil || strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("error: %v", err)
	}
	if !contains(f.keys, "SECRET-STEAM-KEY") {
		t.Fatal("the test never sent the key")
	}
	rows, _ := st.DB.Query(`SELECT * FROM steam_snapshots`)
	defer rows.Close()
	for rows.Next() {
		var a int64
		var ta string
		var m, b int
		rows.Scan(&a, &ta, &m, &b)
		if strings.Contains(ta, "SECRET") {
			t.Error("key in the snapshots")
		}
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func TestRunStoresSteamDaysThroughTheRunner(t *testing.T) {
	c, f, st := setup(t)
	f.games = map[int64]int{570: 100}
	if err := activity.Run(context.Background(), c, at(27, 10, 0), st); err != nil {
		t.Fatal(err)
	}
	f.games[570] = 160
	if err := activity.Run(context.Background(), c, at(27, 11, 0), st); err != nil {
		t.Fatal(err)
	}
	rows, _ := st.Activity("2026-10-01", "2026-10-31", "steam")
	if len(rows) != 1 || rows[0].Minutes != 60 || rows[0].Category != "leisure" || !strings.Contains(rows[0].Extra, "Action") {
		t.Fatalf("rows: %+v", rows)
	}

	// A private profile marks the source as failed and keeps the old days.
	f.private = true
	if err := activity.Run(context.Background(), c, at(27, 12, 0), st); err == nil {
		t.Fatal("expected an error")
	}
	states, _ := st.SourceStates([]string{"steam"})
	if states[0].LastError == nil || !strings.Contains(*states[0].LastError, "private") {
		t.Errorf("state: %+v", states[0])
	}
	if rows, _ := st.Activity("2026-10-01", "2026-10-31", "steam"); len(rows) != 1 {
		t.Error("old days must stay")
	}

	// A switched-off source is not even asked.
	st.SetSourceEnabled("steam", false)
	f.private = false
	f.games[570] = 500
	activity.Run(context.Background(), c, at(27, 13, 0), st)
	if rows, _ := st.Activity("2026-10-01", "2026-10-31", "steam"); rows[0].Minutes != 60 {
		t.Errorf("a disabled source changed data: %+v", rows)
	}
}
