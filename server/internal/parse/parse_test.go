package parse

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"lifeagent/internal/llm"
	"lifeagent/internal/timeparse"
)

func TestPromptHasTheMomentAndNoCalculation(t *testing.T) {
	p := SystemPrompt(now)
	for _, want := range []string{"среда, 28 октября 2026 года, 12:00", "Asia/Yekaterinburg", "не считаешь", "<<<"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	if strings.Contains(p, "%!") {
		t.Errorf("format error in prompt: %s", p)
	}
	if u := UserPrompt("  КВН завтра  "); u != "Фраза пользователя:\n<<<\nКВН завтра\n>>>" {
		t.Errorf("user prompt %q", u)
	}
}

func TestDecodeAcceptsThePlanExamples(t *testing.T) {
	event := `{"kind":"event","title":"КВН","date":{"type":"weekday","value":"fri","week":null},"time_from":"18:00","time_to":"20:00",
	 "duration_min":null,"deadline":null,"recurrence":null,"category_guess":"hobby","missing":[]}`
	task := `{"kind":"task","title":"Курсовая","date":null,"time_from":null,"time_to":null,"duration_min":360,
	 "deadline":{"type":"weekday","value":"fri","week":null},"recurrence":null,"category_guess":"study","missing":[]}`
	flat := `{"kind":"task","title":"Курсовая","date":null,"time_from":null,"time_to":null,"duration_min":360,
	 "deadline":{"type":"weekday","days":null,"value":"fri","week":null},"recurrence":null,"category_guess":"study","missing":[]}`
	weekly := `{"kind":"event","title":"КВН","date":{"type":"weekday","value":"fri","week":null},"time_from":"18:00","time_to":"20:00",
	 "duration_min":null,"deadline":null,"recurrence":"FREQ=WEEKLY;BYDAY=FR","category_guess":"hobby","missing":[]}`
	for name, raw := range map[string]string{"event": event, "task": task, "strict-schema form with nulls": flat, "weekly": weekly} {
		if _, err := Decode(raw, now, ekb); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestDecodeRejects(t *testing.T) {
	const base = `{"kind":"event","title":"КВН","date":{"type":"weekday","value":"fri"},"time_from":"18:00","time_to":"20:00",
	 "duration_min":null,"deadline":null,"recurrence":null,"category_guess":"hobby","missing":[]}`
	rep := func(old, new string) string {
		if !strings.Contains(base, old) {
			panic("test bug: " + old)
		}
		return strings.Replace(base, old, new, 1)
	}
	tests := []struct{ name, raw string }{
		{"not an object", `[]`},
		{"unknown field", rep(`"missing":[]`, `"missing":[],"note":"x"`)},
		{"wrong type", rep(`"title":"КВН"`, `"title":5`)},
		{"unknown kind", rep(`"kind":"event"`, `"kind":"reminder"`)},
		{"empty title", rep(`"title":"КВН"`, `"title":"  "`)},
		{"huge title", rep(`"title":"КВН"`, `"title":"`+strings.Repeat("я", 201)+`"`)},
		{"unknown category", rep(`"hobby"`, `"fun"`)},
		{"unknown missing", rep(`"missing":[]`, `"missing":["color"]`)},
		{"bad time", rep(`"18:00"`, `"6pm"`)},
		{"24:00", rep(`"20:00"`, `"24:00"`)},
		{"zero duration", rep(`"duration_min":null`, `"duration_min":0`)},
		{"huge duration", rep(`"duration_min":null`, `"duration_min":999999`)},
		{"bad weekday", rep(`"fri"`, `"friday"`)},
		{"bad date type", rep(`"type":"weekday"`, `"type":"soon"`)},
		{"bad absolute", rep(`{"type":"weekday","value":"fri"}`, `{"type":"absolute","value":"2026-02-30"}`)},
		{"relative too far", rep(`{"type":"weekday","value":"fri"}`, `{"type":"relative","days":5}`)},
		{"bad rrule", rep(`"recurrence":null`, `"recurrence":"EVERY FRIDAY"`)},
		{"rrule without freq", rep(`"recurrence":null`, `"recurrence":"BYDAY=FR"`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode(tt.raw, now, ekb)
			if err == nil {
				t.Fatal("accepted")
			}
			if !llm.Retryable(err) {
				t.Errorf("a malformed answer must be retried with the error text: %v", err)
			}
			if strings.TrimSpace(err.Error()) == "" {
				t.Error("empty error text")
			}
		})
	}
}

func TestDecodeEmptyRecurrenceBecomesNil(t *testing.T) {
	e, err := Decode(`{"kind":"event","title":"x","date":null,"time_from":null,"time_to":null,"duration_min":null,
	 "deadline":null,"recurrence":"","category_guess":"other","missing":[]}`, now, ekb)
	if err != nil || e.Recurrence != nil {
		t.Fatalf("%v %v", e.Recurrence, err)
	}
}

func TestBuild(t *testing.T) {
	abs := timeparse.Date{Type: "absolute", Value: "2026-10-20"}
	tests := []struct {
		name    string
		in      Extract
		date    string
		start   string
		end     string
		dl      string
		missing string
		problem string
	}{
		{
			name: "plan example: KVN on Friday 18-20",
			in:   Extract{Kind: "event", Title: "КВН", Date: wday("fri", ""), TimeFrom: s("18:00"), TimeTo: s("20:00"), CategoryGuess: "hobby"},
			date: "2026-10-30", start: "2026-10-30T18:00:00+05:00", end: "2026-10-30T20:00:00+05:00",
		},
		{
			name: "plan example: coursework 6 hours until Friday",
			in:   Extract{Kind: "task", Title: "Курсовая", DurationMin: ip(360), Deadline: wday("fri", ""), CategoryGuess: "study"},
			dl:   "2026-10-30",
		},
		{
			name: "duration instead of an end time",
			in:   Extract{Kind: "event", Title: "Матан", Date: wday("tue", ""), TimeFrom: s("08:30"), DurationMin: ip(90), CategoryGuess: "study"},
			date: "2026-11-03", start: "2026-11-03T08:30:00+05:00", end: "2026-11-03T10:00:00+05:00",
		},
		{
			name:    "event without a time asks for it",
			in:      Extract{Kind: "event", Title: "Стоматолог", Date: rel(1), CategoryGuess: "chores"},
			date:    "2026-10-29",
			missing: "time_from,time_to",
		},
		{
			name: "only the start is known",
			in:   Extract{Kind: "event", Title: "Кино", Date: rel(1), TimeFrom: s("20:00"), CategoryGuess: "rest"},
			date: "2026-10-29", start: "2026-10-29T20:00:00+05:00",
			missing: "time_to",
		},
		{
			name:    "event with nothing but a title",
			in:      Extract{Kind: "event", Title: "Встреча", CategoryGuess: "other"},
			missing: "date,time_from,time_to",
		},
		{
			name:    "task without duration and deadline",
			in:      Extract{Kind: "task", Title: "Доклад", CategoryGuess: "study"},
			missing: "duration_min,deadline",
		},
		{
			name: "the model's own missing is ignored",
			in:   Extract{Kind: "event", Title: "КВН", Date: wday("fri", ""), TimeFrom: s("18:00"), TimeTo: s("20:00"), CategoryGuess: "hobby", Missing: []string{"date"}},
			date: "2026-10-30", start: "2026-10-30T18:00:00+05:00", end: "2026-10-30T20:00:00+05:00",
		},
		{
			name: "task with a date and no deadline uses the date as deadline",
			in:   Extract{Kind: "task", Title: "Отчёт", Date: rel(1), DurationMin: ip(60), CategoryGuess: "work"},
			dl:   "2026-10-29",
		},
		{
			name: "end before start is a question for the user",
			in:   Extract{Kind: "event", Title: "Ночь", Date: rel(1), TimeFrom: s("22:00"), TimeTo: s("02:00"), CategoryGuess: "rest"},
			date: "2026-10-29", start: "2026-10-29T22:00:00+05:00", problem: "end_not_after_start",
		},
		{
			name: "zero-length event is not allowed",
			in:   Extract{Kind: "event", Title: "X", Date: rel(1), TimeFrom: s("10:00"), TimeTo: s("10:00"), CategoryGuess: "other"},
			date: "2026-10-29", start: "2026-10-29T10:00:00+05:00", problem: "end_not_after_start",
		},
		{
			name: "today but the time has passed",
			in:   Extract{Kind: "event", Title: "Пара", Date: rel(0), TimeFrom: s("09:00"), TimeTo: s("10:30"), CategoryGuess: "study"},
			date: "2026-10-28", start: "2026-10-28T09:00:00+05:00", end: "2026-10-28T10:30:00+05:00", problem: "in_past",
		},
		{
			name: "this Monday on a Wednesday is in the past",
			in:   Extract{Kind: "event", Title: "Пара", Date: wday("mon", "this"), CategoryGuess: "study"},
			date: "2026-10-26", problem: "in_past", missing: "time_from,time_to",
		},
		{
			name: "bare weekday today after its time means next week, no problem",
			in:   Extract{Kind: "event", Title: "Пара", Date: wday("wed", ""), TimeFrom: s("09:00"), TimeTo: s("10:30"), CategoryGuess: "study"},
			date: "2026-11-04", start: "2026-11-04T09:00:00+05:00", end: "2026-11-04T10:30:00+05:00",
		},
		{
			name: "deadline in the past",
			in:   Extract{Kind: "task", Title: "Старое", Deadline: &abs, DurationMin: ip(60), CategoryGuess: "study"},
			dl:   "2026-10-20", problem: "in_past",
		},
		{
			name: "deadline today with not enough hours left",
			in:   Extract{Kind: "task", Title: "Эссе", Deadline: rel(0), DurationMin: ip(13 * 60), CategoryGuess: "study"},
			dl:   "2026-10-28", problem: "deadline_unreachable",
		},
		{
			name: "deadline today with enough hours left",
			in:   Extract{Kind: "task", Title: "Эссе", Deadline: rel(0), DurationMin: ip(11 * 60), CategoryGuess: "study"},
			dl:   "2026-10-28",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Build(tt.in, now, ekb)
			eqp(t, "date", r.Date, tt.date)
			eqp(t, "start", r.Start, tt.start)
			eqp(t, "end", r.End, tt.end)
			eqp(t, "deadline", r.Deadline, tt.dl)
			if join(r.Missing) != tt.missing {
				t.Errorf("missing %v, want %q", r.Missing, tt.missing)
			}
			var codes []string
			for _, p := range r.Problems {
				codes = append(codes, p.Code)
				if p.Message == "" {
					t.Error("problem without a message")
				}
			}
			if join(codes) != tt.problem {
				t.Errorf("problems %v, want %q", codes, tt.problem)
			}
			if r.Missing == nil || r.Problems == nil {
				t.Error("missing and problems must be empty lists, not null")
			}
		})
	}
}

func TestBuildDropsFieldsOfTheOtherKind(t *testing.T) {
	task := Build(Extract{Kind: "task", Title: "T", Date: rel(1), TimeFrom: s("10:00"), TimeTo: s("11:00"),
		DurationMin: ip(60), Recurrence: s("FREQ=DAILY"), CategoryGuess: "other"}, now, ekb)
	if task.Date != nil || task.TimeFrom != nil || task.TimeTo != nil || task.Recurrence != nil || task.Start != nil {
		t.Errorf("task keeps event fields: %+v", task)
	}
	ev := Build(Extract{Kind: "event", Title: "E", Date: rel(1), TimeFrom: s("10:00"), TimeTo: s("11:00"),
		Deadline: rel(2), CategoryGuess: "other"}, now, ekb)
	if ev.Deadline != nil {
		t.Errorf("event keeps a deadline: %+v", ev)
	}
}

func TestBuildKeepsTheRecurrence(t *testing.T) {
	r := Build(Extract{Kind: "event", Title: "КВН", Date: wday("fri", ""), TimeFrom: s("18:00"), TimeTo: s("20:00"),
		Recurrence: s("FREQ=WEEKLY;BYDAY=FR"), CategoryGuess: "hobby"}, now, ekb)
	if r.Recurrence == nil || *r.Recurrence != "FREQ=WEEKLY;BYDAY=FR" {
		t.Errorf("%+v", r.Recurrence)
	}
}

type fakeCaller struct {
	answer string
	err    error
	inputs []llm.Input
}

func (f *fakeCaller) Call(ctx context.Context, in llm.Input) (llm.Output, error) {
	f.inputs = append(f.inputs, in)
	if f.err != nil {
		return llm.Output{}, f.err
	}
	raw := llm.ExtractJSON(f.answer)
	if err := in.Check(raw); err != nil {
		return llm.Output{}, err
	}
	return llm.Output{JSON: raw, Model: "a/m:free", Calls: 1}, nil
}

func newParser(f *fakeCaller) *Parser {
	return &Parser{LLM: f, Loc: ekb, Now: func() time.Time { return now }}
}

const kvn = `{"kind":"event","title":"КВН","date":{"type":"weekday","days":null,"value":"fri","week":null},"time_from":"18:00","time_to":"20:00","duration_min":null,"deadline":null,"recurrence":null,"category_guess":"hobby","missing":[]}`

func TestParserEndToEnd(t *testing.T) {
	f := &fakeCaller{answer: "```json\n" + kvn + "\n```"}
	got, err := newParser(f).Parse(context.Background(), "  КВН в пятницу с 18 до 20 ", llm.SourceUser)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "event" || *got.Start != "2026-10-30T18:00:00+05:00" || got.Model != "a/m:free" || got.Calls != 1 {
		t.Errorf("%+v", got)
	}
	in := f.inputs[0]
	if in.Task != llm.TaskParse || in.Source != llm.SourceUser || in.SchemaName != SchemaName ||
		!strings.Contains(in.System, "28 октября 2026") || !strings.Contains(in.User, "КВН в пятницу с 18 до 20") {
		t.Errorf("input %+v", in)
	}
}

func TestParserSendsOnlyThePhraseTheMomentAndTheZone(t *testing.T) {
	f := &fakeCaller{answer: kvn}
	newParser(f).Parse(context.Background(), "Тайная фраза", llm.SourceUser)
	in := f.inputs[0]
	if strings.Count(in.User, "Тайная фраза") != 1 || strings.Contains(in.System, "Тайная") {
		t.Errorf("%q / %q", in.System, in.User)
	}
}

func TestParserSourceIsPassedOn(t *testing.T) {
	f := &fakeCaller{answer: kvn}
	newParser(f).Parse(context.Background(), "x", llm.SourceEval)
	if f.inputs[0].Source != llm.SourceEval {
		t.Errorf("%v", f.inputs[0].Source)
	}
}

func TestParserRejectsAnInvalidAnswer(t *testing.T) {
	f := &fakeCaller{answer: `{"kind":"party"}`}
	_, err := newParser(f).Parse(context.Background(), "вечеринка", llm.SourceUser)
	if err == nil || !llm.Retryable(err) {
		t.Fatalf("err %v", err)
	}
}

func TestParserTextLimits(t *testing.T) {
	f := &fakeCaller{answer: kvn}
	p := newParser(f)
	for _, text := range []string{"", "   ", strings.Repeat("я", MaxTextRunes+1)} {
		if _, err := p.Parse(context.Background(), text, llm.SourceUser); !errors.Is(err, ErrBadText) {
			t.Errorf("%d runes: %v", len([]rune(text)), err)
		}
	}
	if len(f.inputs) != 0 {
		t.Error("a bad text must not reach the model")
	}
	if _, err := p.Parse(context.Background(), strings.Repeat("я", MaxTextRunes), llm.SourceUser); err != nil {
		t.Errorf("500 runes is fine: %v", err)
	}
}

func TestParserPassesLimitErrorsThrough(t *testing.T) {
	reset := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	f := &fakeCaller{err: &llm.DailyLimitError{ResetAt: reset}}
	_, err := newParser(f).Parse(context.Background(), "x", llm.SourceUser)
	var dl *llm.DailyLimitError
	if !errors.As(err, &dl) {
		t.Fatalf("%v", err)
	}
}
