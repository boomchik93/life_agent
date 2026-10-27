package activity

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lifeagent/internal/store"
)

func TestValidateExtra(t *testing.T) {
	good := []map[string]any{
		nil,
		{"temp_c": 12.5, "precip_mm": 0.0, "daylight_hours": 9.5},
		{"genres": []any{"RPG", "Action-Adventure", "Strategy & Tactics"}},
		{"commits": 5.0, "prs": 1.0},
	}
	for _, e := range good {
		if err := ValidateExtra(e); err != nil {
			t.Errorf("%v rejected: %v", e, err)
		}
	}
	bad := map[string]map[string]any{
		"title key":        {"title": "Secret doc - Google Docs"},
		"url key":          {"url": "https://example.com/private"},
		"window":           {"window": "x"},
		"text in a number": {"temp_c": "https://example.com"},
		"url as a genre":   {"genres": []any{"https://example.com/private?id=1"}},
		"long genre":       {"genres": []any{strings.Repeat("a", 60)}},
		"six genres":       {"genres": []any{"a", "b", "c", "d", "e", "f"}},
		"genre not text":   {"genres": []any{5.0}},
		"genres as text":   {"genres": "RPG"},
	}
	for name, e := range bad {
		if err := ValidateExtra(e); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestRowValidate(t *testing.T) {
	ok := Row{Category: Study, Minutes: 60, NightMinutes: 10, Confidence: 0.9}
	if err := ok.Validate(AndroidUsage); err != nil {
		t.Fatal(err)
	}
	for name, r := range map[string]Row{
		"category":   {Category: "gaming", Minutes: 1},
		"minutes":    {Category: Study, Minutes: 1441},
		"negative":   {Category: Study, Minutes: -1},
		"night":      {Category: Study, Minutes: 10, NightMinutes: 11},
		"confidence": {Category: Study, Minutes: 1, Confidence: 1.5},
		"extra":      {Category: Study, Minutes: 1, Extra: map[string]any{"title": "x"}},
	} {
		if r.Validate(Steam) == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if !ClientSource(AndroidUsage) || !ClientSource(MacActivityWatch) || ClientSource(Steam) {
		t.Error("only the phone and the Mac send their own sums")
	}
}

type fakeCollector struct {
	source string
	days   []DayRows
	err    error
	calls  int
}

func (f *fakeCollector) Source() string { return f.source }
func (f *fakeCollector) Collect(context.Context, time.Time) ([]DayRows, error) {
	f.calls++
	return f.days, f.err
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

var now = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

func TestRunStoresDaysAndReplacesThem(t *testing.T) {
	st := openStore(t)
	c := &fakeCollector{source: Steam, days: []DayRows{{Date: "2026-10-28", Rows: []Row{
		{Category: Leisure, Minutes: 120, Extra: map[string]any{"genres": []any{"RPG"}}},
	}}}}
	if err := Run(context.Background(), c, now, st); err != nil {
		t.Fatal(err)
	}
	rows, _ := st.Activity("2026-10-01", "2026-10-31", Steam)
	if len(rows) != 1 || rows[0].Minutes != 120 || rows[0].Confidence != 1 || !strings.Contains(rows[0].Extra, "RPG") {
		t.Fatalf("rows: %+v", rows)
	}
	c.days[0].Rows = []Row{{Category: Leisure, Minutes: 30}}
	if err := Run(context.Background(), c, now, st); err != nil {
		t.Fatal(err)
	}
	rows, _ = st.Activity("2026-10-01", "2026-10-31", Steam)
	if len(rows) != 1 || rows[0].Minutes != 30 {
		t.Fatalf("a new summary replaces the day: %+v", rows)
	}
	states, _ := st.SourceStates([]string{Steam})
	if states[0].LastSync == nil || states[0].LastError != nil {
		t.Errorf("state: %+v", states[0])
	}
}

func TestRunSkipsADisabledSource(t *testing.T) {
	st := openStore(t)
	st.SetSourceEnabled(Steam, false)
	c := &fakeCollector{source: Steam, days: []DayRows{{Date: "2026-10-28", Rows: []Row{{Category: Leisure, Minutes: 5}}}}}
	if err := Run(context.Background(), c, now, st); err != nil || c.calls != 0 {
		t.Fatalf("err %v calls %d", err, c.calls)
	}
	if rows, _ := st.Activity("2026-01-01", "2026-12-31", ""); len(rows) != 0 {
		t.Errorf("a disabled source wrote: %+v", rows)
	}
}

func TestRunRecordsFailuresWithoutURLs(t *testing.T) {
	st := openStore(t)
	st.ReplaceActivity("2026-10-27", Steam, []store.ActivityRow{{Category: Leisure, Minutes: 60, Confidence: 1}})
	c := &fakeCollector{source: Steam, err: fmt.Errorf(`Get "https://api.steampowered.com/x?key=SECRETKEY&steamid=1": timeout`)}
	err := Run(context.Background(), c, now, st)
	if err == nil || strings.Contains(err.Error(), "SECRETKEY") {
		t.Fatalf("error leaks the key: %v", err)
	}
	states, _ := st.SourceStates([]string{Steam})
	if states[0].LastError == nil || strings.Contains(*states[0].LastError, "SECRETKEY") || !strings.Contains(*states[0].LastError, "<url>") {
		t.Errorf("state: %+v", states[0])
	}
	if rows, _ := st.Activity("2026-10-27", "2026-10-27", Steam); len(rows) != 1 {
		t.Error("a failed round must leave the old days")
	}
	c.err, c.days = nil, []DayRows{{Date: "2026-10-28", Rows: []Row{{Category: Leisure, Minutes: 5}}}}
	Run(context.Background(), c, now, st)
	if states, _ = st.SourceStates([]string{Steam}); states[0].LastError != nil {
		t.Errorf("a success clears the error: %+v", states[0])
	}
}

func TestRunRejectsABadRoundAsAWhole(t *testing.T) {
	st := openStore(t)
	c := &fakeCollector{source: Steam, days: []DayRows{
		{Date: "2026-10-27", Rows: []Row{{Category: Leisure, Minutes: 10}}},
		{Date: "2026-10-28", Rows: []Row{{Category: Leisure, Minutes: 10, Extra: map[string]any{"title": "Secret"}}}},
	}}
	if err := Run(context.Background(), c, now, st); err == nil {
		t.Fatal("a row with a forbidden key must fail the round")
	}
	if rows, _ := st.Activity("2026-01-01", "2026-12-31", ""); len(rows) != 0 {
		t.Errorf("half a round was written: %+v", rows)
	}
	for name, days := range map[string][]DayRows{
		"duplicate category": {{Date: "2026-10-28", Rows: []Row{{Category: Study, Minutes: 1}, {Category: Study, Minutes: 2}}}},
		"bad date":           {{Date: "28.10", Rows: []Row{{Category: Study, Minutes: 1}}}},
	} {
		c.days = days
		if Run(context.Background(), c, now, st) == nil {
			t.Errorf("%s accepted", name)
		}
	}
	c.err = errors.New("x")
	if Run(context.Background(), c, now, st) == nil {
		t.Error("collector error lost")
	}
}
