// Package macagent reads ActivityWatch on the Mac and sends the server only
// categories and minutes. Window titles and full URLs stay in this process:
// they are read, reduced to an app name or a domain, and dropped.
package macagent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Bucket is an ActivityWatch bucket.
type Bucket struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// rawEvent is an ActivityWatch event. Data holds the title or the URL; it is
// never passed on beyond the reduction in Segments.
type rawEvent struct {
	Timestamp time.Time      `json:"timestamp"`
	Duration  float64        `json:"duration"`
	Data      map[string]any `json:"data"`
}

func (e rawEvent) span() Span {
	return Span{e.Timestamp, e.Timestamp.Add(time.Duration(e.Duration * float64(time.Second)))}
}

// Span is a period of time.
type Span struct{ Start, End time.Time }

func (s Span) Minutes() float64 { return s.End.Sub(s.Start).Minutes() }

// AW is a client of the ActivityWatch REST API (aw-server on localhost:5600).
type AW struct {
	BaseURL string
	HTTP    *http.Client
}

func (a *AW) get(ctx context.Context, path string, q url.Values, out any) error {
	u := strings.TrimRight(a.BaseURL, "/") + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	client := a.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("ActivityWatch is not reachable at %s", a.BaseURL)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ActivityWatch answered %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 256<<20)).Decode(out)
}

func (a *AW) Buckets(ctx context.Context) ([]Bucket, error) {
	var m map[string]Bucket
	if err := a.get(ctx, "/api/0/buckets/", nil, &m); err != nil {
		return nil, err
	}
	out := make([]Bucket, 0, len(m))
	for id, b := range m {
		b.ID = id
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (a *AW) events(ctx context.Context, bucket string, from, to time.Time) ([]rawEvent, error) {
	q := url.Values{"start": {from.Format(time.RFC3339)}, "end": {to.Format(time.RFC3339)}, "limit": {"-1"}}
	var out []rawEvent
	err := a.get(ctx, "/api/0/buckets/"+url.PathEscape(bucket)+"/events", q, &out)
	return out, err
}

// Bucket types of the standard watchers.
const (
	typeWindow = "currentwindow"   // aw-watcher-window
	typeAFK    = "afkstatus"       // aw-watcher-afk
	typeWeb    = "web.tab.current" // aw-watcher-web
)

// Seg is time spent on one app or one domain. Key is a name, never a title or an address.
type Seg struct {
	Key  string
	Span Span
	// Browser: a browser window with no known domain. It is unknown by
	// definition and is not asked about.
	Browser bool
}

var browsers = map[string]bool{
	"safari": true, "google chrome": true, "firefox": true, "arc": true, "brave browser": true,
	"microsoft edge": true, "opera": true, "vivaldi": true, "chromium": true, "yandex": true, "orion": true,
}

// Domain reduces a page address to its host without "www.". An address that is
// not an http(s) page gives "".
func Domain(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	h := strings.ToLower(u.Hostname())
	h = strings.TrimPrefix(h, "www.")
	if h == "" || !strings.Contains(h, ".") && h != "localhost" {
		return ""
	}
	return h
}

func str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

// Segments reads the day's events and reduces them to app and domain time,
// counting only the time the user was not away from the keyboard. The titles
// and the addresses are used for nothing but picking the domain.
func (a *AW) Segments(ctx context.Context, from, to time.Time) ([]Seg, error) {
	buckets, err := a.Buckets(ctx)
	if err != nil {
		return nil, err
	}
	var windows, afk, web []rawEvent
	var haveAFK, haveWindow bool
	for _, b := range buckets {
		switch {
		case b.Type == typeWindow || strings.HasPrefix(b.ID, "aw-watcher-window"):
			ev, err := a.events(ctx, b.ID, from, to)
			if err != nil {
				return nil, err
			}
			windows, haveWindow = append(windows, ev...), true
		case b.Type == typeAFK || strings.HasPrefix(b.ID, "aw-watcher-afk"):
			ev, err := a.events(ctx, b.ID, from, to)
			if err != nil {
				return nil, err
			}
			afk, haveAFK = append(afk, ev...), true
		case b.Type == typeWeb || strings.HasPrefix(b.ID, "aw-watcher-web"):
			ev, err := a.events(ctx, b.ID, from, to)
			if err != nil {
				return nil, err
			}
			web = append(web, ev...)
		}
	}
	if !haveWindow {
		return nil, fmt.Errorf("no aw-watcher-window bucket: install the window watcher")
	}
	return BuildSegments(windows, afk, haveAFK, web, from, to), nil
}

// BuildSegments is the pure part of Segments.
func BuildSegments(windows, afk []rawEvent, haveAFK bool, web []rawEvent, from, to time.Time) []Seg {
	window := Span{from, to}
	var active []Span
	if haveAFK {
		for _, e := range afk {
			if str(e.Data, "status") == "not-afk" {
				active = append(active, e.span())
			}
		}
		active = mergeSpans(active)
	} else {
		active = []Span{window}
	}

	// Web events with their domain; incognito pages give no domain.
	type webSpan struct {
		span   Span
		domain string
	}
	var pages []webSpan
	for _, e := range web {
		if incog, _ := e.Data["incognito"].(bool); incog {
			continue
		}
		if d := Domain(str(e.Data, "url")); d != "" {
			pages = append(pages, webSpan{e.span(), d})
		}
	}

	sort.Slice(pages, func(i, j int) bool { return pages[i].span.Start.Before(pages[j].span.Start) })

	var out []Seg
	for _, w := range windows {
		app := strings.ToLower(strings.TrimSpace(str(w.Data, "app")))
		if app == "" {
			continue
		}
		for _, part := range intersect([]Span{w.span()}, active, window) {
			if !browsers[app] {
				out = append(out, Seg{Key: app, Span: part})
				continue
			}
			// A browser window: its time goes to the domains it showed.
			cursor := part.Start
			for _, p := range pages {
				for _, sub := range intersect([]Span{part}, []Span{p.span}, window) {
					if sub.Start.After(cursor) {
						out = append(out, Seg{Key: app, Span: Span{cursor, sub.Start}, Browser: true})
					}
					out = append(out, Seg{Key: p.domain, Span: sub})
					if sub.End.After(cursor) {
						cursor = sub.End
					}
				}
			}
			if part.End.After(cursor) {
				out = append(out, Seg{Key: app, Span: Span{cursor, part.End}, Browser: true})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Span.Start.Before(out[j].Span.Start) })
	return out
}

func mergeSpans(in []Span) []Span {
	if len(in) == 0 {
		return nil
	}
	s := append([]Span(nil), in...)
	sort.Slice(s, func(i, j int) bool { return s[i].Start.Before(s[j].Start) })
	out := []Span{s[0]}
	for _, p := range s[1:] {
		last := &out[len(out)-1]
		if !p.Start.After(last.End) {
			if p.End.After(last.End) {
				last.End = p.End
			}
			continue
		}
		out = append(out, p)
	}
	return out
}

// intersect returns the parts of a that lie inside b, clipped to the window.
func intersect(a, b []Span, window Span) []Span {
	var out []Span
	for _, x := range a {
		for _, y := range b {
			s, e := maxT(maxT(x.Start, y.Start), window.Start), minT(minT(x.End, y.End), window.End)
			if e.After(s) {
				out = append(out, Span{s, e})
			}
		}
	}
	return out
}

func maxT(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minT(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
