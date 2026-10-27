// Command macagent sends the time spent on the Mac to the life-agent server,
// as minutes per category. It reads ActivityWatch on localhost:5600; window
// titles and URLs never leave the Mac.
//
//	macagent sync     send yesterday and today
//	macagent check    check ActivityWatch, the server and FileVault
//
// Settings come from the environment or from ~/.config/life-agent/mac.env:
// LIFE_SERVER (https://...), LIFE_TOKEN, AW_URL (default http://localhost:5600),
// TIMEZONE (default Asia/Yekaterinburg).
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
	_ "time/tzdata"

	"lifeagent/internal/config"
	"lifeagent/internal/macagent"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "macagent:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cmd := "sync"
	if len(args) > 0 {
		cmd = args[0]
	}
	if home, err := os.UserHomeDir(); err == nil {
		if err := config.LoadDotEnv(filepath.Join(home, ".config", "life-agent", "mac.env")); err != nil {
			return err
		}
	}
	server, token := os.Getenv("LIFE_SERVER"), os.Getenv("LIFE_TOKEN")
	if server == "" || token == "" {
		return fmt.Errorf("set LIFE_SERVER and LIFE_TOKEN (environment or ~/.config/life-agent/mac.env)")
	}
	awURL := os.Getenv("AW_URL")
	if awURL == "" {
		awURL = "http://localhost:5600"
	}
	tz := os.Getenv("TIMEZONE")
	if tz == "" {
		tz = "Asia/Yekaterinburg"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return err
	}
	agent := &macagent.Agent{
		AW:   &macagent.AW{BaseURL: awURL},
		Life: &macagent.Life{BaseURL: server, Token: token},
		Loc:  loc, Now: time.Now,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	switch cmd {
	case "sync":
		reports, err := agent.Sync(ctx)
		for _, r := range reports {
			fmt.Printf("%s: %d categories, unknown %d%%\n", r.Date, len(r.Rows), r.UnknownPercent)
		}
		return err
	case "check":
		return check(ctx, agent)
	default:
		return fmt.Errorf("unknown command %q (want sync or check)", cmd)
	}
}

func check(ctx context.Context, a *macagent.Agent) error {
	failed := false
	say := func(ok bool, text string) {
		mark := "ok  "
		if !ok {
			mark, failed = "FAIL", true
		}
		fmt.Println(mark, text)
	}
	if _, err := a.AW.Buckets(ctx); err != nil {
		say(false, err.Error())
	} else {
		say(true, "ActivityWatch answers")
	}
	if _, err := a.Life.Categorize(ctx, []string{"github.com"}, false); err != nil {
		say(false, err.Error())
	} else {
		say(true, "the server accepts the token")
	}
	on, err := macagent.CheckFileVault(ctx)
	switch {
	case err != nil:
		say(false, "could not read the FileVault status (fdesetup)")
	case !on:
		say(false, "FileVault is off: ActivityWatch keeps titles and URLs unencrypted on this disk")
	default:
		say(true, "FileVault is on")
	}
	if failed {
		return fmt.Errorf("some checks failed")
	}
	return nil
}
