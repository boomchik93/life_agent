package macagent

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Sleep window of the night minutes (PLAN.md section 14).
const (
	SleepStartHour = 23
	SleepEndHour   = 7
)

// Agent ties ActivityWatch to the server.
type Agent struct {
	AW   *AW
	Life *Life
	Loc  *time.Location
	Now  func() time.Time
}

// Report is the result of one day.
type Report struct {
	Date           string
	Rows           []Row
	UnknownPercent int
}

// SyncDay reads one day and sends its sums. A day without activity sends nothing.
func (a *Agent) SyncDay(ctx context.Context, day time.Time) (Report, error) {
	now := a.Now().In(a.Loc)
	from := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, a.Loc)
	to := from.AddDate(0, 0, 1)
	if to.After(now) {
		to = now
	}
	rep := Report{Date: Date(from)}
	if !to.After(from) {
		return rep, nil
	}
	segs, err := a.AW.Segments(ctx, from, to)
	if err != nil {
		return rep, err
	}
	// No context is better than no summary.
	events, err := a.Life.ContextEvents(ctx, from)
	if err != nil {
		var se *ServerError
		if errors.As(err, &se) && se.Status == 401 {
			return rep, err
		}
		events = nil
	}
	rows, err := Summarize(ctx, from, segs, events, a.Life.Categorize, SleepStartHour, SleepEndHour)
	if err != nil {
		return rep, err
	}
	rep.Rows = rows
	var total, unknown int
	for _, r := range rows {
		total += r.Minutes
		if r.Category == "unknown" {
			unknown += r.Minutes
		}
	}
	if total > 0 {
		rep.UnknownPercent = (100*unknown + total/2) / total
	}
	if len(rows) == 0 {
		return rep, nil
	}
	return rep, a.Life.SendDay(ctx, from, rows)
}

// Sync sends yesterday and today.
func (a *Agent) Sync(ctx context.Context) ([]Report, error) {
	today := a.Now().In(a.Loc)
	var out []Report
	for _, d := range []time.Time{today.AddDate(0, 0, -1), today} {
		rep, err := a.SyncDay(ctx, d)
		if err != nil {
			return out, fmt.Errorf("%s: %w", Date(d), err)
		}
		out = append(out, rep)
	}
	return out, nil
}

// FileVaultOn parses the output of `fdesetup status`.
func FileVaultOn(output string) bool {
	return strings.Contains(strings.ToLower(output), "filevault is on")
}

// CheckFileVault runs `fdesetup status`. ActivityWatch keeps titles and URLs
// on disk unencrypted, so the disk must be (PLAN.md section 17).
func CheckFileVault(ctx context.Context) (on bool, err error) {
	out, err := exec.CommandContext(ctx, "fdesetup", "status").Output()
	if err != nil {
		return false, err
	}
	return FileVaultOn(string(out)), nil
}
