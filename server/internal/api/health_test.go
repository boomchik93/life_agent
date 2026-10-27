package api

import (
	"strings"
	"testing"

	"lifeagent/internal/calendar"
)

func TestHealthDailyStoresAndDeletes(t *testing.T) {
	h := writeServer(t, []calendar.Event{{ID: "1", Source: "google", Calendar: "c", Title: "x", Start: at(27, 10, 0), End: at(27, 11, 0)}}, &fakeWriter{})

	rec := post(h, "/v1/health/daily", "", `{"date":"2026-10-27","sleep_hours":5,"avg_hr":61}`)
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	// Measured sleep, no check-in typed: the sleep risk is built from it.
	var week weekResponse
	decodeJSON(t, request(h, "GET", "/v1/week", ""), &week)
	if week.Risks.Sleep.Color != "red" || week.Risks.Sleep.Metrics["avg_sleep_hours"] != 5 {
		t.Errorf("sleep risk: %+v", week.Risks.Sleep)
	}
	if week.Risks.State.Color != "gray" {
		t.Errorf("health must not invent the state: %+v", week.Risks.State)
	}

	// The same day again, only the stress: sleep stays.
	post(h, "/v1/health/daily", "", `{"date":"2026-10-27","stress":20}`)
	decodeJSON(t, request(h, "GET", "/v1/week", ""), &week)
	if week.Risks.Sleep.Metrics["avg_sleep_hours"] != 5 {
		t.Errorf("a partial summary wiped the sleep: %+v", week.Risks.Sleep)
	}

	rec = request(h, "DELETE", "/v1/health", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"deleted":1`) {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	decodeJSON(t, request(h, "GET", "/v1/week", ""), &week)
	if week.Risks.Sleep.Color != "gray" {
		t.Errorf("after delete the sleep is unknown: %+v", week.Risks.Sleep)
	}
}

func TestHealthDailyRejectsBadInput(t *testing.T) {
	h := writeServer(t, nil, &fakeWriter{})
	for name, body := range map[string]string{
		"empty":        `{}`,
		"not json":     `[`,
		"future":       `{"date":"2026-10-29","sleep_hours":7}`,
		"bad date":     `{"date":"27.10","sleep_hours":7}`,
		"sleep 30":     `{"sleep_hours":30}`,
		"hr 5":         `{"avg_hr":5}`,
		"stress 101":   `{"stress":101}`,
		"unknown key":  `{"sleep_hours":7,"steps":10}`,
		"window title": `{"sleep_hours":7,"title":"x"}`,
	} {
		if rec := post(h, "/v1/health/daily", "", body); rec.Code != 400 {
			t.Errorf("%s: status %d", name, rec.Code)
		}
	}
	if rec := post(h, "/v1/health/daily", "", `{"sleep_hours":7}`); rec.Code != 200 {
		t.Errorf("date defaults to today: %d %s", rec.Code, rec.Body)
	}
}
