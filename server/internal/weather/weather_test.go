package weather

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lifeagent/internal/activity"
	"lifeagent/internal/store"
)

var yekat, _ = time.LoadLocation("Asia/Yekaterinburg")

func at(day, h int) time.Time { return time.Date(2026, 10, day, h, 0, 0, 0, yekat) }

type fakeMeteo struct {
	forecasts, geocodes int
	lastQuery           string
	noCity              bool
}

func (f *fakeMeteo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/v1/search":
		f.geocodes++
		if f.noCity {
			fmt.Fprint(w, `{}`)
			return
		}
		fmt.Fprint(w, `{"results":[{"name":"Екатеринбург","latitude":56.8519,"longitude":60.6122}]}`)
	case "/v1/forecast":
		f.forecasts++
		f.lastQuery = r.URL.RawQuery
		fmt.Fprint(w, `{"daily":{"time":["2026-10-27","2026-10-28"],"temperature_2m_mean":[3.24,-1.5],
			"precipitation_sum":[0,4.26],"daylight_duration":[34200,33600],
			"sunrise":["2026-10-27T07:51","2026-10-28T07:53"],"sunset":["2026-10-27T17:21","2026-10-28T17:19"]}}`)
	default:
		w.WriteHeader(404)
	}
}

func setup(t *testing.T) (*Collector, *fakeMeteo, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	f := &fakeMeteo{}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c := New(st, yekat)
	c.ForecastBase, c.GeocodeBase = srv.URL, srv.URL
	return c, f, st
}

func TestNoCityNoRequests(t *testing.T) {
	c, f, _ := setup(t)
	days, err := c.Collect(context.Background(), at(27, 10))
	if err != nil || days != nil || f.forecasts+f.geocodes != 0 {
		t.Errorf("%v %v %+v", days, err, f)
	}
}

func TestCityBecomesCoordinatesAndForecast(t *testing.T) {
	c, f, st := setup(t)
	st.PutSettings(map[string]string{SettingKey: `{"city":"Екатеринбург"}`})

	days, err := c.Collect(context.Background(), at(27, 10))
	if err != nil || len(days) != 2 {
		t.Fatalf("%+v %v", days, err)
	}
	r := days[0].Rows[0]
	if days[0].Date != "2026-10-27" || r.Category != "unknown" || r.Minutes != 0 {
		t.Errorf("row: %+v", days[0])
	}
	for k, want := range map[string]float64{"temp_c": 3.2, "precip_mm": 0, "daylight_hours": 9.5, "sunrise_h": 7.85, "sunset_h": 17.35} {
		if got, _ := r.Extra[k].(float64); got != want {
			t.Errorf("%s = %v, want %v", k, r.Extra[k], want)
		}
	}
	if err := r.Validate(activity.Weather); err != nil {
		t.Errorf("the row would be refused: %v", err)
	}
	if !strings.Contains(f.lastQuery, "latitude=56.8519") || !strings.Contains(f.lastQuery, "Asia%2FYekaterinburg") {
		t.Errorf("query: %s", f.lastQuery)
	}
	// The coordinates are saved with the name: no second geocoding.
	kv, _ := st.Settings()
	if !strings.Contains(kv[SettingKey], "56.8519") {
		t.Errorf("setting: %s", kv[SettingKey])
	}
}

func TestCacheLastsThreeHours(t *testing.T) {
	c, f, st := setup(t)
	st.PutSettings(map[string]string{SettingKey: `{"city":"Екатеринбург"}`})
	c.Collect(context.Background(), at(27, 10))
	if days, _ := c.Collect(context.Background(), at(27, 12)); days != nil || f.forecasts != 1 {
		t.Errorf("fetched again within 3 hours: forecasts %d", f.forecasts)
	}
	if days, _ := c.Collect(context.Background(), at(27, 14)); days == nil || f.forecasts != 2 {
		t.Errorf("not refreshed after 3 hours: forecasts %d", f.forecasts)
	}
	if f.geocodes != 1 {
		t.Errorf("geocodes %d", f.geocodes)
	}
}

func TestChangingTheCityRefreshesAtOnce(t *testing.T) {
	c, f, st := setup(t)
	st.PutSettings(map[string]string{SettingKey: `{"city":"Екатеринбург"}`})
	c.Collect(context.Background(), at(27, 10))
	st.PutSettings(map[string]string{SettingKey: `{"city":"Москва"}`})
	if days, _ := c.Collect(context.Background(), at(27, 10)); days == nil || f.forecasts != 2 || f.geocodes != 2 {
		t.Errorf("forecasts %d geocodes %d", f.forecasts, f.geocodes)
	}
}

func TestErrors(t *testing.T) {
	c, f, st := setup(t)
	f.noCity = true
	st.PutSettings(map[string]string{SettingKey: `{"city":"Нигде"}`})
	if _, err := c.Collect(context.Background(), at(27, 10)); err == nil || !strings.Contains(err.Error(), "was not found") {
		t.Errorf("unknown city: %v", err)
	}
	st.PutSettings(map[string]string{SettingKey: `{"city":""}`})
	if _, err := c.Collect(context.Background(), at(27, 10)); err == nil {
		t.Error("an empty city")
	}
	down := New(st, yekat)
	down.GeocodeBase, down.ForecastBase = "http://127.0.0.1:1", "http://127.0.0.1:1"
	st.PutSettings(map[string]string{SettingKey: `{"city":"Екатеринбург","lat":56.8,"lon":60.6}`})
	if _, err := down.Collect(context.Background(), at(27, 10)); err == nil || strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("outage: %v", err)
	}
}

func TestParseLocation(t *testing.T) {
	if l, err := ParseLocation([]byte(`{"city":"  Пермь ","lat":58,"lon":56.2}`)); err != nil || l.City != "Пермь" {
		t.Errorf("%+v %v", l, err)
	}
	for _, bad := range []string{`{}`, `{"city":"x","lat":1}`, `{"city":"x","lat":91,"lon":0}`, `{"city":"x","lon":181,"lat":0}`,
		`{"city":"x","zip":"1"}`, `{"city":"<b>"}`, `[]`, `{"city":"` + strings.Repeat("я", 81) + `"}`} {
		if _, err := ParseLocation([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestRunnerStoresTheForecast(t *testing.T) {
	c, _, st := setup(t)
	st.PutSettings(map[string]string{SettingKey: `{"city":"Екатеринбург"}`})
	if err := activity.Run(context.Background(), c, at(27, 10), st); err != nil {
		t.Fatal(err)
	}
	rows, _ := st.Activity("2026-10-01", "2026-10-31", "weather")
	if len(rows) != 2 || !strings.Contains(rows[0].Extra, "daylight_hours") {
		t.Errorf("%+v", rows)
	}
}
