package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"filippo.io/age"
)

var (
	steamIDRe = regexp.MustCompile(`^\d{17}$`)
	loginRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,38}$`)
)

const minTokenLen = 43 // 32 random bytes in base64url; hex of 32 bytes is 64

type Config struct {
	Listen    string
	DBPath    string
	BackupDir string
	// AppDir holds the phone app the server hands out: life-agent.apk and
	// latest.json, both put there by deploy.sh. Read-only for the service.
	AppDir   string
	Token    string
	Location *time.Location

	GoogleKeyFile string   // service-account JSON key
	CalendarIDs   []string // calendars read by the agent
	CalendarWrite string   // calendar the agent creates events in; empty: writing is off
	ICSURL        string   // optional class-schedule feed

	OpenRouterKey string // empty: the LLM features report "not configured"

	// Activity sources read by the server (all optional; a source without its
	// settings is simply off). Keys live only here, never in the database.
	SteamKey, SteamID string // Web API key and SteamID64 (17 digits)
	GitHubToken       string // fine-grained, read-only
	GitHubLogin       string
	LeetCodeUser      string // empty: the optional LeetCode path is off

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
	c.AppDir = orDefault(getenv("APP_DIR"), filepath.Join(filepath.Dir(c.DBPath), "..", "app"))

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
	c.SteamKey = strings.TrimSpace(getenv("STEAM_API_KEY"))
	c.SteamID = strings.TrimSpace(getenv("STEAM_ID"))
	if (c.SteamKey == "") != (c.SteamID == "") {
		return Config{}, errors.New("STEAM_API_KEY and STEAM_ID go together")
	}
	if c.SteamID != "" && !steamIDRe.MatchString(c.SteamID) {
		return Config{}, errors.New("STEAM_ID must be a SteamID64: 17 digits")
	}
	c.GitHubToken = strings.TrimSpace(getenv("GITHUB_TOKEN"))
	c.GitHubLogin = strings.TrimSpace(getenv("GITHUB_LOGIN"))
	if (c.GitHubToken == "") != (c.GitHubLogin == "") {
		return Config{}, errors.New("GITHUB_TOKEN and GITHUB_LOGIN go together")
	}
	if c.GitHubLogin != "" && !loginRe.MatchString(c.GitHubLogin) {
		return Config{}, errors.New("GITHUB_LOGIN is not a valid login")
	}
	c.LeetCodeUser = strings.TrimSpace(getenv("LEETCODE_USER"))
	if c.LeetCodeUser != "" && !loginRe.MatchString(c.LeetCodeUser) {
		return Config{}, errors.New("LEETCODE_USER is not a valid login")
	}
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
