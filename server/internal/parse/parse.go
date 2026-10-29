// Package parse turns a phrase into a card: the model extracts the parts, the
// code validates them and calculates the dates.
package parse

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"lifeagent/internal/calendar"
	"lifeagent/internal/llm"
	"lifeagent/internal/load"
	"lifeagent/internal/timeparse"
)

const (
	MaxTextRunes = 500
	maxDuration  = 30 * 24 * 60 // a month, in minutes
	maxTitleRune = 200
)

// Extract is the model's answer (PLAN.md section 13).
type Extract struct {
	Kind          string          `json:"kind"`
	Title         string          `json:"title"`
	Date          *timeparse.Date `json:"date"`
	TimeFrom      *string         `json:"time_from"`
	TimeTo        *string         `json:"time_to"`
	DurationMin   *int            `json:"duration_min"`
	Deadline      *timeparse.Date `json:"deadline"`
	Recurrence    *string         `json:"recurrence"`
	CategoryGuess string          `json:"category_guess"`
	Guessed       []string        `json:"guessed"`
}

var knownGuessed = map[string]bool{"date": true, "time_from": true, "time_to": true, "duration_min": true, "deadline": true}

// defaultDeadlineDays is the deadline of a task whose phrase names no day.
const defaultDeadlineDays = 7

// Decode reads and validates the model's JSON. Every error here is the model's
// fault: it makes the client retry once with the error text, then change model.
func Decode(raw string, now time.Time, loc *time.Location) (Extract, error) {
	var e Extract
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&e); err != nil {
		return e, llm.Invalid(fmt.Errorf("JSON не соответствует схеме: %v", err))
	}
	bad := func(format string, a ...any) (Extract, error) {
		return e, llm.Invalid(fmt.Errorf(format, a...))
	}

	if e.Kind != "event" && e.Kind != "task" {
		return bad("kind должен быть event или task")
	}
	e.Title = strings.TrimSpace(e.Title)
	if e.Title == "" || utf8.RuneCountInString(e.Title) > maxTitleRune {
		return bad("title должен быть непустым и коротким")
	}
	if !load.Category(e.CategoryGuess).Valid() {
		return bad("category_guess должен быть одним из study, work, sport, hobby, rest, chores, other")
	}
	for _, g := range e.Guessed {
		if !knownGuessed[g] {
			return bad("guessed: неизвестное поле %q", g)
		}
	}

	var from *timeparse.Clock
	for name, s := range map[string]*string{"time_from": e.TimeFrom, "time_to": e.TimeTo} {
		if s == nil {
			continue
		}
		c, err := timeparse.ParseClock(*s)
		if err != nil {
			return bad("%s: %v", name, err)
		}
		if name == "time_from" {
			from = &c
		}
	}
	if e.DurationMin != nil && (*e.DurationMin < 1 || *e.DurationMin > maxDuration) {
		return bad("duration_min должен быть от 1 до %d минут", maxDuration)
	}
	for name, d := range map[string]*timeparse.Date{"date": e.Date, "deadline": e.Deadline} {
		if d == nil {
			continue
		}
		if _, err := timeparse.Resolve(*d, now, loc, from); err != nil {
			return bad("%s: %v", name, err)
		}
	}
	if e.Recurrence != nil {
		if *e.Recurrence == "" {
			e.Recurrence = nil
		} else if err := calendar.ValidateRRULE(*e.Recurrence, loc); err != nil {
			return bad("recurrence: %v", err)
		}
	}
	return e, nil
}

type Problem struct {
	Code    string `json:"code"` // end_not_after_start, in_past, deadline_unreachable
	Message string `json:"message"`
}

// Result is the card: the extracted parts with the dates calculated by code.
type Result struct {
	Kind          string    `json:"kind"`
	Title         string    `json:"title"`
	CategoryGuess string    `json:"category_guess"`
	Date          *string   `json:"date"`      // YYYY-MM-DD, event
	TimeFrom      *string   `json:"time_from"` // HH:MM
	TimeTo        *string   `json:"time_to"`
	Start         *string   `json:"start"` // RFC 3339, event with a date and a start time
	End           *string   `json:"end"`
	DurationMin   *int      `json:"duration_min"`
	Deadline      *string   `json:"deadline"` // YYYY-MM-DD, task
	Recurrence    *string   `json:"recurrence"`
	Missing       []string  `json:"missing"`
	Guessed       []string  `json:"guessed"` // filled by the model or by default, not named in the phrase
	Problems      []Problem `json:"problems"`
}

// Build calculates the dates and the questions for the user. The phrase names
// only the task and the day; the model fills the rest and lists it in `guessed`.
// What is still empty after that follows from the fields themselves.
func Build(e Extract, now time.Time, loc *time.Location) Result {
	now = now.In(loc)
	r := Result{
		Kind: e.Kind, Title: e.Title, CategoryGuess: e.CategoryGuess,
		TimeFrom: e.TimeFrom, TimeTo: e.TimeTo, DurationMin: e.DurationMin,
		Recurrence: e.Recurrence, Missing: []string{}, Guessed: []string{}, Problems: []Problem{},
	}
	for _, g := range e.Guessed {
		r.guess(g)
	}
	if e.Kind == "task" {
		buildTask(&r, e, now, loc)
	} else {
		buildEvent(&r, e, now, loc)
	}
	return r
}

func ymd(t time.Time) *string {
	s := t.Format("2006-01-02")
	return &s
}

func rfc(t time.Time) *string {
	s := t.Format(time.RFC3339)
	return &s
}

func (r *Result) guess(field string) {
	if !slices.Contains(r.Guessed, field) {
		r.Guessed = append(r.Guessed, field)
	}
}

func (r *Result) problem(code, msg string) {
	r.Problems = append(r.Problems, Problem{Code: code, Message: msg})
}

func buildEvent(r *Result, e Extract, now time.Time, loc *time.Location) {
	r.Deadline = nil
	var from, to *timeparse.Clock
	if e.TimeFrom != nil {
		c, _ := timeparse.ParseClock(*e.TimeFrom)
		from = &c
	}
	if e.TimeTo != nil {
		c, _ := timeparse.ParseClock(*e.TimeTo)
		to = &c
	}
	if e.Date == nil {
		e.Date = &timeparse.Date{Type: "relative", Days: new(0)}
		r.guess("date")
	}
	if from == nil {
		r.Missing = append(r.Missing, "time_from")
	}
	if to == nil && e.DurationMin == nil {
		r.Missing = append(r.Missing, "time_to")
	}
	day, err := timeparse.Resolve(*e.Date, now, loc, from)
	if err != nil {
		return // Decode has already validated it
	}
	r.Date = ymd(day)
	if from == nil {
		if day.Before(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)) {
			r.problem("in_past", "Эта дата уже прошла.")
		}
		return
	}
	start := timeparse.At(day, *from, loc)
	r.Start = rfc(start)
	switch {
	case to != nil:
		end := timeparse.At(day, *to, loc)
		if !end.After(start) {
			r.problem("end_not_after_start", "Конец события не позже начала.")
		} else {
			r.End = rfc(end)
		}
	case e.DurationMin != nil:
		r.End = rfc(start.Add(time.Duration(*e.DurationMin) * time.Minute))
	}
	if start.Before(now) {
		r.problem("in_past", "Это время уже прошло.")
	}
}

func buildTask(r *Result, e Extract, now time.Time, loc *time.Location) {
	r.Date, r.TimeFrom, r.TimeTo, r.Recurrence = nil, nil, nil, nil
	if e.DurationMin == nil {
		r.Missing = append(r.Missing, "duration_min")
	}
	dl := e.Deadline
	if dl == nil {
		dl = e.Date // «сделать завтра»: the day named is the deadline
	}
	var day time.Time
	if dl == nil {
		day = time.Date(now.Year(), now.Month(), now.Day()+defaultDeadlineDays, 0, 0, 0, 0, loc)
		r.guess("deadline")
	} else {
		var err error
		if day, err = timeparse.Resolve(*dl, now, loc, nil); err != nil {
			return
		}
	}
	r.Deadline = ymd(day)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	if day.Before(today) {
		r.problem("in_past", "Срок уже прошёл.")
		return
	}
	if e.DurationMin != nil {
		endOfDay := time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, loc)
		if endOfDay.Sub(now) < time.Duration(*e.DurationMin)*time.Minute {
			r.problem("deadline_unreachable", "До срока осталось меньше времени, чем нужно на задачу.")
		}
	}
}

// Caller is the part of llm.Client the parser needs.
type Caller interface {
	Call(ctx context.Context, in llm.Input) (llm.Output, error)
}

type Parser struct {
	LLM Caller
	Loc *time.Location
	Now func() time.Time
}

// Parsed is a card together with how it was obtained.
type Parsed struct {
	Result
	Model string `json:"model"`
	Calls int    `json:"calls"`
}

var ErrBadText = errors.New("parse: text must be 1..500 characters")

func (p *Parser) Parse(ctx context.Context, text string, src llm.Source) (Parsed, error) {
	text = strings.TrimSpace(text)
	if n := utf8.RuneCountInString(text); n < 1 || n > MaxTextRunes {
		return Parsed{}, ErrBadText
	}
	now := p.Now().In(p.Loc)
	var ex Extract
	out, err := p.LLM.Call(ctx, llm.Input{
		Task: llm.TaskParse, Source: src,
		System: SystemPrompt(now), User: UserPrompt(text),
		SchemaName: SchemaName, Schema: Schema,
		Check: func(raw string) error {
			var err error
			ex, err = Decode(raw, now, p.Loc)
			return err
		},
	})
	if err != nil {
		return Parsed{}, err
	}
	return Parsed{Result: Build(ex, now, p.Loc), Model: out.Model, Calls: out.Calls}, nil
}
