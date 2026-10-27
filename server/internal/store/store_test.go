package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func open(t *testing.T) (*Store, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "t.db")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, p
}

func TestMigrateIdempotent(t *testing.T) {
	s, p := open(t)
	s.Close()
	s2, err := Open(p)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	var n int
	if err := s2.DB.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil || n != 4 {
		t.Fatalf("migrations applied = %d, err %v", n, err)
	}
}

func TestSettings(t *testing.T) {
	s, _ := open(t)
	if err := s.PutSettings(map[string]string{"a": `1`, "b": `"x"`}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutSettings(map[string]string{"a": `2`}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if got["a"] != "2" || got["b"] != `"x"` || len(got) != 2 {
		t.Fatalf("got %v", got)
	}
}

func TestBackupAndPrune(t *testing.T) {
	s, _ := open(t)
	s.PutSettings(map[string]string{"k": `"v"`})
	dir := filepath.Join(t.TempDir(), "backup")

	old := time.Now().Add(-20 * 24 * time.Hour)
	oldPath, err := s.Backup(dir, 14*24*time.Hour, old)
	if err != nil {
		t.Fatal(err)
	}
	os.Chtimes(oldPath, old, old)
	other := filepath.Join(dir, "keep-me.txt")
	os.WriteFile(other, []byte("x"), 0o600)
	os.Chtimes(other, old, old)

	newPath, err := s.Backup(dir, 14*24*time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Error("old backup not pruned")
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("unrelated file removed")
	}
	info, err := os.Stat(newPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("new backup: %v %v", err, info)
	}

	b, err := Open(newPath)
	if err != nil {
		t.Fatalf("backup not a valid db: %v", err)
	}
	defer b.Close()
	got, _ := b.Settings()
	if got["k"] != `"v"` {
		t.Errorf("backup content %v", got)
	}
}

func TestCheckins(t *testing.T) {
	s, _ := open(t)
	sleep := 6.5
	for _, c := range []Checkin{
		{Date: "2026-10-26", Energy: 3, Mood: 4},
		{Date: "2026-10-27", Energy: 2, Mood: 2, SleepHours: &sleep},
		{Date: "2026-10-27", Energy: 5, Mood: 4, SleepHours: &sleep}, // same day replaces
		{Date: "2026-10-30", Energy: 1, Mood: 1},
	} {
		if err := s.PutCheckin(c); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Checkins("2026-10-26", "2026-10-28")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].SleepHours != nil || got[1].Energy != 5 || *got[1].SleepHours != 6.5 {
		t.Fatalf("got %+v", got)
	}
	for _, bad := range []Checkin{
		{Date: "2026-11-01", Energy: 0, Mood: 3},
		{Date: "2026-11-01", Energy: 3, Mood: 6},
		{Date: "2026-11-01", Energy: 3, Mood: 3, SleepHours: ptr(25)},
	} {
		if err := s.PutCheckin(bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func ptr(v float64) *float64 { return &v }

func TestLLMCalls(t *testing.T) {
	s, _ := open(t)
	tok := 12
	base := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	for i, r := range []string{"limit", "ok", "ok"} {
		c := LLMCall{Time: base.Add(time.Duration(i) * time.Hour), Task: "parse", Provider: "openrouter", Model: "m/x:free",
			LatencyMS: 120, Result: r, Attempt: 1, ChainPos: i, Source: "user"}
		if r == "ok" {
			c.PromptTokens, c.CompletionTokens = &tok, &tok
		}
		if err := s.AddLLMCall(c); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := s.CountLLMCalls(base.Add(30 * time.Minute)); err != nil || n != 2 {
		t.Fatalf("count %d, err %v", n, err)
	}
	got, err := s.RecentLLMCalls(2)
	if err != nil || len(got) != 2 {
		t.Fatalf("recent %v, err %v", got, err)
	}
	if got[0].ChainPos != 2 || got[0].PromptTokens == nil || *got[0].PromptTokens != 12 || !got[0].Time.Equal(base.Add(2*time.Hour)) {
		t.Errorf("newest first: %+v", got[0])
	}
	bad := LLMCall{Time: base, Task: "parse", Provider: "p", Model: "m", Result: "weird", Attempt: 1, Source: "user"}
	if err := s.AddLLMCall(bad); err == nil {
		t.Error("accepted unknown result")
	}
	bad.Result, bad.Source = "ok", "bot"
	if err := s.AddLLMCall(bad); err == nil {
		t.Error("accepted unknown source")
	}
}

func TestItemsIdempotent(t *testing.T) {
	s, _ := open(t)
	ev := "abc"
	it := Item{Kind: "event", Title: "КВН", Category: "hobby", Status: "written", IdempotencyKey: "key-1", EventID: &ev}
	first, created, err := s.AddItem(it)
	if err != nil || !created || first.ID == 0 {
		t.Fatalf("first add: %+v created=%v err=%v", first, created, err)
	}
	it.Title = "другое"
	again, created, err := s.AddItem(it)
	if err != nil || created || again.ID != first.ID || again.Title != "КВН" {
		t.Fatalf("repeat must return the stored item: %+v created=%v err=%v", again, created, err)
	}
	if _, ok, _ := s.ItemByKey("nope"); ok {
		t.Fatal("unknown key found")
	}
}

func TestCategoryMemory(t *testing.T) {
	s, _ := open(t)
	if _, ok, _ := s.CategoryOf("квн"); ok {
		t.Fatal("memory must start empty")
	}
	for _, c := range []string{"hobby", "hobby", "study"} {
		if err := s.RememberCategory("квн", c, "user"); err != nil {
			t.Fatal(err)
		}
	}
	got, ok, err := s.CategoryOf("квн")
	if err != nil || !ok || got != "study" {
		t.Fatalf("the last answer wins: %q %v %v", got, ok, err)
	}
	var n int
	s.DB.QueryRow(`SELECT confirmations FROM category_memory WHERE name = 'квн'`).Scan(&n)
	if n != 1 {
		t.Fatalf("a changed answer restarts the count, got %d", n)
	}
}
