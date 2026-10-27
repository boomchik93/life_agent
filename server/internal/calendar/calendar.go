package calendar

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

type Event struct {
	ID        string    `json:"id"`
	Source    string    `json:"source"` // "google" or "ics"
	Calendar  string    `json:"calendar"`
	Title     string    `json:"title"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	AllDay    bool      `json:"all_day"`
	Recurring bool      `json:"recurring"`
}

// Provider returns the events overlapping [from, to), with recurring series
// already expanded into single instances.
type Provider interface {
	Events(ctx context.Context, from, to time.Time) ([]Event, error)
}

type Named struct {
	Name     string
	Provider Provider
}

// Aggregator merges several providers. One failing source does not hide the rest:
// its error is reported as a warning.
type Aggregator struct {
	Sources []Named
}

func (a *Aggregator) Collect(ctx context.Context, from, to time.Time) ([]Event, []string) {
	type result struct {
		events []Event
		err    error
	}
	results := make([]result, len(a.Sources))
	var wg sync.WaitGroup
	for i, s := range a.Sources {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ev, err := s.Provider.Events(ctx, from, to)
			results[i] = result{ev, err}
		}()
	}
	wg.Wait()

	var all []Event
	var warnings []string
	for i, r := range results {
		if r.err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", a.Sources[i].Name, r.err))
			continue
		}
		all = append(all, r.events...)
	}
	sort.SliceStable(all, func(i, j int) bool {
		if !all[i].Start.Equal(all[j].Start) {
			return all[i].Start.Before(all[j].Start)
		}
		return all[i].End.Before(all[j].End)
	})
	return all, warnings
}

// WeekBounds returns Monday 00:00 of the week containing t and the next Monday 00:00.
func WeekBounds(t time.Time, loc *time.Location) (time.Time, time.Time) {
	t = t.In(loc)
	back := (int(t.Weekday()) + 6) % 7
	from := time.Date(t.Year(), t.Month(), t.Day()-back, 0, 0, 0, 0, loc)
	to := time.Date(from.Year(), from.Month(), from.Day()+7, 0, 0, 0, 0, loc)
	return from, to
}

func overlaps(start, end, from, to time.Time) bool {
	if !end.After(start) {
		return !start.Before(from) && start.Before(to)
	}
	return end.After(from) && start.Before(to)
}
