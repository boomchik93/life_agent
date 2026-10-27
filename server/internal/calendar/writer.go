package calendar

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/oauth2/google"
)

const (
	scopeEvents      = "https://www.googleapis.com/auth/calendar.events"
	categoryProperty = "category" // key in the event's private extended properties
)

// ErrNoWriteAccess means the service account may not create events in the calendar.
var ErrNoWriteAccess = errors.New("calendar: no write access")

// NewEvent is an event the agent creates. ID is the Google event id; it comes
// from the idempotency key, so a repeated request cannot create a second event.
type NewEvent struct {
	ID         string
	Title      string
	Category   string
	Recurrence string // RFC 5545 rule without the RRULE: prefix, or empty
	Start, End time.Time
}

// EventID derives a valid Google event id (lowercase base32hex characters,
// 5..1024 long) from an idempotency key.
func EventID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])[:40]
}

// Writer creates events in one calendar with a service-account key. The
// calendar must be shared with the account's e-mail with the right to change events.
type Writer struct {
	client     *http.Client
	baseURL    string
	calendarID string
	loc        *time.Location
}

func NewGoogleWriter(ctx context.Context, keyJSON []byte, calendarID string, loc *time.Location) (*Writer, error) {
	cfg, err := google.JWTConfigFromJSON(keyJSON, scopeEvents)
	if err != nil {
		return nil, fmt.Errorf("service account key: %w", err)
	}
	client := cfg.Client(ctx)
	client.Timeout = googleTimeout
	return &Writer{client: client, baseURL: googleBase, calendarID: calendarID, loc: loc}, nil
}

type gDateTime struct {
	DateTime string `json:"dateTime"`
	TimeZone string `json:"timeZone"`
}

// Insert creates the event. created is false when an event with this id
// already exists: the earlier request got through, so nothing new is made.
func (w *Writer) Insert(ctx context.Context, e NewEvent) (created bool, err error) {
	body := map[string]any{
		"id":      e.ID,
		"summary": e.Title,
		"start":   gDateTime{e.Start.In(w.loc).Format(time.RFC3339), w.loc.String()},
		"end":     gDateTime{e.End.In(w.loc).Format(time.RFC3339), w.loc.String()},
		// The user's own reminder defaults apply to the event.
		"reminders":          map[string]bool{"useDefault": true},
		"extendedProperties": map[string]any{"private": map[string]string{categoryProperty: e.Category}},
	}
	if e.Recurrence != "" {
		body["recurrence"] = []string{"RRULE:" + e.Recurrence}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return false, err
	}
	u := fmt.Sprintf("%s/calendars/%s/events", w.baseURL, url.PathEscape(w.calendarID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(raw))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.client.Do(req)
	if err != nil {
		return false, err
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusConflict:
		return false, nil
	case http.StatusForbidden, http.StatusNotFound:
		return false, ErrNoWriteAccess
	}
	return false, fmt.Errorf("google api: status %d", resp.StatusCode)
}
