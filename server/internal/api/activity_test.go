package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lifeagent/internal/store"
)

// logged is the server's log output, for tests that look for what must not be in it.
type logged struct{ bytes.Buffer }

func activityServer(t *testing.T) (http.Handler, *store.Store, *logged) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	log := &logged{}
	h := New(Options{
		Store: st, Token: testToken, Logger: slog.New(slog.NewTextHandler(log, &slog.HandlerOptions{Level: slog.LevelDebug})),
		RatePerSec: 100, Burst: 100, Location: yekat,
		Now: func() time.Time { return at(28, 12, 0) },
	})
	return h, st, log
}

func send(h http.Handler, method, path, body string) (int, string) {
	rec := request(h, method, path, body)
	return rec.Code, rec.Body.String()
}

const phoneDay = `{"source":"android_usage","date":"2026-10-27","rows":[
	{"category":"study","minutes":120,"night_minutes":0},
	{"category":"leisure","minutes":200,"night_minutes":35,"confidence":0.8},
	{"category":"unknown","minutes":30}]}`

func TestActivityDailyStoresAndReplaces(t *testing.T) {
	h, st, _ := activityServer(t)
	if code, body := send(h, "POST", "/v1/activity/daily", phoneDay); code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	rows, _ := st.Activity("2026-10-01", "2026-10-31", "android_usage")
	if len(rows) != 3 {
		t.Fatalf("rows: %+v", rows)
	}
	var leisure store.ActivityRow
	for _, r := range rows {
		if r.Category == "leisure" {
			leisure = r
		}
	}
	if leisure.Minutes != 200 || leisure.NightMinutes != 35 || leisure.Confidence != 0.8 {
		t.Errorf("leisure: %+v", leisure)
	}

	// The same day again is the whole picture of that day.
	send(h, "POST", "/v1/activity/daily", `{"source":"android_usage","date":"2026-10-27","rows":[{"category":"study","minutes":10}]}`)
	rows, _ = st.Activity("2026-10-01", "2026-10-31", "android_usage")
	if len(rows) != 1 || rows[0].Minutes != 10 {
		t.Errorf("replace: %+v", rows)
	}

	var res struct{ Sources []store.SourceState }
	code, body := send(h, "GET", "/v1/sources", "")
	json.Unmarshal([]byte(body), &res)
	if code != 200 || len(res.Sources) != 6 {
		t.Fatalf("sources: %d %s", code, body)
	}
	for _, s := range res.Sources {
		if s.Source == "android_usage" && (s.LastSync == nil || !s.Enabled) {
			t.Errorf("phone state: %+v", s)
		}
		if s.Source == "steam" && (s.LastSync != nil || !s.Enabled) {
			t.Errorf("steam state: %+v", s)
		}
	}
}

func TestActivityDailyRejectsBadInput(t *testing.T) {
	h, _, _ := activityServer(t)
	rows := `[{"category":"study","minutes":5}]`
	for name, body := range map[string]string{
		"not json":         `[`,
		"empty rows":       `{"source":"android_usage","date":"2026-10-27","rows":[]}`,
		"server source":    `{"source":"steam","date":"2026-10-27","rows":` + rows + `}`,
		"unknown source":   `{"source":"tiktok","date":"2026-10-27","rows":` + rows + `}`,
		"future":           `{"source":"android_usage","date":"2026-10-29","rows":` + rows + `}`,
		"too old":          `{"source":"android_usage","date":"2024-01-01","rows":` + rows + `}`,
		"bad date":         `{"source":"android_usage","date":"27.10","rows":` + rows + `}`,
		"bad category":     `{"source":"android_usage","date":"2026-10-27","rows":[{"category":"games","minutes":5}]}`,
		"minutes > day":    `{"source":"android_usage","date":"2026-10-27","rows":[{"category":"study","minutes":1500}]}`,
		"night > minutes":  `{"source":"android_usage","date":"2026-10-27","rows":[{"category":"study","minutes":5,"night_minutes":6}]}`,
		"twice":            `{"source":"android_usage","date":"2026-10-27","rows":[{"category":"study","minutes":5},{"category":"study","minutes":6}]}`,
		"confidence 2":     `{"source":"android_usage","date":"2026-10-27","rows":[{"category":"study","minutes":5,"confidence":2}]}`,
		"extra field":      `{"source":"android_usage","date":"2026-10-27","rows":[{"category":"study","minutes":5,"extra":{}}]}`,
		"app name":         `{"source":"android_usage","date":"2026-10-27","rows":[{"category":"study","minutes":5,"app":"com.example"}]}`,
		"window title":     `{"source":"mac_activitywatch","date":"2026-10-27","title":"x","rows":` + rows + `}`,
		"url in a row":     `{"source":"mac_activitywatch","date":"2026-10-27","rows":[{"category":"study","minutes":5,"url":"https://a.b"}]}`,
		"too many rows":    `{"source":"android_usage","date":"2026-10-27","rows":[` + strings.Repeat(`{"category":"study","minutes":1},`, 9) + `{"category":"study","minutes":1}]}`,
		"number as source": `{"source":1,"date":"2026-10-27","rows":` + rows + `}`,
	} {
		if code, _ := send(h, "POST", "/v1/activity/daily", body); code != 400 {
			t.Errorf("%s: status %d", name, code)
		}
	}
}

// The tests of PLAN.md section 16: no window titles and no URLs in the
// database or in the logs, whatever a client sends.
func TestNoTitlesOrURLsInTheDatabaseOrLogs(t *testing.T) {
	h, st, log := activityServer(t)
	secrets := []string{"SECRET-WINDOW-TITLE", "https://secret.example/private/page?token=abc", "secret-app-name"}

	attacks := []string{
		// as an unknown field
		fmt.Sprintf(`{"source":"mac_activitywatch","date":"2026-10-27","window_title":%q,"rows":[{"category":"work","minutes":5}]}`, secrets[0]),
		fmt.Sprintf(`{"source":"mac_activitywatch","date":"2026-10-27","rows":[{"category":"work","minutes":5,"url":%q}]}`, secrets[1]),
		fmt.Sprintf(`{"source":"android_usage","date":"2026-10-27","rows":[{"category":"work","minutes":5,"app":%q}]}`, secrets[2]),
		// as a category
		fmt.Sprintf(`{"source":"mac_activitywatch","date":"2026-10-27","rows":[{"category":%q,"minutes":5}]}`, secrets[1]),
		// as the source, the date, the key of the source
		fmt.Sprintf(`{"source":%q,"date":"2026-10-27","rows":[{"category":"work","minutes":5}]}`, secrets[0]),
		fmt.Sprintf(`{"source":"android_usage","date":%q,"rows":[{"category":"work","minutes":5}]}`, secrets[1]),
	}
	for _, body := range attacks {
		code, resp := send(h, "POST", "/v1/activity/daily", body)
		if code != 400 {
			t.Errorf("accepted: %s", body)
		}
		for _, s := range secrets {
			if strings.Contains(resp, s) {
				t.Errorf("the response echoes %q", s)
			}
		}
	}
	// URL-shaped values in the path of source calls.
	send(h, "PUT", "/v1/sources/"+"SECRET-WINDOW-TITLE", `{"enabled":false}`)
	send(h, "DELETE", "/v1/activity?source=https://secret.example/private/page?token=abc", "")
	// A good request next to the bad ones.
	if code, body := send(h, "POST", "/v1/activity/daily", phoneDay); code != 200 {
		t.Fatalf("%d %s", code, body)
	}

	all := dumpDB(t, st)
	for _, s := range secrets {
		if strings.Contains(all, s) {
			t.Errorf("%q reached the database", s)
		}
		if strings.Contains(log.String(), s) {
			t.Errorf("%q reached the log", s)
		}
	}
	if len(all) == 0 {
		t.Fatal("the scan saw no data: the test checks nothing")
	}
}

func TestSwitchingASourceOff(t *testing.T) {
	h, st, _ := activityServer(t)
	if code, _ := send(h, "PUT", "/v1/sources/android_usage", `{"enabled":false}`); code != 200 {
		t.Fatalf("switch: %d", code)
	}
	code, body := send(h, "POST", "/v1/activity/daily", phoneDay)
	if code != 409 || !strings.Contains(body, "source_disabled") {
		t.Fatalf("a disabled source took data: %d %s", code, body)
	}
	if rows, _ := st.Activity("2026-01-01", "2026-12-31", ""); len(rows) != 0 {
		t.Errorf("rows: %+v", rows)
	}
	send(h, "PUT", "/v1/sources/android_usage", `{"enabled":true}`)
	if code, _ := send(h, "POST", "/v1/activity/daily", phoneDay); code != 200 {
		t.Errorf("after the switch is back: %d", code)
	}
	for body, want := range map[string]int{`{}`: 400, `{"enabled":"yes"}`: 400, `{"enabled":true,"x":1}`: 400} {
		if code, _ := send(h, "PUT", "/v1/sources/steam", body); code != want {
			t.Errorf("%s: %d", body, code)
		}
	}
	if code, _ := send(h, "PUT", "/v1/sources/nope", `{"enabled":true}`); code != 404 {
		t.Errorf("unknown source: %d", code)
	}
}

func TestDeletingASourceErasesItsRows(t *testing.T) {
	h, st, _ := activityServer(t)
	send(h, "POST", "/v1/activity/daily", phoneDay)
	send(h, "POST", "/v1/activity/daily", `{"source":"mac_activitywatch","date":"2026-10-27","rows":[{"category":"work","minutes":90}]}`)

	if code, _ := send(h, "DELETE", "/v1/activity", ""); code != 400 {
		t.Errorf("no source: %d", code)
	}
	if code, _ := send(h, "DELETE", "/v1/activity?source=tiktok", ""); code != 400 {
		t.Errorf("unknown source: %d", code)
	}
	code, body := send(h, "DELETE", "/v1/activity?source=android_usage", "")
	if code != 200 || !strings.Contains(body, `"deleted":3`) {
		t.Fatalf("%d %s", code, body)
	}
	if rows, _ := st.Activity("2026-01-01", "2026-12-31", "android_usage"); len(rows) != 0 {
		t.Errorf("the phone's rows survived: %+v", rows)
	}
	if rows, _ := st.Activity("2026-01-01", "2026-12-31", "mac_activitywatch"); len(rows) != 1 {
		t.Errorf("another source was touched: %+v", rows)
	}
	states, _ := st.SourceStates([]string{"android_usage", "mac_activitywatch"})
	if states[0].LastSync != nil || states[1].LastSync == nil {
		t.Errorf("sync state: %+v", states)
	}
}

func TestExportAndDeleteAll(t *testing.T) {
	h, st, _ := activityServer(t)
	send(h, "POST", "/v1/activity/daily", phoneDay)
	post(h, "/v1/checkin", "", `{"energy":4,"mood":3,"sleep_hours":7}`)
	post(h, "/v1/health/daily", "", `{"sleep_hours":7.5}`)
	addTask(t, h, "task-key-1", "Курсовая", "study", 60, "2026-10-30")
	st.AddAdvice(store.Advice{At: "2026-10-27T08:00:00Z", Reason: "asked", Text: "Ляг пораньше.", Model: "m", Source: "user", Status: "delivered"})

	code, body := send(h, "GET", "/v1/export", "")
	if code != 200 {
		t.Fatalf("export: %d", code)
	}
	var ex struct {
		Tables map[string][]map[string]any `json:"tables"`
	}
	if err := json.Unmarshal([]byte(body), &ex); err != nil {
		t.Fatal(err)
	}
	for table, want := range map[string]int{"activity_daily": 3, "checkins": 1, "health_daily": 1, "items": 1, "advice_log": 1} {
		if len(ex.Tables[table]) != want {
			t.Errorf("export %s: %d rows, want %d", table, len(ex.Tables[table]), want)
		}
	}

	for _, bad := range []string{"", `{}`, `{"confirm":"yes"}`, `[`} {
		if code, _ := send(h, "DELETE", "/v1/data", bad); code != 400 {
			t.Errorf("%q: %d", bad, code)
		}
	}
	if rows, _ := st.Activity("2026-01-01", "2026-12-31", ""); len(rows) != 3 {
		t.Fatal("a request without the confirmation deleted data")
	}

	put := request(h, "PUT", "/v1/settings", `{"calendar_categories":{"КВН":"hobby"}}`)
	if put.Code != 200 {
		t.Fatalf("settings: %d", put.Code)
	}
	code, body = send(h, "DELETE", "/v1/data", `{"confirm":"delete-all-data"}`)
	if code != 200 {
		t.Fatalf("delete: %d %s", code, body)
	}
	code, body = send(h, "GET", "/v1/export", "")
	json.Unmarshal([]byte(body), &ex)
	for table, rows := range ex.Tables {
		if table != "settings" && len(rows) != 0 {
			t.Errorf("%s still has %d rows", table, len(rows))
		}
	}
	if len(ex.Tables["settings"]) == 0 {
		t.Error("the settings are configuration and must stay")
	}
}

// dumpDB returns every value stored in the database as text.
func dumpDB(t *testing.T, st *store.Store) string {
	t.Helper()
	var all strings.Builder
	rows, err := st.DB.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		rows.Scan(&n)
		tables = append(tables, n)
	}
	rows.Close()
	for _, tbl := range tables {
		r, err := st.DB.Query(`SELECT * FROM ` + tbl)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := r.Columns()
		for r.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			r.Scan(ptrs...)
			fmt.Fprintln(&all, vals...)
		}
		r.Close()
	}
	return all.String()
}

func TestWeatherSettingIsChecked(t *testing.T) {
	h, _, _ := activityServer(t)
	if code, body := send(h, "PUT", "/v1/settings", `{"weather":{"city":"Екатеринбург"}}`); code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	for _, bad := range []string{`{"weather":{"city":""}}`, `{"weather":{"city":"x","lat":100,"lon":0}}`, `{"weather":{"city":"x","extra":1}}`, `{"weather":"Москва"}`} {
		if code, _ := send(h, "PUT", "/v1/settings", bad); code != 400 {
			t.Errorf("%s: %d", bad, code)
		}
	}
}
