package calendar

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// maxExpand bounds the loop for pathological rules.
const maxExpand = 200000

type rule struct {
	freq     string
	interval int
	count    int // 0 = unlimited
	until    time.Time
	hasUntil bool
	byDay    []time.Weekday
}

var weekdayCodes = map[string]time.Weekday{
	"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday, "WE": time.Wednesday,
	"TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday,
}

func parseRule(s string, loc *time.Location) (rule, error) {
	r := rule{interval: 1}
	for _, part := range strings.Split(s, ";") {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			return r, fmt.Errorf("bad RRULE part %q", part)
		}
		switch k {
		case "FREQ":
			r.freq = v
		case "INTERVAL":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				return r, fmt.Errorf("bad INTERVAL %q", v)
			}
			r.interval = n
		case "COUNT":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				return r, fmt.Errorf("bad COUNT %q", v)
			}
			r.count = n
		case "UNTIL":
			t, _, err := parseICSTime(v, "", loc)
			if err != nil {
				return r, fmt.Errorf("bad UNTIL %q", v)
			}
			r.until, r.hasUntil = t, true
		case "BYDAY":
			for _, d := range strings.Split(v, ",") {
				wd, ok := weekdayCodes[d]
				if !ok {
					return r, fmt.Errorf("unsupported BYDAY %q", d)
				}
				r.byDay = append(r.byDay, wd)
			}
		case "WKST":
			// Monday-based weeks are assumed.
		default:
			return r, fmt.Errorf("unsupported RRULE part %s", k)
		}
	}
	switch r.freq {
	case "DAILY", "WEEKLY", "MONTHLY", "YEARLY":
	default:
		return r, fmt.Errorf("unsupported FREQ %q", r.freq)
	}
	if len(r.byDay) > 0 && r.freq != "WEEKLY" {
		return r, fmt.Errorf("BYDAY is supported only with FREQ=WEEKLY")
	}
	return r, nil
}

// expand returns occurrence starts of the series that begin before `to` and, for
// a simple sort key, are not before `from-maxLen` (the caller filters by overlap).
// Occurrences keep the wall-clock time of dtstart in its own zone, so daylight
// saving changes do not shift them.
func (r rule) expand(dtstart time.Time, from, to time.Time, maxLen time.Duration) []time.Time {
	loc := dtstart.Location()
	y, m, d := dtstart.Date()
	h, mi, s := dtstart.Clock()
	at := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, h, mi, s, 0, loc) }

	var out []time.Time
	n := 0 // occurrences generated so far (COUNT includes those before the window)
	emit := func(t time.Time) bool {
		if t.Before(dtstart) {
			return true
		}
		if r.hasUntil && t.After(r.until) {
			return false
		}
		if !t.Before(to) {
			return false
		}
		n++
		if !t.Add(maxLen).Before(from) {
			out = append(out, t)
		}
		return r.count == 0 || n < r.count
	}

	switch r.freq {
	case "DAILY":
		for i := 0; i < maxExpand; i++ {
			if !emit(at(y, m, d+i*r.interval)) {
				break
			}
		}
	case "WEEKLY":
		days := r.byDay
		if len(days) == 0 {
			days = []time.Weekday{dtstart.Weekday()}
		}
		// Offsets from Monday, in week order.
		offs := make([]int, 0, len(days))
		for _, wd := range days {
			offs = append(offs, (int(wd)+6)%7)
		}
		sortInts(offs)
		mondayOff := (int(dtstart.Weekday()) + 6) % 7
	weeks:
		for w := 0; w < maxExpand; w++ {
			base := d - mondayOff + w*7*r.interval
			for _, o := range offs {
				if !emit(at(y, m, base+o)) {
					break weeks
				}
			}
		}
	case "MONTHLY":
		for i := 0; i < maxExpand; i++ {
			t := at(y, m+time.Month(i*r.interval), d)
			if t.Day() != d { // month shorter than d: skip, as RFC 5545 does
				if !t.Before(to) {
					break
				}
				continue
			}
			if !emit(t) {
				break
			}
		}
	case "YEARLY":
		for i := 0; i < maxExpand; i++ {
			t := at(y+i*r.interval, m, d)
			if t.Day() != d {
				if !t.Before(to) {
					break
				}
				continue
			}
			if !emit(t) {
				break
			}
		}
	}
	return out
}

func sortInts(a []int) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

// ValidateRRULE checks the syntax of an RFC 5545 rule against the subset the
// agent can read back and write: FREQ, INTERVAL, COUNT, UNTIL, BYDAY (weekly only).
func ValidateRRULE(s string, loc *time.Location) error {
	_, err := parseRule(s, loc)
	return err
}
