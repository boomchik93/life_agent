package calendar

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"golang.org/x/oauth2/google"
)

const (
	googleBase     = "https://www.googleapis.com/calendar/v3"
	scopeReadonly  = "https://www.googleapis.com/auth/calendar.readonly"
	googlePageSize = 250
	googleTimeout  = 20 * time.Second
)

// Google reads events from Google Calendar with a service-account key.
// Every calendar must be shared with the service account's e-mail.
type Google struct {
	client      *http.Client
	baseURL     string
	calendarIDs []string
	loc         *time.Location
}

func NewGoogle(ctx context.Context, keyJSON []byte, calendarIDs []string, loc *time.Location) (*Google, error) {
	cfg, err := google.JWTConfigFromJSON(keyJSON, scopeReadonly)
	if err != nil {
		return nil, fmt.Errorf("service account key: %w", err)
	}
	client := cfg.Client(ctx)
	// A hung API call must not pin the request and its goroutines forever.
	client.Timeout = googleTimeout
	return &Google{client: client, baseURL: googleBase, calendarIDs: calendarIDs, loc: loc}, nil
}

type gEvent struct {
	ID               string `json:"id"`
	Status           string `json:"status"`
	Summary          string `json:"summary"`
	RecurringEventID string `json:"recurringEventId"`
	Start            gTime  `json:"start"`
	End              gTime  `json:"end"`
	Extended         struct {
		Private map[string]string `json:"private"`
	} `json:"extendedProperties"`
}

type gTime struct {
	Date     string `json:"date"`
	DateTime string `json:"dateTime"`
}

type gList struct {
	Summary       string   `json:"summary"`
	Items         []gEvent `json:"items"`
	NextPageToken string   `json:"nextPageToken"`
}

func (g *Google) Events(ctx context.Context, from, to time.Time) ([]Event, error) {
	type result struct {
		events []Event
		err    error
	}
	results := make([]result, len(g.calendarIDs))
	var wg sync.WaitGroup
	for i, id := range g.calendarIDs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ev, err := g.calendarEvents(ctx, id, from, to)
			if err != nil {
				err = fmt.Errorf("calendar %s: %w", id, err)
			}
			results[i] = result{ev, err}
		}()
	}
	wg.Wait()

	var out []Event
	var firstErr error
	for _, r := range results {
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		out = append(out, r.events...)
	}
	// Partial data is still useful; report the error only if nothing was read.
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

func (g *Google) calendarEvents(ctx context.Context, calID string, from, to time.Time) ([]Event, error) {
	var out []Event
	page := ""
	for {
		q := url.Values{
			"timeMin":      {from.Format(time.RFC3339)},
			"timeMax":      {to.Format(time.RFC3339)},
			"singleEvents": {"true"}, // expand recurring series into instances
			"orderBy":      {"startTime"},
			"maxResults":   {fmt.Sprint(googlePageSize)},
		}
		if page != "" {
			q.Set("pageToken", page)
		}
		u := fmt.Sprintf("%s/calendars/%s/events?%s", g.baseURL, url.PathEscape(calID), q.Encode())
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		resp, err := g.client.Do(req)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("google api: status %d", resp.StatusCode)
		}
		var list gList
		if err := json.Unmarshal(body, &list); err != nil {
			return nil, fmt.Errorf("decode: %w", err)
		}
		for _, ge := range list.Items {
			if ge.Status == "cancelled" {
				continue
			}
			ev, err := g.convert(ge, calID, list.Summary)
			if err != nil {
				return nil, err
			}
			out = append(out, ev)
		}
		if list.NextPageToken == "" {
			return out, nil
		}
		page = list.NextPageToken
	}
}

func (g *Google) convert(ge gEvent, calID, calName string) (Event, error) {
	ev := Event{
		ID:        ge.ID,
		Source:    "google",
		Calendar:  calName,
		Title:     ge.Summary,
		Recurring: ge.RecurringEventID != "",
		Category:  ge.Extended.Private[categoryProperty],
	}
	if ev.Calendar == "" {
		ev.Calendar = calID
	}
	if ev.Title == "" {
		ev.Title = "(без названия)"
	}
	var err error
	if ge.Start.Date != "" {
		ev.AllDay = true
		if ev.Start, err = time.ParseInLocation("2006-01-02", ge.Start.Date, g.loc); err != nil {
			return ev, err
		}
		if ev.End, err = time.ParseInLocation("2006-01-02", ge.End.Date, g.loc); err != nil {
			return ev, err
		}
		return ev, nil
	}
	if ev.Start, err = time.Parse(time.RFC3339, ge.Start.DateTime); err != nil {
		return ev, err
	}
	if ev.End, err = time.Parse(time.RFC3339, ge.End.DateTime); err != nil {
		return ev, err
	}
	ev.Start, ev.End = ev.Start.In(g.loc), ev.End.In(g.loc)
	return ev, nil
}
