// Package timeparse turns the date parts a model extracts from a phrase into
// real dates. The model never computes dates: it names a weekday or an offset,
// and this package resolves it against the current moment and time zone.
package timeparse

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Date is the `date` / `deadline` object of PLAN.md section 13.
type Date struct {
	Type  string  `json:"type"`            // relative | weekday | absolute
	Days  *int    `json:"days,omitempty"`  // relative: 0 today, 1 tomorrow, 2 the day after
	Value string  `json:"value,omitempty"` // weekday: mon..sun; absolute: YYYY-MM-DD
	Week  *string `json:"week,omitempty"`  // weekday: "this", "next" or null
}

// Clock is a time of day.
type Clock struct{ Hour, Min int }

func (c Clock) String() string { return fmt.Sprintf("%02d:%02d", c.Hour, c.Min) }

func (c Clock) minutes() int { return c.Hour*60 + c.Min }

// ParseClock reads "H:MM" or "HH:MM" (24-hour, 00:00..23:59).
func ParseClock(s string) (Clock, error) {
	h, m, ok := strings.Cut(s, ":")
	if !ok || len(h) < 1 || len(h) > 2 || len(m) != 2 || !digits(h) || !digits(m) {
		return Clock{}, fmt.Errorf("time %q: want HH:MM", s)
	}
	hour, _ := strconv.Atoi(h)
	min, _ := strconv.Atoi(m)
	if hour > 23 || min > 59 {
		return Clock{}, fmt.Errorf("time %q is out of range", s)
	}
	return Clock{hour, min}, nil
}

func digits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

var weekdays = map[string]int{"mon": 0, "tue": 1, "wed": 2, "thu": 3, "fri": 4, "sat": 5, "sun": 6}

// Resolve returns local midnight of the date described by d.
//
// at is the time of day the date will be used with (the start of an event).
// It only matters for a weekday without a week: if that weekday is today and
// at has already passed, the next week's day is meant. Pass nil when there is no time.
func Resolve(d Date, now time.Time, loc *time.Location, at *Clock) (time.Time, error) {
	now = now.In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	switch d.Type {
	case "relative":
		if d.Days == nil || *d.Days < 0 || *d.Days > 2 {
			return time.Time{}, fmt.Errorf("relative date: days must be 0..2")
		}
		return addDays(today, *d.Days, loc), nil

	case "weekday":
		want, ok := weekdays[d.Value]
		if !ok {
			return time.Time{}, fmt.Errorf("weekday %q: want mon..sun", d.Value)
		}
		cur := (int(today.Weekday()) + 6) % 7 // Monday = 0
		week := ""
		if d.Week != nil {
			week = *d.Week
		}
		switch week {
		case "this":
			return addDays(today, want-cur, loc), nil
		case "next":
			return addDays(today, want-cur+7, loc), nil
		case "":
			delta := (want - cur + 7) % 7
			if delta == 0 && at != nil && at.minutes() <= now.Hour()*60+now.Minute() {
				delta = 7
			}
			return addDays(today, delta, loc), nil
		}
		return time.Time{}, fmt.Errorf("week %q: want this, next or null", week)

	case "absolute":
		t, err := time.ParseInLocation("2006-01-02", d.Value, loc)
		if err != nil || len(d.Value) != 10 {
			return time.Time{}, fmt.Errorf("absolute date %q: want YYYY-MM-DD", d.Value)
		}
		return t, nil
	}
	return time.Time{}, fmt.Errorf("date type %q: want relative, weekday or absolute", d.Type)
}

// At combines a local date with a time of day.
func At(date time.Time, c Clock, loc *time.Location) time.Time {
	return time.Date(date.Year(), date.Month(), date.Day(), c.Hour, c.Min, 0, 0, loc)
}

// addDays goes through time.Date so that daylight-saving days keep their wall clock.
func addDays(t time.Time, n int, loc *time.Location) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day()+n, 0, 0, 0, 0, loc)
}
