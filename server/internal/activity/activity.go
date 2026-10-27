// Package activity holds what the agent knows about the user's day: daily
// sums per category and source. Only categories, minutes and counts are
// accepted here; titles of windows, URLs and names of pages have no place in
// any type of this package (PLAN.md section 17, "Модель угроз").
package activity

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"time"
)

// Categories of activity (PLAN.md stage 10).
const (
	Study   = "study"
	Career  = "career"
	Sport   = "sport"
	Work    = "work"
	Leisure = "leisure"
	Social  = "social"
	Sleep   = "sleep"
	Unknown = "unknown"
)

var Categories = []string{Study, Career, Sport, Work, Leisure, Social, Sleep, Unknown}

// Sources of daily sums.
const (
	AndroidUsage     = "android_usage"
	MacActivityWatch = "mac_activitywatch"
	Steam            = "steam"
	GitHub           = "github"
	LeetCode         = "leetcode"
	Weather          = "weather"
)

var Sources = []string{AndroidUsage, MacActivityWatch, Steam, GitHub, LeetCode, Weather}

func ValidCategory(c string) bool { return contains(Categories, c) }
func ValidSource(s string) bool   { return contains(Sources, s) }

// ClientSource: the phone and the Mac send their own sums; the other sources
// are read by the server itself.
func ClientSource(s string) bool { return s == AndroidUsage || s == MacActivityWatch }

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Row is the sum of one category on one day.
type Row struct {
	Category string
	Minutes  int
	// NightMinutes: part of Minutes inside the sleep window (PLAN.md section 14).
	NightMinutes int
	Count        int
	Confidence   float64 // 0..1; 0 is treated as unset by the callers that build rows
	// Extra holds numbers (and a short genre list for Steam) from a fixed set of keys.
	Extra map[string]any
}

// DayRows are the rows of one date (YYYY-MM-DD, the user's zone).
type DayRows struct {
	Date string
	Rows []Row
}

// Collector reads one source on the server. It returns the days it knows about
// at the moment "now"; each returned day replaces the stored day.
type Collector interface {
	Source() string
	Collect(ctx context.Context, now time.Time) ([]DayRows, error)
}

// Validate checks a row against the rules every source shares.
func (r Row) Validate(source string) error {
	switch {
	case !ValidCategory(r.Category):
		return fmt.Errorf("category must be one of %v", Categories)
	case r.Minutes < 0 || r.Minutes > 24*60:
		return errors.New("minutes must be 0..1440")
	case r.NightMinutes < 0 || r.NightMinutes > r.Minutes:
		return errors.New("night_minutes must be 0..minutes")
	case r.Count < 0 || r.Count > 1_000_000:
		return errors.New("count must be 0..1000000")
	case r.Confidence < 0 || r.Confidence > 1:
		return errors.New("confidence must be 0..1")
	}
	return ValidateExtra(r.Extra)
}

type extraKind int

const (
	numberKey extraKind = iota + 1
	wordsKey
)

// extraKeys is the whole vocabulary of the free-form field. A key outside it
// is refused, so nothing like a title can travel in there.
var extraKeys = map[string]extraKind{
	"temp_c": numberKey, "precip_mm": numberKey, "daylight_hours": numberKey,
	"commits": numberKey, "prs": numberKey, "reviews": numberKey, "solved": numberKey,
	"hours_total": numberKey, "games": numberKey,
	"genres": wordsKey,
}

var genreRe = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} &'-]{0,38}$`)

// ValidateExtra accepts only the known keys with numbers, or up to five short
// genre names made of letters, digits, spaces and a few signs.
func ValidateExtra(extra map[string]any) error {
	for k, v := range extra {
		kind, ok := extraKeys[k]
		if !ok {
			return fmt.Errorf("extra: unknown key %q", k)
		}
		switch kind {
		case numberKey:
			f, ok := v.(float64)
			if !ok || f != f || f > 1e9 || f < -1e9 {
				return fmt.Errorf("extra.%s must be a number", k)
			}
		case wordsKey:
			list, ok := v.([]any)
			if !ok || len(list) > 5 {
				return fmt.Errorf("extra.%s must be a list of up to 5 names", k)
			}
			for _, item := range list {
				s, ok := item.(string)
				if !ok || !genreRe.MatchString(s) {
					return fmt.Errorf("extra.%s: bad name", k)
				}
			}
		}
	}
	return nil
}

// ExtraKeys lists the allowed keys, for documentation and tests.
func ExtraKeys() []string {
	out := make([]string, 0, len(extraKeys))
	for k := range extraKeys {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
