package timeparse

import (
	"testing"
	"time"
)

func loc(t *testing.T, name string) *time.Location {
	t.Helper()
	l, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func ptr[T any](v T) *T { return &v }

func TestResolve(t *testing.T) {
	ekb := loc(t, "Asia/Yekaterinburg")
	at := func(y int, m time.Month, d, h, mi int) time.Time { return time.Date(y, m, d, h, mi, 0, 0, ekb) }
	// Wednesday 2026-10-28.
	wed := at(2026, 10, 28, 12, 0)

	tests := []struct {
		name  string
		d     Date
		now   time.Time
		clock *Clock
		want  time.Time
	}{
		{"today", Date{Type: "relative", Days: ptr(0)}, wed, nil, at(2026, 10, 28, 0, 0)},
		{"tomorrow", Date{Type: "relative", Days: ptr(1)}, wed, nil, at(2026, 10, 29, 0, 0)},
		{"day after tomorrow", Date{Type: "relative", Days: ptr(2)}, wed, nil, at(2026, 10, 30, 0, 0)},
		{"tomorrow across month end", Date{Type: "relative", Days: ptr(1)}, at(2026, 10, 31, 23, 0), nil, at(2026, 11, 1, 0, 0)},
		{"tomorrow across year end", Date{Type: "relative", Days: ptr(1)}, at(2026, 12, 31, 20, 0), nil, at(2027, 1, 1, 0, 0)},

		{"friday, week null, from Wednesday", Date{Type: "weekday", Value: "fri"}, wed, nil, at(2026, 10, 30, 0, 0)},
		{"monday, week null, from Wednesday means next Monday", Date{Type: "weekday", Value: "mon"}, wed, nil, at(2026, 11, 2, 0, 0)},
		{"wednesday on Wednesday before the time", Date{Type: "weekday", Value: "wed"}, wed, &Clock{18, 0}, at(2026, 10, 28, 0, 0)},
		{"wednesday on Wednesday after the time", Date{Type: "weekday", Value: "wed"}, wed, &Clock{9, 0}, at(2026, 11, 4, 0, 0)},
		{"wednesday on Wednesday at the exact time has passed", Date{Type: "weekday", Value: "wed"}, wed, &Clock{12, 0}, at(2026, 11, 4, 0, 0)},
		{"wednesday on Wednesday without a time stays today", Date{Type: "weekday", Value: "wed"}, wed, nil, at(2026, 10, 28, 0, 0)},
		{"sunday, week null", Date{Type: "weekday", Value: "sun"}, wed, nil, at(2026, 11, 1, 0, 0)},
		{"this friday", Date{Type: "weekday", Value: "fri", Week: ptr("this")}, wed, nil, at(2026, 10, 30, 0, 0)},
		{"this monday is in the past", Date{Type: "weekday", Value: "mon", Week: ptr("this")}, wed, nil, at(2026, 10, 26, 0, 0)},
		{"this sunday", Date{Type: "weekday", Value: "sun", Week: ptr("this")}, wed, nil, at(2026, 11, 1, 0, 0)},
		{"next monday", Date{Type: "weekday", Value: "mon", Week: ptr("next")}, wed, nil, at(2026, 11, 2, 0, 0)},
		{"next friday", Date{Type: "weekday", Value: "fri", Week: ptr("next")}, wed, nil, at(2026, 11, 6, 0, 0)},
		{"next sunday from a Sunday", Date{Type: "weekday", Value: "sun", Week: ptr("next")}, at(2026, 11, 1, 10, 0), nil, at(2026, 11, 8, 0, 0)},
		{"this monday from a Sunday is six days back", Date{Type: "weekday", Value: "mon", Week: ptr("this")}, at(2026, 11, 1, 10, 0), nil, at(2026, 10, 26, 0, 0)},
		{"weekday over year end", Date{Type: "weekday", Value: "fri"}, at(2026, 12, 30, 10, 0), nil, at(2027, 1, 1, 0, 0)},

		{"absolute", Date{Type: "absolute", Value: "2026-11-03"}, wed, nil, at(2026, 11, 3, 0, 0)},
		{"absolute leap day", Date{Type: "absolute", Value: "2028-02-29"}, wed, nil, at(2028, 2, 29, 0, 0)},
		{"absolute in the past is still resolved", Date{Type: "absolute", Value: "2026-01-01"}, wed, nil, at(2026, 1, 1, 0, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(tt.d, tt.now, ekb, tt.clock)
			if err != nil {
				t.Fatal(err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveUsesTheUserZoneNotUTC(t *testing.T) {
	ekb := loc(t, "Asia/Yekaterinburg")
	// 22:00 UTC on Wednesday is already 03:00 Thursday in Yekaterinburg.
	now := time.Date(2026, 10, 28, 22, 0, 0, 0, time.UTC)
	got, err := Resolve(Date{Type: "relative", Days: ptr(0)}, now, ekb, nil)
	if err != nil || got.Day() != 29 {
		t.Fatalf("today = %v, err %v", got, err)
	}
}

func TestResolveDST(t *testing.T) {
	berlin := loc(t, "Europe/Berlin")
	// Saturday 2026-03-28; summer time starts on Sunday the 29th, so "tomorrow" is a 23-hour day.
	now := time.Date(2026, 3, 28, 12, 0, 0, 0, berlin)
	got, err := Resolve(Date{Type: "relative", Days: ptr(1)}, now, berlin, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Day() != 29 || got.Hour() != 0 {
		t.Fatalf("got %v", got)
	}
	// The day after is Monday the 30th at local midnight, not 01:00.
	got, _ = Resolve(Date{Type: "relative", Days: ptr(2)}, now, berlin, nil)
	if got.Day() != 30 || got.Hour() != 0 {
		t.Fatalf("got %v", got)
	}
	// Evening of the DST day keeps its wall clock.
	d, _ := Resolve(Date{Type: "relative", Days: ptr(1)}, now, berlin, nil)
	if e := At(d, Clock{20, 0}, berlin); e.Hour() != 20 || e.Day() != 29 {
		t.Fatalf("evening %v", e)
	}
}

func TestResolveErrors(t *testing.T) {
	ekb := loc(t, "Asia/Yekaterinburg")
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, ekb)
	bad := []Date{
		{},
		{Type: "tomorrow"},
		{Type: "relative"},
		{Type: "relative", Days: ptr(3)},
		{Type: "relative", Days: ptr(-1)},
		{Type: "weekday", Value: "friday"},
		{Type: "weekday", Value: ""},
		{Type: "weekday", Value: "fri", Week: ptr("last")},
		{Type: "absolute", Value: "2026-02-30"},
		{Type: "absolute", Value: "03.11.2026"},
		{Type: "absolute", Value: "2026-11-3"},
		{Type: "absolute", Value: ""},
	}
	for _, d := range bad {
		if got, err := Resolve(d, now, ekb, nil); err == nil {
			t.Errorf("%+v accepted as %v", d, got)
		}
	}
}

func TestParseClock(t *testing.T) {
	good := map[string]Clock{"18:00": {18, 0}, "0:00": {0, 0}, "00:00": {0, 0}, "9:05": {9, 5}, "23:59": {23, 59}}
	for s, want := range good {
		if got, err := ParseClock(s); err != nil || got != want {
			t.Errorf("%q = %v, %v", s, got, err)
		}
	}
	for _, s := range []string{"", "24:00", "18", "18:60", "6pm", "18:00:00", "-1:00", "ab:cd", "18:0", "1800", " 18:00"} {
		if got, err := ParseClock(s); err == nil {
			t.Errorf("%q accepted as %v", s, got)
		}
	}
	if (Clock{9, 5}).String() != "09:05" {
		t.Error("String")
	}
}
