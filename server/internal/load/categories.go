package load

import (
	"encoding/json"
	"fmt"

	"lifeagent/internal/category"
)

// CalendarCategories maps a calendar name to the category of all its events.
// Stored in settings under "calendar_categories".
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

// RestTracked reports whether some calendar is mapped to sport, hobby or rest.
func (m CalendarCategories) RestTracked() bool {
	for _, c := range m {
		if c == Sport || c == Hobby || c == Rest {
			return true
		}
	}
	return false
}

// EventCategories finds the category of a calendar event the agent did not
// write. Layers, the first answer wins: what the user confirmed for this
// title before (Memory, by category.Normalize), the calendar the user mapped,
// the words of the title, and last the default of the source.
type EventCategories struct {
	Calendars CalendarCategories
	Memory    map[string]Category
}

func (c EventCategories) Of(source, calendar, title string) Category {
	if key := category.Normalize(title); key != "" {
		if m, ok := c.Memory[key]; ok && m.Valid() {
			return m
		}
	}
	if m, ok := c.Calendars[calendar]; ok {
		return m
	}
	if g, ok := category.Guess(title); ok {
		return Category(g)
	}
	return c.Calendars.Of(source, calendar)
}

func (c EventCategories) RestTracked() bool { return c.Calendars.RestTracked() }
