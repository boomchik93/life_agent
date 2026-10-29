package macagent

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"
)

// Threshold is the confidence under which a category is "unknown".
const Threshold = 0.7

// NameCategory is the server's answer for one name.
type NameCategory struct {
	Category   string
	Confidence float64
	Resolved   bool
}

// Categorizer asks the server about names (dictionary, cache, model).
type Categorizer func(ctx context.Context, names []string, llm bool) (map[string]NameCategory, error)

// ContextEvent is a calendar event with an activity category.
type ContextEvent struct {
	Span     Span
	Category string
}

// ContextCategory maps a calendar category to an activity one; chores and
// "other" say nothing about what was on the screen.
func ContextCategory(c string) string {
	switch c {
	case "study":
		return "study"
	case "work":
		return "work"
	case "sport":
		return "sport"
	case "hobby", "rest":
		return "leisure"
	}
	return ""
}

// Row is the minutes of one category on one day, as they are sent.
type Row struct {
	Category     string  `json:"category"`
	Minutes      int     `json:"minutes"`
	NightMinutes int     `json:"night_minutes"`
	Confidence   float64 `json:"confidence"`
}

type acc struct{ minutes, night, weight float64 }

// Summarize turns the segments of one day into minutes per category. Layers,
// in order: the dictionary and the cache of the server, the calendar context,
// then the model for what is still unplaced. The names go to the server only
// through categorize; what comes out holds categories and numbers.
func Summarize(ctx context.Context, day time.Time, segs []Seg, events []ContextEvent, categorize Categorizer, sleepStart, sleepEnd int) ([]Row, error) {
	loc := day.Location()
	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
	dayEnd := time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, loc)
	window := Span{dayStart, dayEnd}
	nightA := Span{dayStart, time.Date(day.Year(), day.Month(), day.Day(), sleepEnd, 0, 0, 0, loc)}
	nightB := Span{time.Date(day.Year(), day.Month(), day.Day(), sleepStart, 0, 0, 0, loc), dayEnd}

	// Browser windows with no domain: unknown, and nobody is asked.
	var named []Seg
	totals := map[string]*acc{}
	add := func(cat string, s Span, conf float64) {
		a := totals[cat]
		if a == nil {
			a = &acc{}
			totals[cat] = a
		}
		m := s.Minutes()
		a.minutes += m
		a.weight += m * conf
		for _, n := range []Span{nightA, nightB} {
			for _, p := range intersect([]Span{s}, []Span{n}, window) {
				a.night += p.Minutes()
			}
		}
	}
	for _, sg := range segs {
		for _, p := range intersect([]Span{sg.Span}, []Span{window}, window) {
			if sg.Browser {
				add("unknown", p, 0)
				continue
			}
			named = append(named, Seg{Key: sg.Key, Span: p})
		}
	}

	keys := distinct(named)
	known := map[string]NameCategory{}
	settled := map[string]bool{}
	if len(keys) > 0 {
		got, err := categorize(ctx, keys, false)
		if err != nil {
			return nil, err
		}
		for _, k := range keys {
			g, ok := got[k]
			switch {
			case !ok || !g.Resolved:
			case g.Category != "unknown" && g.Confidence >= Threshold:
				known[k] = g
			default:
				settled[k] = true
			}
		}
	}

	left := map[string][]Span{}
	for _, sg := range named {
		if g, ok := known[sg.Key]; ok {
			add(g.Category, sg.Span, g.Confidence)
			continue
		}
		cursor := sg.Span.Start
		var cuts []ContextEvent
		for _, e := range events {
			if e.Span.End.After(sg.Span.Start) && e.Span.Start.Before(sg.Span.End) {
				cuts = append(cuts, e)
			}
		}
		sort.Slice(cuts, func(i, j int) bool { return cuts[i].Span.Start.Before(cuts[j].Span.Start) })
		for _, e := range cuts {
			s, t := maxT(e.Span.Start, cursor), minT(e.Span.End, sg.Span.End)
			if !t.After(s) {
				continue
			}
			if s.After(cursor) {
				left[sg.Key] = append(left[sg.Key], Span{cursor, s})
			}
			add(e.Category, Span{s, t}, 0.9)
			cursor = t
		}
		if sg.Span.End.After(cursor) {
			left[sg.Key] = append(left[sg.Key], Span{cursor, sg.Span.End})
		}
	}

	var ask []string
	for k := range left {
		if !settled[k] {
			ask = append(ask, k)
		}
	}
	sort.Strings(ask)
	guess := map[string]NameCategory{}
	if len(ask) > 0 {
		got, err := categorize(ctx, ask, true)
		if err != nil {
			return nil, err
		}
		for _, k := range ask {
			if g, ok := got[k]; ok && g.Resolved && g.Category != "unknown" && g.Confidence >= Threshold {
				guess[k] = g
			}
		}
	}
	for k, spans := range left {
		g, ok := guess[k]
		for _, s := range spans {
			if ok {
				add(g.Category, s, g.Confidence)
			} else {
				add("unknown", s, 0)
			}
		}
	}

	var rows []Row
	for cat, a := range totals {
		m := int(math.Round(a.minutes))
		if m <= 0 {
			continue
		}
		night := int(math.Round(a.night))
		if night > m {
			night = m
		}
		conf := math.Round(a.weight/a.minutes*100) / 100
		rows = append(rows, Row{Category: cat, Minutes: m, NightMinutes: night, Confidence: math.Min(math.Max(conf, 0), 1)})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Category < rows[j].Category })
	return rows, nil
}

func distinct(segs []Seg) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range segs {
		if !seen[s.Key] {
			seen[s.Key] = true
			out = append(out, s.Key)
		}
	}
	sort.Strings(out)
	return out
}

// Date formats a day for the server.
func Date(t time.Time) string {
	return fmt.Sprintf("%04d-%02d-%02d", t.Year(), int(t.Month()), t.Day())
}
