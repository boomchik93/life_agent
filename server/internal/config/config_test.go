package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoad(t *testing.T) {
	good := strings.Repeat("a", 64)
	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
	}{
		{"ok defaults", map[string]string{"ACCESS_TOKEN": good}, false},
		{"missing token", map[string]string{}, true},
		{"short token", map[string]string{"ACCESS_TOKEN": "short"}, true},
		{"bad timezone", map[string]string{"ACCESS_TOKEN": good, "TIMEZONE": "Mars/Base"}, true},
		{"calendars without key", map[string]string{"ACCESS_TOKEN": good, "CALENDAR_IDS": "a@b"}, true},
		{"calendars with key", map[string]string{"ACCESS_TOKEN": good, "CALENDAR_IDS": "a@b", "GOOGLE_SA_KEY_FILE": "k.json"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(env(tt.env))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoadDefaults(t *testing.T) {
	c, err := Load(env(map[string]string{"ACCESS_TOKEN": strings.Repeat("a", 64), "DB_PATH": "/opt/life-agent/data/x.db"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != "127.0.0.1:18010" {
		t.Errorf("listen = %q", c.Listen)
	}
	if c.BackupDir != "/opt/life-agent/backup" {
		t.Errorf("backup dir = %q", c.BackupDir)
	}
	if c.Location.String() != "Asia/Yekaterinburg" {
		t.Errorf("tz = %q", c.Location)
	}
}

func TestLoadCalendarIDs(t *testing.T) {
	c, err := Load(env(map[string]string{
		"ACCESS_TOKEN": strings.Repeat("a", 64), "GOOGLE_SA_KEY_FILE": "k.json",
		"CALENDAR_IDS": " a@b.c , ,d@e.f,",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.CalendarIDs) != 2 || c.CalendarIDs[0] != "a@b.c" || c.CalendarIDs[1] != "d@e.f" {
		t.Errorf("ids = %q", c.CalendarIDs)
	}
}

func TestLoadDotEnv(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(p, []byte("# c\nLA_TEST_A=1\nLA_TEST_B=\"two\"\nLA_TEST_C=x=y\n"), 0o600)
	t.Setenv("LA_TEST_A", "preset")
	t.Cleanup(func() { os.Unsetenv("LA_TEST_B"); os.Unsetenv("LA_TEST_C") })

	if err := LoadDotEnv(p); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("LA_TEST_A") != "preset" {
		t.Error("existing var overwritten")
	}
	if os.Getenv("LA_TEST_B") != "two" || os.Getenv("LA_TEST_C") != "x=y" {
		t.Errorf("B=%q C=%q", os.Getenv("LA_TEST_B"), os.Getenv("LA_TEST_C"))
	}
	if err := LoadDotEnv(filepath.Join(t.TempDir(), "nope")); err != nil {
		t.Errorf("missing file: %v", err)
	}
}
