package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"
)

const minTokenLen = 43 // 32 random bytes in base64url; hex of 32 bytes is 64

type Config struct {
	Listen    string
	DBPath    string
	BackupDir string
	Token     string
	Location  *time.Location

	GoogleKeyFile string   // service-account JSON key
	CalendarIDs   []string // calendars read by the agent
	CalendarWrite string   // calendar the agent creates events in; empty: writing is off
	ICSURL        string   // optional class-schedule feed

	OpenRouterKey string // empty: the LLM features report "not configured"

	// AgeRecipient is the public key (age1...) backups are encrypted to. The
	// private key stays on the Mac, so the server cannot read its own backups.
	AgeRecipient string
}

// Load reads configuration from the environment.
func Load(getenv func(string) string) (Config, error) {
	c := Config{
		Listen: orDefault(getenv("LISTEN_ADDR"), "127.0.0.1:18010"),
		DBPath: orDefault(getenv("DB_PATH"), "./data/life-agent.db"),
		Token:  getenv("ACCESS_TOKEN"),
	}
	c.BackupDir = orDefault(getenv("BACKUP_DIR"), filepath.Join(filepath.Dir(c.DBPath), "..", "backup"))

	if len(c.Token) < minTokenLen {
		return Config{}, fmt.Errorf("ACCESS_TOKEN must be at least %d characters", minTokenLen)
	}

	tz := orDefault(getenv("TIMEZONE"), "Asia/Yekaterinburg")
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return Config{}, fmt.Errorf("TIMEZONE %q: %w", tz, err)
	}
	c.Location = loc

	c.GoogleKeyFile = getenv("GOOGLE_SA_KEY_FILE")
	c.ICSURL = getenv("ICS_URL")
	c.CalendarWrite = strings.TrimSpace(getenv("CALENDAR_WRITE_ID"))
	c.OpenRouterKey = getenv("OPENROUTER_API_KEY")
	c.AgeRecipient = strings.TrimSpace(getenv("AGE_RECIPIENT"))
	if c.AgeRecipient != "" {
		if _, err := age.ParseX25519Recipient(c.AgeRecipient); err != nil {
			return Config{}, fmt.Errorf("AGE_RECIPIENT: %w", err)
		}
	}
	for _, id := range strings.Split(getenv("CALENDAR_IDS"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			c.CalendarIDs = append(c.CalendarIDs, id)
		}
	}
	if (len(c.CalendarIDs) > 0 || c.CalendarWrite != "") && c.GoogleKeyFile == "" {
		return Config{}, errors.New("CALENDAR_IDS is set but GOOGLE_SA_KEY_FILE is not")
	}
	return c, nil
}

// LoadDotEnv sets variables from a KEY=VALUE file for keys not already set.
// A missing file is not an error.
func LoadDotEnv(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if _, set := os.LookupEnv(k); !set {
			os.Setenv(k, v)
		}
	}
	return sc.Err()
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
