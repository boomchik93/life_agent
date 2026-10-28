package calendar

import (
	"context"
	"fmt"
	"sync"
	"time"
)

const (
	// cacheKeep is how long a week that could not be read again is still answered.
	cacheKeep = 6 * time.Hour
	// cacheRetry is how long a source that failed is not asked again.
	cacheRetry = 30 * time.Second
	// cacheReadTimeout bounds one read of a source, all its pages included.
	cacheReadTimeout = time.Minute
)

type weekKey struct {
	src    int
	monday int64
}

type weekEntry struct {
	at       time.Time     // when the events were read; zero: never
	events   []Event       // the events overlapping the week
	failedAt time.Time     // when the last read failed; zero: it did not
	err      error         // the error of the last read, nil after a good one
	loading  chan struct{} // closed when the running read ends; nil: none runs
}

// Cache keeps the events of every source week by week, so the screens of the
// app that open together, and the windows of different sizes they ask for,
// share one read of the calendars. A week is fresh for a minute or so; after
// that it is still answered at once while it is read again in the
// background, for up to cacheKeep. A source that fails is not asked again for
// cacheRetry, and its last events are answered with a warning.
type Cache struct {
	sources []Named
	loc     *time.Location
	fresh   time.Duration
	keep    time.Duration
	retry   time.Duration
	now     func() time.Time

	mu    sync.Mutex
	weeks map[weekKey]*weekEntry
}

func NewCache(sources []Named, loc *time.Location, fresh time.Duration) *Cache {
	return &Cache{
		sources: sources, loc: loc, fresh: fresh, keep: cacheKeep, retry: cacheRetry,
		now: time.Now, weeks: map[weekKey]*weekEntry{},
	}
}

// Collect returns the events overlapping [from, to) from all sources, sorted.
// A source that could not be read becomes a warning.
func (c *Cache) Collect(ctx context.Context, from, to time.Time) ([]Event, []string) {
	mondays := c.mondays(from, to)
	results := make([][]Event, len(c.sources))
	errs := make([]error, len(c.sources))
	var wg sync.WaitGroup
	for i := range c.sources {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = c.source(ctx, i, mondays)
		}()
	}
	wg.Wait()

	var all []Event
	var warnings []string
	// An event across Monday midnight is kept by both weeks.
	seen := map[Event]bool{}
	for i := range c.sources {
		if errs[i] != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", c.sources[i].Name, errs[i]))
		}
		for _, e := range results[i] {
			if !seen[e] && overlaps(e.Start, e.End, from, to) {
				seen[e] = true
				all = append(all, e)
			}
		}
	}
	sortEvents(all)
	return all, warnings
}

// Invalidate forgets everything: the calendar has just changed. A read that
// is running keeps writing to the forgotten weeks, not to the new ones.
func (c *Cache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.weeks = map[weekKey]*weekEntry{}
}

// mondays lists the Monday midnights of the weeks [from, to) touches.
func (c *Cache) mondays(from, to time.Time) []time.Time {
	m, _ := WeekBounds(from, c.loc)
	var out []time.Time
	for ; m.Before(to); m = m.AddDate(0, 0, 7) {
		out = append(out, m)
	}
	return out
}

func (c *Cache) entry(src int, monday time.Time) *weekEntry {
	k := weekKey{src, monday.Unix()}
	e := c.weeks[k]
	if e == nil {
		e = &weekEntry{}
		c.weeks[k] = e
	}
	return e
}

// source answers the weeks of one source. A week that has events, even old
// ones, is answered at once; a week without them waits for its read.
func (c *Cache) source(ctx context.Context, src int, mondays []time.Time) ([]Event, error) {
	for {
		c.mu.Lock()
		now := c.now()
		var out []Event
		var err error
		var wait chan struct{}
		var read []time.Time // weeks to read now
		missing := false     // some week has no events to answer yet
		for _, m := range mondays {
			e := c.entry(src, m)
			failed := !e.failedAt.IsZero() && now.Sub(e.failedAt) < c.retry
			if e.err != nil {
				err = e.err
			}
			if !e.at.IsZero() && now.Sub(e.at) < c.keep {
				out = append(out, e.events...)
				if now.Sub(e.at) >= c.fresh && e.loading == nil && !failed {
					read = append(read, m)
				}
				continue
			}
			switch {
			case e.loading != nil:
				missing, wait = true, e.loading
			case failed:
				// No events and the source is resting: the warning says why.
			default:
				missing = true
				read = append(read, m)
			}
		}
		var started chan struct{}
		if len(read) > 0 {
			started = c.startRead(src, read)
		}
		c.mu.Unlock()

		if !missing {
			return out, err
		}
		if wait == nil {
			wait = started
		}
		select {
		case <-wait:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// startRead marks the weeks as being read and reads them in the background.
// The caller holds the lock.
func (c *Cache) startRead(src int, mondays []time.Time) chan struct{} {
	done := make(chan struct{})
	entries := make([]*weekEntry, len(mondays))
	for i, m := range mondays {
		entries[i] = c.entry(src, m)
		entries[i].loading = done
	}
	go c.read(src, mondays, entries, done)
	return done
}

// read asks the source once per run of consecutive weeks and splits the
// answer into the weeks. It does not depend on the request that started it:
// the others waiting for the same weeks still get the answer.
func (c *Cache) read(src int, mondays []time.Time, entries []*weekEntry, done chan struct{}) {
	defer close(done)
	ctx, cancel := context.WithTimeout(context.Background(), cacheReadTimeout)
	defer cancel()
	for start := 0; start < len(mondays); {
		end := start + 1
		for end < len(mondays) && mondays[end].Equal(mondays[end-1].AddDate(0, 0, 7)) {
			end++
		}
		from, to := mondays[start], mondays[end-1].AddDate(0, 0, 7)
		events, err := c.sources[src].Provider.Events(ctx, from, to)
		c.store(entries[start:end], mondays[start:end], events, err)
		start = end
	}
	c.prune()
}

func (c *Cache) store(entries []*weekEntry, mondays []time.Time, events []Event, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	for i, e := range entries {
		e.loading = nil
		if err != nil {
			e.failedAt, e.err = now, err
			continue
		}
		from, to := mondays[i], mondays[i].AddDate(0, 0, 7)
		var week []Event
		for _, ev := range events {
			if overlaps(ev.Start, ev.End, from, to) {
				week = append(week, ev)
			}
		}
		e.at, e.events, e.failedAt, e.err = now, week, time.Time{}, nil
	}
}

// prune drops the weeks that are too old to answer and are not being read.
func (c *Cache) prune() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	for k, e := range c.weeks {
		if e.loading != nil {
			continue
		}
		old := e.at.IsZero() || now.Sub(e.at) >= c.keep
		resting := !e.failedAt.IsZero() && now.Sub(e.failedAt) < c.retry
		if old && !resting {
			delete(c.weeks, k)
		}
	}
}
