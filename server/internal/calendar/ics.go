package calendar

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxICSBytes = 5 << 20

// icsReuse is how long a downloaded feed is parsed again without asking the
// server; a schedule changes seldom, and every week of the cache reads it.
const icsReuse = 10 * time.Minute

// ICS reads a public iCalendar feed (for example a class schedule).
type ICS struct {
	url    string
	name   string
	loc    *time.Location
	client *http.Client
	now    func() time.Time

	mu           sync.Mutex
	body         string
	fetchedAt    time.Time
	etag         string
	lastModified string
}

func NewICS(feedURL, name string, loc *time.Location) *ICS {
	return &ICS{url: feedURL, name: name, loc: loc, client: &http.Client{Timeout: 20 * time.Second}, now: time.Now}
}

func (c *ICS) Events(ctx context.Context, from, to time.Time) ([]Event, error) {
	body, err := c.feed(ctx)
	if err != nil {
		return nil, err
	}
	events, warnings := ParseICS(body, c.name, c.loc, from, to)
	if len(warnings) > 0 && len(events) == 0 {
		return nil, fmt.Errorf("%s", warnings[0])
	}
	return events, nil
}

// feed returns the text of the feed. A recent copy is used as is; an older
// one is checked with the validators the server gave, so an unchanged feed is
// not downloaded again.
func (c *ICS) feed(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.body != "" && c.now().Sub(c.fetchedAt) < icsReuse {
		return c.body, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return "", fmt.Errorf("bad feed url")
	}
	req.Header.Set("User-Agent", "life-agent/1")
	if c.body != "" {
		if c.etag != "" {
			req.Header.Set("If-None-Match", c.etag)
		}
		if c.lastModified != "" {
			req.Header.Set("If-Modified-Since", c.lastModified)
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		// The URL is not part of the message: it may carry personal parameters.
		return "", fmt.Errorf("feed request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified && c.body != "" {
		c.fetchedAt = c.now()
		return c.body, nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("feed status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxICSBytes))
	if err != nil {
		return "", fmt.Errorf("feed read failed")
	}
	c.body, c.fetchedAt = string(raw), c.now()
	c.etag, c.lastModified = resp.Header.Get("ETag"), resp.Header.Get("Last-Modified")
	return c.body, nil
}

type icsProp struct {
	name   string
	params map[string]string
	value  string
}

type vevent struct {
	props []icsProp
}

func (v vevent) get(name string) (icsProp, bool) {
	for _, p := range v.props {
		if p.name == name {
			return p, true
		}
	}
	return icsProp{}, false
}

func (v vevent) all(name string) []icsProp {
	var out []icsProp
	for _, p := range v.props {
		if p.name == name {
			out = append(out, p)
		}
	}
	return out
}

// ParseICS extracts the events overlapping [from, to) from iCalendar text,
// expanding recurrence rules. Unsupported constructs become warnings and the
// event is kept as a single occurrence.
func ParseICS(text, calendarName string, defLoc *time.Location, from, to time.Time) ([]Event, []string) {
	var warnings []string
	var events []vevent
	var cur *vevent
	nested := 0 // depth of sub-components (VALARM) inside the current VEVENT
	for _, line := range unfold(text) {
		switch {
		case line == "BEGIN:VEVENT":
			cur = &vevent{}
			nested = 0
		case line == "END:VEVENT":
			if cur != nil {
				events = append(events, *cur)
			}
			cur = nil
		case cur != nil && strings.HasPrefix(line, "BEGIN:"):
			nested++
		case cur != nil && strings.HasPrefix(line, "END:"):
			if nested > 0 {
				nested--
			}
		case cur != nil && nested == 0:
			// Properties of sub-components (an alarm's SUMMARY or DURATION) are not the event's.
			if p, ok := parseProp(line); ok {
				cur.props = append(cur.props, p)
			}
		}
	}

	// Occurrences replaced by a RECURRENCE-ID override are dropped from the series.
	overridden := map[string]bool{}
	for _, ev := range events {
		uid, _ := ev.get("UID")
		rid, ok := ev.get("RECURRENCE-ID")
		if !ok {
			continue
		}
		if t, _, err := parseICSTime(rid.value, rid.params["TZID"], defLoc); err == nil {
			overridden[uid.value+"|"+strconv.FormatInt(t.Unix(), 10)] = true
		}
	}

	var out []Event
	for _, ev := range events {
		if st, ok := ev.get("STATUS"); ok && strings.EqualFold(st.value, "CANCELLED") {
			continue
		}
		e, err := buildEvents(ev, calendarName, defLoc, from, to, overridden)
		if err != nil {
			title := ""
			if s, ok := ev.get("SUMMARY"); ok {
				title = s.value
			}
			warnings = append(warnings, fmt.Sprintf("ics event %q: %v", title, err))
		}
		out = append(out, e...)
	}
	return out, warnings
}

func buildEvents(ev vevent, calName string, defLoc *time.Location, from, to time.Time, overridden map[string]bool) ([]Event, error) {
	ds, ok := ev.get("DTSTART")
	if !ok {
		return nil, fmt.Errorf("no DTSTART")
	}
	start, allDay, err := parseICSTime(ds.value, ds.params["TZID"], defLoc)
	if err != nil {
		return nil, err
	}

	var length time.Duration
	if de, ok := ev.get("DTEND"); ok {
		end, _, err := parseICSTime(de.value, de.params["TZID"], defLoc)
		if err != nil {
			return nil, err
		}
		length = end.Sub(start)
	} else if du, ok := ev.get("DURATION"); ok {
		if length, err = parseDuration(du.value); err != nil {
			return nil, err
		}
	} else if allDay {
		length = 24 * time.Hour
	}

	uid, _ := ev.get("UID")
	summary, _ := ev.get("SUMMARY")
	title := unescapeText(summary.value)
	if title == "" {
		title = "(без названия)"
	}

	make1 := func(s time.Time, recurring bool) Event {
		e := Event{
			ID: uid.value, Source: "ics", Calendar: calName, Title: title,
			Start: s, End: s.Add(length), AllDay: allDay, Recurring: recurring,
		}
		if allDay {
			// Whole days: advance by calendar days, not 24h, to survive DST changes.
			// Rounded: a span over a DST change is 23h or 25h long.
			days := int((length + 12*time.Hour) / (24 * time.Hour))
			e.End = time.Date(s.Year(), s.Month(), s.Day()+days, 0, 0, 0, 0, s.Location())
		} else {
			e.Start, e.End = e.Start.In(defLoc), e.End.In(defLoc)
		}
		if recurring {
			e.ID = uid.value + "@" + strconv.FormatInt(s.Unix(), 10)
		}
		return e
	}

	rr, hasRule := ev.get("RRULE")
	if !hasRule {
		e := make1(start, false)
		if overlaps(e.Start, e.End, from, to) {
			return []Event{e}, nil
		}
		return nil, nil
	}

	r, err := parseRule(rr.value, start.Location())
	if err != nil {
		e := make1(start, false)
		if overlaps(e.Start, e.End, from, to) {
			return []Event{e}, fmt.Errorf("%v (shown once)", err)
		}
		return nil, fmt.Errorf("%v (shown once)", err)
	}

	skip := map[int64]bool{}
	for _, ex := range ev.all("EXDATE") {
		for _, v := range strings.Split(ex.value, ",") {
			if t, _, err := parseICSTime(v, ex.params["TZID"], defLoc); err == nil {
				skip[t.Unix()] = true
			}
		}
	}

	var out []Event
	for _, s := range r.expand(start, from, to, length) {
		if skip[s.Unix()] || overridden[uid.value+"|"+strconv.FormatInt(s.Unix(), 10)] {
			continue
		}
		e := make1(s, true)
		if overlaps(e.Start, e.End, from, to) {
			out = append(out, e)
		}
	}
	return out, nil
}

// parseICSTime parses an iCalendar DATE or DATE-TIME. A trailing Z means UTC,
// a TZID names the zone, otherwise the time is floating and read in defLoc.
// Instants are returned in defLoc's zone so the API shows the user's local time.
func parseICSTime(value, tzid string, defLoc *time.Location) (time.Time, bool, error) {
	if len(value) == 8 {
		t, err := time.ParseInLocation("20060102", value, defLoc)
		return t, true, err
	}
	if strings.HasSuffix(value, "Z") {
		t, err := time.ParseInLocation("20060102T150405Z", value, time.UTC)
		return t.In(defLoc), false, err
	}
	loc := defLoc
	if tzid != "" {
		l, err := time.LoadLocation(tzid)
		if err != nil {
			return time.Time{}, false, fmt.Errorf("unknown time zone %q", tzid)
		}
		loc = l
	}
	t, err := time.ParseInLocation("20060102T150405", value, loc)
	return t, false, err
}

// parseDuration reads an RFC 5545 duration such as PT1H30M, P1D or P1W.
func parseDuration(s string) (time.Duration, error) {
	orig := s
	neg := false
	if strings.HasPrefix(s, "-") {
		neg, s = true, s[1:]
	} else if strings.HasPrefix(s, "+") {
		s = s[1:]
	}
	if !strings.HasPrefix(s, "P") {
		return 0, fmt.Errorf("bad DURATION %q", orig)
	}
	s = s[1:]
	var d time.Duration
	inTime := false
	num := ""
	for _, ch := range s {
		switch {
		case ch >= '0' && ch <= '9':
			num += string(ch)
		case ch == 'T':
			inTime = true
		default:
			n, err := strconv.Atoi(num)
			if err != nil {
				return 0, fmt.Errorf("bad DURATION %q", orig)
			}
			num = ""
			switch {
			case ch == 'W' && !inTime:
				d += time.Duration(n) * 7 * 24 * time.Hour
			case ch == 'D' && !inTime:
				d += time.Duration(n) * 24 * time.Hour
			case ch == 'H' && inTime:
				d += time.Duration(n) * time.Hour
			case ch == 'M' && inTime:
				d += time.Duration(n) * time.Minute
			case ch == 'S' && inTime:
				d += time.Duration(n) * time.Second
			default:
				return 0, fmt.Errorf("bad DURATION %q", orig)
			}
		}
	}
	if neg {
		d = -d
	}
	return d, nil
}

func unfold(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		if (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")) && len(lines) > 0 {
			lines[len(lines)-1] += l[1:]
			continue
		}
		lines = append(lines, l)
	}
	return lines
}

func parseProp(line string) (icsProp, bool) {
	// The name/params end at the first ':' outside double quotes.
	inQuote := false
	idx := -1
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '"':
			inQuote = !inQuote
		case ':':
			if !inQuote {
				idx = i
			}
		}
		if idx >= 0 {
			break
		}
	}
	if idx < 0 {
		return icsProp{}, false
	}
	head, value := line[:idx], line[idx+1:]
	parts := strings.Split(head, ";")
	p := icsProp{name: strings.ToUpper(parts[0]), params: map[string]string{}, value: value}
	for _, kv := range parts[1:] {
		if k, v, ok := strings.Cut(kv, "="); ok {
			p.params[strings.ToUpper(k)] = strings.Trim(v, `"`)
		}
	}
	return p, true
}

func unescapeText(s string) string {
	r := strings.NewReplacer(`\n`, "\n", `\N`, "\n", `\,`, ",", `\;`, ";", `\\`, `\`)
	return r.Replace(s)
}
