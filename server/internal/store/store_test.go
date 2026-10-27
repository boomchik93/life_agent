package store

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
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

// migrationCount is the number of migration files: a new one needs no test edit.
func migrationCount(t *testing.T) int {
	t.Helper()
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
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
	if err := s2.DB.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil || n != migrationCount(t) {
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
	oldPath, err := s.Backup(dir, 14*24*time.Hour, old, "")
	if err != nil {
		t.Fatal(err)
	}
	os.Chtimes(oldPath, old, old)
	other := filepath.Join(dir, "keep-me.txt")
	os.WriteFile(other, []byte("x"), 0o600)
	os.Chtimes(other, old, old)

	newPath, err := s.Backup(dir, 14*24*time.Hour, time.Now(), "")
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

func TestListAndPatchItems(t *testing.T) {
	s, _ := open(t)
	dur, dl := 120, "2026-10-30"
	task := func(key, status string) Item {
		return Item{Kind: "task", Title: key, Category: "study", DurationMin: &dur, Deadline: &dl, Status: status, IdempotencyKey: key}
	}
	a, _, _ := s.AddItem(task("a-task-key", "confirmed"))
	s.AddItem(task("b-task-key", "done"))
	ev := "e1"
	s.AddItem(Item{Kind: "event", Title: "e", Category: "hobby", Status: "written", IdempotencyKey: "event-key", EventID: &ev})

	open, err := s.Items("task", "open")
	if err != nil || len(open) != 1 || open[0].ID != a.ID {
		t.Fatalf("open tasks = %+v err %v", open, err)
	}
	if all, _ := s.Items("", ""); len(all) != 3 {
		t.Fatalf("all items = %d", len(all))
	}
	done := "done"
	got, err := s.PatchItem(a.ID, ItemPatch{Status: &done})
	if err != nil || got.Status != "done" || got.Title != "a-task-key" {
		t.Fatalf("patch = %+v err %v", got, err)
	}
	if open, _ := s.Items("task", "open"); len(open) != 0 {
		t.Fatalf("a done task is not open: %+v", open)
	}
	if _, err := s.PatchItem(999, ItemPatch{Status: &done}); err != ErrNotFound {
		t.Fatalf("missing id: %v", err)
	}
}

func TestPlanChunks(t *testing.T) {
	s, _ := open(t)
	dur, dl := 120, "2026-10-30"
	it, _, _ := s.AddItem(Item{Kind: "task", Title: "t", Category: "study", DurationMin: &dur, Deadline: &dl, Status: "confirmed", IdempotencyKey: "task-key-1"})
	c := PlanChunk{ItemID: it.ID, ApplyKey: "apply-1", Seq: 0, EventID: "ev0", Start: "2026-10-27T13:00:00+05:00", End: "2026-10-27T14:00:00+05:00", Minutes: 60}
	if err := s.AddChunk(c); err != nil {
		t.Fatal(err)
	}
	if err := s.AddChunk(c); err != nil { // the same piece again is ignored
		t.Fatal(err)
	}
	c.Seq, c.EventID = 1, "ev1"
	if err := s.AddChunk(c); err != nil {
		t.Fatal(err)
	}
	got, err := s.ChunksByKey("apply-1")
	if err != nil || len(got) != 2 {
		t.Fatalf("chunks = %+v err %v", got, err)
	}
	if m, _ := s.PlannedMinutes(""); m[it.ID] != 120 {
		t.Fatalf("planned = %v", m)
	}
	if m, _ := s.PlannedMinutes("apply-1"); m[it.ID] != 0 {
		t.Fatalf("a retry must not count its own pieces: %v", m)
	}
}

func TestHealth(t *testing.T) {
	s, _ := open(t)
	f := func(v float64) *float64 { return &v }
	if err := s.PutHealth(Health{Date: "2026-10-27", SleepHours: f(7.5), AvgHR: f(62)}); err != nil {
		t.Fatal(err)
	}
	// A later partial reading keeps what it does not mention.
	if err := s.PutHealth(Health{Date: "2026-10-27", Stress: f(30), AvgHR: f(64)}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Healths("2026-10-01", "2026-10-31")
	if err != nil || len(got) != 1 {
		t.Fatalf("healths = %+v err %v", got, err)
	}
	h := got[0]
	if h.Steps != nil || h.WorkoutMin != nil || h.RestingHR != nil {
		t.Errorf("unknown fields must stay nil: %+v", h)
	}
	steps, workout, rest := 8200, 45, 58.0
	if err := s.PutHealth(Health{Date: "2026-10-27", Steps: &steps, WorkoutMin: &workout, RestingHR: &rest}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Healths("2026-10-01", "2026-10-31")
	h = got[0]
	if *h.Steps != 8200 || *h.WorkoutMin != 45 || *h.RestingHR != 58 || *h.SleepHours != 7.5 {
		t.Errorf("extra fields merged badly: %+v", h)
	}
	if *h.SleepHours != 7.5 || *h.AvgHR != 64 || *h.Stress != 30 {
		t.Errorf("merged = sleep %v hr %v stress %v", *h.SleepHours, *h.AvgHR, *h.Stress)
	}
	if n, err := s.DeleteHealth(); err != nil || n != 1 {
		t.Fatalf("delete = %d %v", n, err)
	}
	if got, _ := s.Healths("2026-10-01", "2026-10-31"); len(got) != 0 {
		t.Errorf("rows left: %+v", got)
	}
}

func TestAdviceLog(t *testing.T) {
	s, _ := open(t)
	if last, err := s.LastAdviceAt(); err != nil || last != nil {
		t.Fatalf("empty log: %v %v", last, err)
	}
	if _, ok, _ := s.TakePendingAdvice(); ok {
		t.Fatal("nothing is pending yet")
	}
	a1, err := s.AddAdvice(Advice{At: "2026-10-20T08:00:00Z", Reason: "overload", Text: "раз", Model: "m", Source: "auto", Status: "pending"})
	if err != nil || a1.ID == 0 {
		t.Fatal(err)
	}
	s.AddAdvice(Advice{At: "2026-10-25T08:00:00Z", Reason: "asked", Text: "два", Model: "m", Source: "user", Status: "delivered"})

	if p, _ := s.HasPendingAdvice(); !p {
		t.Error("one advice waits")
	}
	got, ok, err := s.TakePendingAdvice()
	if err != nil || !ok || got.ID != a1.ID || got.Status != "delivered" {
		t.Fatalf("take = %+v %v %v", got, ok, err)
	}
	if _, ok, _ := s.TakePendingAdvice(); ok {
		t.Error("an advice is handed over once")
	}
	last, _ := s.LastAdviceAt()
	if last == nil || last.Format("2006-01-02") != "2026-10-25" {
		t.Errorf("last = %v", last)
	}
	list, _ := s.Advices(10)
	if len(list) != 2 || list[0].Text != "два" {
		t.Errorf("history newest first: %+v", list)
	}
	if _, err := s.AddAdvice(Advice{At: "x", Source: "bot", Status: "pending"}); err == nil {
		t.Error("accepted an unknown source")
	}
}

func TestEncryptedBackup(t *testing.T) {
	s, _ := open(t)
	s.PutSettings(map[string]string{"k": `"secret-value-in-the-db"`})
	dir := filepath.Join(t.TempDir(), "backup")

	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	path, err := s.Backup(dir, 14*24*time.Hour, time.Now(), id.Recipient().String())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, ".db.age") {
		t.Fatalf("path %s", path)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("only the encrypted file may stay on the server: %v", entries)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", info.Mode())
	}
	raw, _ := os.ReadFile(path)
	if bytes.Contains(raw, []byte("secret-value-in-the-db")) || bytes.HasPrefix(raw, []byte("SQLite format")) {
		t.Fatal("the backup is not encrypted")
	}

	// Only the private key opens it.
	other, _ := age.GenerateX25519Identity()
	if _, err := age.Decrypt(bytes.NewReader(raw), other); err == nil {
		t.Fatal("a stranger's key opened the backup")
	}
	r, err := age.Decrypt(bytes.NewReader(raw), id)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	restored := filepath.Join(t.TempDir(), "restored.db")
	os.WriteFile(restored, plain, 0o600)
	b, err := Open(restored)
	if err != nil {
		t.Fatalf("decrypted backup is not a database: %v", err)
	}
	defer b.Close()
	if kv, _ := b.Settings(); kv["k"] != `"secret-value-in-the-db"` {
		t.Errorf("restored settings: %v", kv)
	}

	// Encrypted copies are pruned like plain ones.
	old := time.Now().Add(-30 * 24 * time.Hour)
	os.Chtimes(path, old, old)
	if _, err := s.Backup(dir, 14*24*time.Hour, time.Now().Add(time.Second), id.Recipient().String()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("old encrypted backup not pruned")
	}
	if _, err := s.Backup(dir, time.Hour, time.Now(), "not-a-key"); err == nil {
		t.Error("a bad recipient must fail, not write a plain copy")
	}
	if left, _ := os.ReadDir(dir); len(left) != 1 {
		t.Errorf("a failed backup left files: %v", left)
	}
}
