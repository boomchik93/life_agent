package calendar

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type countingProvider struct {
	calls   atomic.Int32
	mu      sync.Mutex
	events  []Event
	err     error
	delay   time.Duration
	windows [][2]time.Time
}

func (p *countingProvider) Events(_ context.Context, from, to time.Time) ([]Event, error) {
	p.calls.Add(1)
	time.Sleep(p.delay)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.windows = append(p.windows, [2]time.Time{from, to})
	if p.err != nil {
		return nil, p.err
	}
	var out []Event
	for _, e := range p.events {
		if overlaps(e.Start, e.End, from, to) {
			out = append(out, e)
		}
	}
	return out, nil
}

func (p *countingProvider) set(events []Event, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events, p.err = events, err
}

// clock is a settable time for the cache.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.t = c.t.Add(d) }

func newTestCache(p Provider) (*Cache, *clock) {
	c := NewCache([]Named{{Name: "google", Provider: p}}, yekat, time.Minute)
	clk := &clock{t: day(yekat, 2026, 10, 28, 9, 0)}
	c.now = clk.now
	return c, clk
}

// eventAt is a one-hour event on the given day of October 2026.
func eventAt(id string, d int) Event {
	start := day(yekat, 2026, 10, d, 10, 0)
	return Event{ID: id, Source: "google", Start: start, End: start.Add(time.Hour)}
}

// settle waits for the reads started in the background.
func settle(c *Cache) {
	for range 200 {
		c.mu.Lock()
		busy := false
		for _, e := range c.weeks {
			busy = busy || e.loading != nil
		}
		c.mu.Unlock()
		if !busy {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

func TestCacheReusesWeekWhileFresh(t *testing.T) {
	src := &countingProvider{events: []Event{eventAt("a", 27)}}
	c, _ := newTestCache(src)
	from, to := week()
	for range 3 {
		if ev, _ := c.Collect(context.Background(), from, to); len(ev) != 1 {
			t.Fatalf("events %+v", ev)
		}
	}
	if n := src.calls.Load(); n != 1 {
		t.Errorf("calls %d, want 1", n)
	}
	// A part of the week is answered from the same read.
	if ev, _ := c.Collect(context.Background(), from.AddDate(0, 0, 1), from.AddDate(0, 0, 2)); len(ev) != 1 {
		t.Errorf("tuesday %+v", ev)
	}
	if n := src.calls.Load(); n != 1 {
		t.Errorf("calls %d, want 1", n)
	}
}

func TestCacheAnswersStaleAndRefreshes(t *testing.T) {
	src := &countingProvider{events: []Event{eventAt("a", 27)}}
	c, clk := newTestCache(src)
	from, to := week()
	c.Collect(context.Background(), from, to)

	src.set([]Event{eventAt("a", 27), eventAt("b", 28)}, nil)
	clk.add(2 * time.Minute)
	if ev, _ := c.Collect(context.Background(), from, to); len(ev) != 1 {
		t.Fatalf("stale answer must come at once: %+v", ev)
	}
	settle(c)
	if ev, _ := c.Collect(context.Background(), from, to); len(ev) != 2 {
		t.Errorf("after the refresh %+v", ev)
	}
	if n := src.calls.Load(); n != 2 {
		t.Errorf("calls %d, want 2", n)
	}
}

func TestCacheReadsMissingWeeksAtOnce(t *testing.T) {
	src := &countingProvider{events: []Event{eventAt("a", 20), eventAt("b", 27)}}
	c, _ := newTestCache(src)
	from, to := week()
	c.Collect(context.Background(), from, to)
	// Three weeks back to this one: the two missing ones are one read.
	ev, _ := c.Collect(context.Background(), from.AddDate(0, 0, -21), to)
	if len(ev) != 2 {
		t.Fatalf("events %+v", ev)
	}
	if n := src.calls.Load(); n != 2 {
		t.Errorf("calls %d, want 2", n)
	}
	w := src.windows[1]
	if !w[0].Equal(from.AddDate(0, 0, -21)) || !w[1].Equal(from) {
		t.Errorf("second read %v", w)
	}
}

func TestCacheKeepsEventAcrossMondayOnce(t *testing.T) {
	from, to := week()
	cross := Event{ID: "night", Start: from.Add(-time.Hour), End: from.Add(time.Hour)}
	src := &countingProvider{events: []Event{cross}}
	c, _ := newTestCache(src)
	if ev, _ := c.Collect(context.Background(), from.AddDate(0, 0, -7), to); len(ev) != 1 {
		t.Errorf("events %+v", ev)
	}
}

func TestCacheInvalidate(t *testing.T) {
	src := &countingProvider{}
	c, _ := newTestCache(src)
	from, to := week()
	c.Collect(context.Background(), from, to)
	c.Invalidate()
	c.Collect(context.Background(), from, to)
	if n := src.calls.Load(); n != 2 {
		t.Errorf("calls %d, want 2", n)
	}
}

func TestCacheRestsFailingSource(t *testing.T) {
	src := &countingProvider{err: errors.New("timeout")}
	c, clk := newTestCache(src)
	from, to := week()
	for range 2 {
		if _, w := c.Collect(context.Background(), from, to); len(w) != 1 || w[0] != "google: timeout" {
			t.Errorf("warnings %v", w)
		}
	}
	if n := src.calls.Load(); n != 1 {
		t.Errorf("calls %d, want 1: the source rests", n)
	}
	clk.add(time.Minute)
	c.Collect(context.Background(), from, to)
	if n := src.calls.Load(); n != 2 {
		t.Errorf("calls %d, want 2 after the rest", n)
	}
}

func TestCacheKeepsLastEventsOfFailingSource(t *testing.T) {
	src := &countingProvider{events: []Event{eventAt("a", 27)}}
	c, clk := newTestCache(src)
	from, to := week()
	c.Collect(context.Background(), from, to)
	src.set(nil, errors.New("timeout"))
	clk.add(2 * time.Minute)
	c.Collect(context.Background(), from, to)
	settle(c)
	ev, w := c.Collect(context.Background(), from, to)
	if len(ev) != 1 || len(w) != 1 {
		t.Errorf("events %+v warnings %v", ev, w)
	}
}

func TestCacheMergesConcurrentRequests(t *testing.T) {
	src := &countingProvider{delay: 50 * time.Millisecond}
	c, _ := newTestCache(src)
	from, to := week()
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() { defer wg.Done(); c.Collect(context.Background(), from, to) }()
	}
	wg.Wait()
	if n := src.calls.Load(); n != 1 {
		t.Errorf("calls %d, want 1", n)
	}
}

func TestCacheCancelledRequestDoesNotStopRead(t *testing.T) {
	src := &countingProvider{events: []Event{eventAt("a", 27)}, delay: 50 * time.Millisecond}
	c, _ := newTestCache(src)
	from, to := week()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, w := c.Collect(ctx, from, to); len(w) != 1 {
		t.Errorf("a cancelled request reports it: %v", w)
	}
	if ev, _ := c.Collect(context.Background(), from, to); len(ev) != 1 {
		t.Errorf("events %+v", ev)
	}
	if n := src.calls.Load(); n != 1 {
		t.Errorf("calls %d, want 1", n)
	}
}

func TestCacheResultIsACopy(t *testing.T) {
	src := &countingProvider{events: []Event{eventAt("a", 27)}}
	c, _ := newTestCache(src)
	from, to := week()
	ev, _ := c.Collect(context.Background(), from, to)
	ev[0].ID = "changed"
	ev, _ = c.Collect(context.Background(), from, to)
	if ev[0].ID != "a" {
		t.Errorf("cache was changed through its result: %+v", ev)
	}
}
