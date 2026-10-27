package activity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"time"

	"lifeagent/internal/store"
)

// Sink is where collected days go; *store.Store is one.
type Sink interface {
	SourceEnabled(source string) (bool, error)
	ReplaceActivity(date, source string, rows []store.ActivityRow) error
	RecordSync(source, at string, errMsg *string) error
	SetSourceEnabled(source string, enabled bool) error
}

// Fragile marks a collector on an unofficial path that may break at any time.
// After a failure its source switches itself off and keeps the error in its
// state instead of failing again every round.
type Fragile interface {
	DisableOnError() bool
}

var urlRe = regexp.MustCompile(`https?://\S+`)

// SafeError makes an error fit to store and log: a failed HTTP call carries
// its URL, and the URL of an API may carry a key.
func SafeError(err error) string {
	s := urlRe.ReplaceAllString(err.Error(), "<url>")
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// Run reads one source once and stores what it returns. A source that is
// switched off is skipped without a trace. A failure is written to the
// source's state, not to the data: the old days stay as they were.
func Run(ctx context.Context, c Collector, now time.Time, sink Sink) error {
	source := c.Source()
	on, err := sink.SourceEnabled(source)
	if err != nil {
		return err
	}
	if !on {
		return nil
	}
	fail := func(err error) error {
		msg := SafeError(err)
		_ = sink.RecordSync(source, now.UTC().Format(time.RFC3339), &msg)
		if f, ok := c.(Fragile); ok && f.DisableOnError() {
			_ = sink.SetSourceEnabled(source, false)
		}
		return errors.New(msg)
	}

	days, err := c.Collect(ctx, now)
	if err != nil {
		return fail(err)
	}
	// Check everything before writing anything: a bad row discards the round.
	type stored struct {
		date string
		rows []store.ActivityRow
	}
	var out []stored
	for _, d := range days {
		if _, err := time.Parse("2006-01-02", d.Date); err != nil {
			return fail(fmt.Errorf("bad date %q from the source", d.Date))
		}
		seen := map[string]bool{}
		rows := make([]store.ActivityRow, 0, len(d.Rows))
		for _, r := range d.Rows {
			if r.Confidence == 0 {
				r.Confidence = 1
			}
			if err := r.Validate(source); err != nil {
				return fail(err)
			}
			if seen[r.Category] {
				return fail(fmt.Errorf("category %s appears twice on %s", r.Category, d.Date))
			}
			seen[r.Category] = true
			extra, err := json.Marshal(nonNil(r.Extra))
			if err != nil {
				return fail(err)
			}
			rows = append(rows, store.ActivityRow{Category: r.Category, Minutes: r.Minutes, NightMinutes: r.NightMinutes,
				Count: r.Count, Confidence: r.Confidence, Extra: string(extra)})
		}
		out = append(out, stored{d.Date, rows})
	}
	for _, d := range out {
		if err := sink.ReplaceActivity(d.date, source, d.rows); err != nil {
			return fail(err)
		}
	}
	return sink.RecordSync(source, now.UTC().Format(time.RFC3339), nil)
}

func nonNil(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// Schedule runs a collector at an interval.
type Schedule struct {
	Collector Collector
	Every     time.Duration
}

// Loop runs every schedule until ctx ends, the first time at once. Errors are
// logged in their safe form; the loop never stops because of one.
func Loop(ctx context.Context, schedules []Schedule, sink Sink, now func() time.Time, logger *slog.Logger) {
	for _, sc := range schedules {
		go func() {
			round := func() {
				if err := Run(ctx, sc.Collector, now(), sink); err != nil {
					logger.Error("collector failed", "source", sc.Collector.Source(), "err", err)
				}
			}
			round()
			t := time.NewTicker(sc.Every)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					round()
				}
			}
		}()
	}
}
