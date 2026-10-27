// Package load turns calendar events and check-ins into the four burnout
// risks of PLAN.md section 6. It is pure code: no I/O, no clock, no LLM.
package load

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

type Color string

const (
	Green  Color = "green"
	Yellow Color = "yellow"
	Red    Color = "red"
	Gray   Color = "gray" // no data: the colour is never guessed
)

type Category string

const (
	Study  Category = "study"
	Work   Category = "work"
	Hobby  Category = "hobby"
	Rest   Category = "rest"
	Chores Category = "chores"
	Other  Category = "other"
)

func (c Category) Valid() bool {
	switch c {
	case Study, Work, Hobby, Rest, Chores, Other:
		return true
	}
	return false
}

// Event is a calendar event already expanded into a single instance.
type Event struct {
	Start, End time.Time
	AllDay     bool // all-day events never count towards hours
	Category   Category
}

type Checkin struct {
	Date       string // YYYY-MM-DD, user's time zone
	Energy     int    // 1..5
	Mood       int    // 1..5
	SleepHours *float64
}

type Input struct {
	Events   []Event
	From, To time.Time // the week: [From, To)
	Loc      *time.Location
	Now      time.Time
	Checkins []Checkin
	// RestTracked reports that some calendar is mapped to hobby or rest. Without
	// it a week with zero rest hours says nothing: rest may simply not be in the calendar.
	RestTracked bool
	T           Thresholds
}

type Risk struct {
	Color   Color              `json:"color"`
	Reasons []string           `json:"reasons"`
	Metrics map[string]float64 `json:"metrics"`
}

type Assessment struct {
	Sleep    Risk `json:"sleep"`
	Overload Risk `json:"overload"`
	NoRest   Risk `json:"no_rest"`
	State    Risk `json:"state"`
}

// Assess evaluates all four risks. Sleep and state look at the last
// T.StateDays check-in days ending at Now, not at the week.
func Assess(in Input) Assessment {
	return Assessment{
		Sleep:    assessSleep(in),
		Overload: assessOverload(in),
		NoRest:   assessNoRest(in),
		State:    assessState(in),
	}
}

func newRisk() Risk {
	return Risk{Color: Gray, Reasons: []string{}, Metrics: map[string]float64{}}
}

// Worse returns the more severe colour; gray never wins over real data.
func Worse(a, b Color) Color {
	rank := map[Color]int{Gray: 0, Green: 1, Yellow: 2, Red: 3}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

func (r *Risk) add(c Color, reason string) {
	r.Color = Worse(r.Color, c)
	if c == Yellow || c == Red {
		r.Reasons = append(r.Reasons, reason)
	}
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func hours(v float64) string {
	return strings.Replace(fmt.Sprintf("%.1f", v), ".", ",", 1) + " ч"
}

var weekdays = [...]string{"вс", "пн", "вт", "ср", "чт", "пт", "сб"}

func dayLabel(t time.Time) string {
	return fmt.Sprintf("%s %02d.%02d", weekdays[t.Weekday()], t.Day(), int(t.Month()))
}

// above grades a value where larger is worse.
func above(v, yellow, red float64) Color {
	switch {
	case v > red:
		return Red
	case v > yellow:
		return Yellow
	}
	return Green
}

// below grades a value where smaller is worse.
func below(v, yellow, red float64) Color {
	switch {
	case v < red:
		return Red
	case v < yellow:
		return Yellow
	}
	return Green
}

type span struct{ s, e time.Time }

func (p span) dur() time.Duration { return p.e.Sub(p.s) }

// spans returns the timed events of the given categories (all if none given)
// clipped to [from, to), with overlaps merged so nothing is counted twice.
func spans(events []Event, from, to time.Time, cats ...Category) []span {
	var out []span
	for _, e := range events {
		if e.AllDay || !e.End.After(e.Start) {
			continue
		}
		if len(cats) > 0 && !hasCat(cats, e.Category) {
			continue
		}
		s, en := e.Start, e.End
		if s.Before(from) {
			s = from
		}
		if en.After(to) {
			en = to
		}
		if en.After(s) {
			out = append(out, span{s, en})
		}
	}
	return merge(out, 0)
}

func hasCat(cats []Category, c Category) bool {
	for _, x := range cats {
		if x == c {
			return true
		}
	}
	return false
}

// merge joins spans that overlap or are separated by less than gap.
func merge(in []span, gap time.Duration) []span {
	if len(in) == 0 {
		return nil
	}
	sort.Slice(in, func(i, j int) bool { return in[i].s.Before(in[j].s) })
	out := []span{in[0]}
	for _, p := range in[1:] {
		last := &out[len(out)-1]
		if p.s.Sub(last.e) < gap || !p.s.After(last.e) {
			if p.e.After(last.e) {
				last.e = p.e
			}
			continue
		}
		out = append(out, p)
	}
	return out
}

func total(sp []span) time.Duration {
	var d time.Duration
	for _, p := range sp {
		d += p.dur()
	}
	return d
}

// days lists the local midnights of [from, to). Calendar arithmetic goes
// through time.Date, so DST days are 23 or 25 hours long without special cases.
func days(from, to time.Time, loc *time.Location) []time.Time {
	var out []time.Time
	for d := from.In(loc); d.Before(to); d = time.Date(d.Year(), d.Month(), d.Day()+1, 0, 0, 0, 0, loc) {
		out = append(out, d)
	}
	return out
}

func nextDay(d time.Time, loc *time.Location) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day()+1, 0, 0, 0, 0, loc)
}

func hasTimed(events []Event, from, to time.Time) bool {
	for _, e := range events {
		if !e.AllDay && touches(e.Start, e.End, from, to) {
			return true
		}
	}
	return false
}

// touches reports whether [start, end) meets [from, to); an event without
// duration is an instant at start.
func touches(start, end, from, to time.Time) bool {
	if !end.After(start) {
		return !start.Before(from) && start.Before(to)
	}
	return end.After(from) && start.Before(to)
}

func assessOverload(in Input) Risk {
	r := newRisk()
	if !hasTimed(in.Events, in.From, in.To) {
		r.Reasons = append(r.Reasons, "в календаре на неделе нет событий: оценить нечем")
		return r
	}
	study := spans(in.Events, in.From, in.To, Study)
	t := in.T

	week := total(study).Hours()
	r.Metrics["week_hours"] = round2(week)
	r.add(above(week, t.StudyWeekYellowAbove, t.StudyWeekRedAbove),
		fmt.Sprintf("учёба за неделю %s (порог: жёлтый выше %s, красный выше %s)",
			hours(week), hours(t.StudyWeekYellowAbove), hours(t.StudyWeekRedAbove)))

	var maxDay float64
	var maxDayAt time.Time
	for _, d := range days(in.From, in.To, in.Loc) {
		h := total(within(study, d, nextDay(d, in.Loc))).Hours()
		if h > maxDay {
			maxDay, maxDayAt = h, d
		}
	}
	r.Metrics["max_day_hours"] = round2(maxDay)
	r.add(above(maxDay, t.StudyDayYellowAbove, t.StudyDayRedAbove),
		fmt.Sprintf("учёба %s в день: %s (порог: жёлтый выше %s, красный выше %s)",
			dayLabel(maxDayAt), hours(maxDay), hours(t.StudyDayYellowAbove), hours(t.StudyDayRedAbove)))

	var streak time.Duration
	for _, p := range merge(append([]span(nil), study...), time.Duration(t.BreakMin)*time.Minute) {
		streak = max(streak, p.dur())
	}
	r.Metrics["max_streak_hours"] = round2(streak.Hours())
	r.add(above(streak.Hours(), t.StudyStreakYellowAbove, t.StudyStreakRedAbove),
		fmt.Sprintf("учёба подряд без перерыва %s (порог: жёлтый выше %s, красный выше %s)",
			hours(streak.Hours()), hours(t.StudyStreakYellowAbove), hours(t.StudyStreakRedAbove)))
	return r
}

// within clips already merged spans to [from, to).
func within(sp []span, from, to time.Time) []span {
	var out []span
	for _, p := range sp {
		if p.s.Before(from) {
			p.s = from
		}
		if p.e.After(to) {
			p.e = to
		}
		if p.e.After(p.s) {
			out = append(out, p)
		}
	}
	return out
}

func assessNoRest(in Input) Risk {
	r := newRisk()
	if !hasTimed(in.Events, in.From, in.To) {
		r.Reasons = append(r.Reasons, "в календаре на неделе нет событий: оценить нечем")
		return r
	}
	t := in.T

	rest := spans(in.Events, in.From, in.To, Hobby, Rest)
	if in.RestTracked || len(rest) > 0 {
		h := total(rest).Hours()
		r.Metrics["rest_hours"] = round2(h)
		r.add(below(h, t.RestWeekYellowBelow, t.RestWeekRedBelow),
			fmt.Sprintf("отдых и развлечения за неделю %s (порог: жёлтый ниже %s, красный ниже %s)",
				hours(h), hours(t.RestWeekYellowBelow), hours(t.RestWeekRedBelow)))
	} else {
		r.Reasons = append(r.Reasons, "часы отдыха неизвестны: ни один календарь не отнесён к категориям hobby и rest")
	}

	free := 0
	for _, d := range days(in.From, in.To, in.Loc) {
		evening := time.Date(d.Year(), d.Month(), d.Day(), t.EveningStartHour, 0, 0, 0, in.Loc)
		if !hasTimed(in.Events, evening, nextDay(d, in.Loc)) {
			free++
		}
	}
	r.Metrics["free_evenings"] = float64(free)
	c := Green
	if free < t.FreeEveningsMin {
		c = Yellow
	}
	r.add(c, fmt.Sprintf("свободных вечеров (после %d:00) %d, нужно хотя бы %d", t.EveningStartHour, free, t.FreeEveningsMin))
	return r
}

// recent returns the check-ins of the StateDays days ending at Now.
func recent(in Input) []Checkin {
	now := in.Now.In(in.Loc)
	last := now.Format("2006-01-02")
	first := time.Date(now.Year(), now.Month(), now.Day()-(in.T.StateDays-1), 0, 0, 0, 0, in.Loc).Format("2006-01-02")
	var out []Checkin
	for _, c := range in.Checkins {
		if c.Date >= first && c.Date <= last {
			out = append(out, c)
		}
	}
	return out
}

func assessSleep(in Input) Risk {
	r := newRisk()
	var sum float64
	n := 0
	for _, c := range recent(in) {
		if c.SleepHours != nil {
			sum += *c.SleepHours
			n++
		}
	}
	if n == 0 {
		r.Reasons = append(r.Reasons, "нет данных о сне: часы сна не указаны в чек-ине")
		return r
	}
	avg := round2(sum / float64(n))
	r.Metrics["avg_sleep_hours"] = avg
	r.add(below(avg, in.T.SleepYellowBelow, in.T.SleepRedBelow),
		fmt.Sprintf("в среднем %s сна за %d дн. (порог: жёлтый ниже %s, красный ниже %s)",
			hours(avg), in.T.StateDays, hours(in.T.SleepYellowBelow), hours(in.T.SleepRedBelow)))
	return r
}

func assessState(in Input) Risk {
	r := newRisk()
	cs := recent(in)
	if len(cs) == 0 {
		r.Reasons = append(r.Reasons, "нет данных о состоянии: нет чек-инов за последние дни")
		return r
	}
	var sum float64
	for _, c := range cs {
		sum += float64(c.Energy + c.Mood)
	}
	avg := round2(sum / float64(2*len(cs)))
	r.Metrics["avg_score"] = avg
	c := Green
	switch {
	case avg <= in.T.StateRedAtMost:
		c = Red
	case avg <= in.T.StateYellowAtMost:
		c = Yellow
	}
	r.add(c, fmt.Sprintf("среднее энергии и настроения %s за %d дн. (порог: жёлтый %s и ниже, красный %s и ниже)",
		strings.Replace(fmt.Sprint(avg), ".", ",", 1), in.T.StateDays,
		strings.Replace(fmt.Sprint(in.T.StateYellowAtMost), ".", ",", 1),
		strings.Replace(fmt.Sprint(in.T.StateRedAtMost), ".", ",", 1)))
	return r
}
