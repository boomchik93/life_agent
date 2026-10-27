package load

import (
	"encoding/json"
	"fmt"
)

// CalendarCategories maps a calendar name to the category of all its events.
// Stored in settings under "calendar_categories". It is the only category
// source until the category memory arrives (stage 6).
type CalendarCategories map[string]Category

func ParseCalendarCategories(raw []byte) (CalendarCategories, error) {
	var m CalendarCategories
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	for cal, c := range m {
		if !c.Valid() {
			return nil, fmt.Errorf("calendar %q: unknown category %q", cal, c)
		}
	}
	return m, nil
}

// Of returns the category of an event. The ICS class schedule is study unless
// its calendar is mapped explicitly; unmapped Google events are other.
func (m CalendarCategories) Of(source, calendar string) Category {
	if c, ok := m[calendar]; ok {
		return c
	}
	if source == "ics" {
		return Study
	}
	return Other
}

// RestTracked reports whether some calendar is mapped to hobby or rest.
func (m CalendarCategories) RestTracked() bool {
	for _, c := range m {
		if c == Hobby || c == Rest {
			return true
		}
	}
	return false
}
