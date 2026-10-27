// Package weather reads the forecast from Open-Meteo (no key) for the city
// the user entered in the app. Dry, light hours are where the planner puts
// sport (PLAN.md stage 10.3). Weather is not a thing the user did, so its rows
// carry no minutes: only numbers in the free-form field.
package weather

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"lifeagent/internal/activity"
)

// SettingKey is where the location lives in the settings table.
const SettingKey = "weather"

// CacheFor is how long a forecast is reused (PLAN.md section 14).
const CacheFor = 3 * time.Hour

// Every is how often the collector looks whether a fetch is due: a changed
// city is picked up within this time, not after the cache runs out.
const Every = 15 * time.Minute

// Location is the place the user chose. Lat and Lon are filled in by the
// server from the city name when the user gives only the name.
type Location struct {
	City string   `json:"city"`
	Lat  *float64 `json:"lat,omitempty"`
	Lon  *float64 `json:"lon,omitempty"`
}

// ParseLocation reads and checks the stored setting.
func ParseLocation(raw []byte) (Location, error) {
	var l Location
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&l); err != nil {
		return Location{}, err
	}
	l.City = strings.TrimSpace(l.City)
	if n := utf8.RuneCountInString(l.City); n < 1 || n > 80 || strings.ContainsAny(l.City, "<>\"\n\r\t{}") {
		return Location{}, errors.New("city must be 1..80 characters")
	}
	if (l.Lat == nil) != (l.Lon == nil) {
		return Location{}, errors.New("lat and lon go together")
	}
	if l.Lat != nil && (*l.Lat < -90 || *l.Lat > 90 || *l.Lon < -180 || *l.Lon > 180) {
		return Location{}, errors.New("coordinates out of range")
	}
	return l, nil
}

// KV is the settings table; *store.Store is one.
type KV interface {
	Settings() (map[string]string, error)
	PutSettings(map[string]string) error
}

type Collector struct {
	Settings KV
	Loc      *time.Location
	HTTP     *http.Client
	// ForecastBase and GeocodeBase are the Open-Meteo hosts; tests replace them.
	ForecastBase, GeocodeBase string

	fetchedAt  time.Time
	fetchedFor string
}

func New(kv KV, loc *time.Location) *Collector {
	return &Collector{Settings: kv, Loc: loc,
		ForecastBase: "https://api.open-meteo.com", GeocodeBase: "https://geocoding-api.open-meteo.com"}
}

func (c *Collector) Source() string { return activity.Weather }

func (c *Collector) get(ctx context.Context, base, path string, q url.Values, out any) error {
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("open-meteo is not reachable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("open-meteo answered %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
}

// location reads the setting and, if only the name is there, finds the
// coordinates and saves them with it.
func (c *Collector) location(ctx context.Context) (Location, bool, error) {
	kv, err := c.Settings.Settings()
	if err != nil {
		return Location{}, false, err
	}
	raw, ok := kv[SettingKey]
	if !ok {
		return Location{}, false, nil
	}
	l, err := ParseLocation([]byte(raw))
	if err != nil {
		return Location{}, false, fmt.Errorf("weather setting: %w", err)
	}
	if l.Lat != nil {
		return l, true, nil
	}
	var geo struct {
		Results []struct {
			Name string  `json:"name"`
			Lat  float64 `json:"latitude"`
			Lon  float64 `json:"longitude"`
		} `json:"results"`
	}
	q := url.Values{"name": {l.City}, "count": {"1"}, "language": {"ru"}, "format": {"json"}}
	if err := c.get(ctx, c.GeocodeBase, "/v1/search", q, &geo); err != nil {
		return Location{}, false, err
	}
	if len(geo.Results) == 0 {
		return Location{}, false, fmt.Errorf("city %q was not found", l.City)
	}
	lat, lon := geo.Results[0].Lat, geo.Results[0].Lon
	l.Lat, l.Lon = &lat, &lon
	saved, err := json.Marshal(l)
	if err != nil {
		return Location{}, false, err
	}
	if err := c.Settings.PutSettings(map[string]string{SettingKey: string(saved)}); err != nil {
		return Location{}, false, err
	}
	return l, true, nil
}

func hourOf(iso string) (float64, bool) {
	// "2026-10-28T07:51"
	i := strings.IndexByte(iso, 'T')
	if i < 0 || len(iso) < i+6 {
		return 0, false
	}
	h, err1 := strconv.Atoi(iso[i+1 : i+3])
	m, err2 := strconv.Atoi(iso[i+4 : i+6])
	if err1 != nil || err2 != nil {
		return 0, false
	}
	return math.Round((float64(h)+float64(m)/60)*100) / 100, true
}

// Collect returns the forecast for the coming days, one row per date. It
// fetches at most every CacheFor, or at once when the place has changed.
func (c *Collector) Collect(ctx context.Context, now time.Time) ([]activity.DayRows, error) {
	l, ok, err := c.location(ctx)
	if err != nil || !ok {
		return nil, err
	}
	key := fmt.Sprintf("%s|%.4f|%.4f", l.City, *l.Lat, *l.Lon)
	if key == c.fetchedFor && now.Sub(c.fetchedAt) < CacheFor {
		return nil, nil
	}
	var resp struct {
		Daily struct {
			Time     []string  `json:"time"`
			TempMean []float64 `json:"temperature_2m_mean"`
			Precip   []float64 `json:"precipitation_sum"`
			Daylight []float64 `json:"daylight_duration"`
			Sunrise  []string  `json:"sunrise"`
			Sunset   []string  `json:"sunset"`
		} `json:"daily"`
	}
	q := url.Values{
		"latitude": {strconv.FormatFloat(*l.Lat, 'f', 4, 64)}, "longitude": {strconv.FormatFloat(*l.Lon, 'f', 4, 64)},
		"daily":    {"temperature_2m_mean,precipitation_sum,daylight_duration,sunrise,sunset"},
		"timezone": {c.Loc.String()}, "past_days": {"1"}, "forecast_days": {"7"},
	}
	if err := c.get(ctx, c.ForecastBase, "/v1/forecast", q, &resp); err != nil {
		return nil, err
	}
	d := resp.Daily
	var out []activity.DayRows
	for i, date := range d.Time {
		if _, err := time.Parse("2006-01-02", date); err != nil || i >= len(d.TempMean) || i >= len(d.Precip) || i >= len(d.Daylight) {
			continue
		}
		extra := map[string]any{
			"temp_c":         math.Round(d.TempMean[i]*10) / 10,
			"precip_mm":      math.Round(d.Precip[i]*10) / 10,
			"daylight_hours": math.Round(d.Daylight[i]/3600*10) / 10,
		}
		if i < len(d.Sunrise) {
			if h, ok := hourOf(d.Sunrise[i]); ok {
				extra["sunrise_h"] = h
			}
		}
		if i < len(d.Sunset) {
			if h, ok := hourOf(d.Sunset[i]); ok {
				extra["sunset_h"] = h
			}
		}
		out = append(out, activity.DayRows{Date: date, Rows: []activity.Row{
			{Category: activity.Unknown, Confidence: 1, Extra: extra},
		}})
	}
	if len(out) == 0 {
		return nil, errors.New("open-meteo returned no days")
	}
	c.fetchedAt, c.fetchedFor = now, key
	return out, nil
}
