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
	if err := s2.DB.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil || n != 1 {
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
