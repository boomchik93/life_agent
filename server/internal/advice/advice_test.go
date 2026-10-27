package advice

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"lifeagent/internal/llm"
	"lifeagent/internal/load"
)

func asm(sleep, overload, noRest, state load.Color) load.Assessment {
	r := func(c load.Color) load.Risk { return load.Risk{Color: c} }
	return load.Assessment{Sleep: r(sleep), Overload: r(overload), NoRest: r(noRest), State: r(state)}
}

func TestDecide(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	ago := func(d float64) *time.Time { t := now.Add(-time.Duration(d * 24 * float64(time.Hour))); return &t }
	s := DefaultSettings()
	g, y, r, gray := load.Green, load.Yellow, load.Red, load.Gray

	tests := []struct {
		name    string
		last    *time.Time
		a       load.Assessment
		events  int
		due     bool
		reasons []string
	}{
		{"all green", nil, asm(g, g, g, g), 5, false, nil},
		{"no data is no reason", nil, asm(gray, gray, gray, gray), 0, false, nil},
		{"yellow overload", nil, asm(g, y, g, g), 5, true, []string{ReasonOverload}},
		{"red overload", nil, asm(g, r, g, g), 5, true, []string{ReasonOverload}},
		{"lack of sleep", nil, asm(y, g, g, g), 5, true, []string{ReasonSleep}},
		{"red state", nil, asm(g, g, g, r), 5, true, []string{ReasonState}},
		{"yellow state is not enough", nil, asm(g, g, g, y), 5, false, nil},
		{"no rest alone is not a reason", nil, asm(g, g, y, g), 5, false, nil},
		{"too many events", nil, asm(g, g, g, g), 31, true, []string{ReasonEvents}},
		{"exactly the limit of events", nil, asm(g, g, g, g), 30, false, nil},
		{"several reasons", nil, asm(r, y, g, r), 40, true, []string{ReasonOverload, ReasonSleep, ReasonState, ReasonEvents}},
		{"6 days since the last advice", ago(6), asm(g, r, g, g), 5, false, nil},
		{"exactly 7 days", ago(7), asm(g, r, g, g), 5, false, nil},
		{"8 days", ago(8), asm(g, r, g, g), 5, true, []string{ReasonOverload}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Decide(now, tt.last, s, tt.a, tt.events)
			if d.Due != tt.due {
				t.Fatalf("due = %v, want %v (%v)", d.Due, tt.due, d.Reasons)
			}
			if tt.due && strings.Join(d.Reasons, ",") != strings.Join(tt.reasons, ",") {
				t.Errorf("reasons = %v, want %v", d.Reasons, tt.reasons)
			}
		})
	}
	s.IntervalDays = 3
	if !Decide(now, ago(4), s, asm(g, y, g, g), 0).Due {
		t.Error("the interval is a setting")
	}
}

func TestDecode(t *testing.T) {
	long := `{"text":"` + strings.Repeat("Отдохни. ", 70) + `"}`
	tests := []struct {
		name, raw string
		ok        bool
		retry     bool
	}{
		{"fine", `{"text":"Сдвинь сдачу на среду. Ляг до полуночи. Выбери один вечер без дел."}`, true, false},
		{"one sentence without a stop", `{"text":"Ляг пораньше"}`, true, false},
		{"four sentences", `{"text":"Раз. Два. Три. Четыре."}`, false, true},
		{"too long", long, false, true},
		{"english", `{"text":"Please rest more and sleep early today."}`, false, true},
		{"empty", `{"text":"  "}`, false, true},
		{"extra field", `{"text":"Ляг пораньше.","note":"x"}`, false, true},
		{"not json", `текст`, false, true},
		{"decimal point is not a sentence end", `{"text":"Учёба 6.5 часов в день. Сократи до 5."}`, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, err := Decode(tt.raw)
			if (err == nil) != tt.ok {
				t.Fatalf("err = %v, want ok=%v", err, tt.ok)
			}
			if err != nil && llm.Retryable(err) != tt.retry {
				t.Errorf("retryable = %v", llm.Retryable(err))
			}
			if tt.ok && text == "" {
				t.Error("empty text")
			}
		})
	}
}

func TestPromptKeepsPrivateDataOut(t *testing.T) {
	in := Input{
		Reasons:         []string{ReasonSleep, ReasonState},
		HoursByCategory: map[load.Category]float64{load.Study: 31.5, load.Hobby: 2},
		Colors:          map[string]load.Color{"overload": load.Yellow, "no_rest": load.Red},
	}
	p := UserPrompt(in)
	for _, want := range []string{"недосып", "состояние по ежедневному чек-ину плохое", "учёба: 31,5 ч", "хобби: 2 ч",
		"Риск перегруза: жёлтый", "Риск «нет отдыха»: красный", "<<<", ">>>"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q:\n%s", want, p)
		}
	}
	for _, no := range []string{"сон за", "энергии и настроения (1"} {
		if strings.Contains(p, no) {
			t.Errorf("private data in the default prompt: %q", no)
		}
	}

	sleep, state := 5.5, 2.0
	in.Private = &Private{AvgSleepHours: &sleep, AvgStateScore: &state}
	p = UserPrompt(in)
	if !strings.Contains(p, "Средний сон за последние дни: 5,5 ч") || !strings.Contains(p, "(1–5): 2") {
		t.Errorf("private data missing when allowed:\n%s", p)
	}
}

type fakeLLM struct {
	answers []string
	err     error
	got     llm.Input
}

func (f *fakeLLM) Call(_ context.Context, in llm.Input) (llm.Output, error) {
	f.got = in
	if f.err != nil {
		return llm.Output{}, f.err
	}
	for _, a := range f.answers {
		if in.Check(a) == nil {
			return llm.Output{JSON: a, Model: "m", Calls: 1}, nil
		}
	}
	return llm.Output{}, &llm.ExhaustedError{Calls: 1, Last: "check_failed"}
}

func TestGenerate(t *testing.T) {
	f := &fakeLLM{answers: []string{`{"text":"Сдвинь сдачу на среду. Ляг до полуночи."}`}}
	a := &Advisor{LLM: f}
	res, err := a.Generate(context.Background(), Input{Reasons: []string{ReasonOverload}}, llm.SourceAuto)
	if err != nil || res.Text != "Сдвинь сдачу на среду. Ляг до полуночи." || res.Model != "m" {
		t.Fatalf("res %+v err %v", res, err)
	}
	if f.got.Task != llm.TaskAdvice || f.got.Source != llm.SourceAuto || f.got.SchemaName != SchemaName {
		t.Errorf("call = %+v", f.got)
	}

	f = &fakeLLM{answers: []string{`{"text":"Rest."}`}}
	if _, err := (&Advisor{LLM: f}).Generate(context.Background(), Input{}, llm.SourceUser); err == nil {
		t.Error("a bad answer must not become advice")
	}
	boom := errors.New("boom")
	if _, err := (&Advisor{LLM: &fakeLLM{err: boom}}).Generate(context.Background(), Input{}, llm.SourceUser); !errors.Is(err, boom) {
		t.Errorf("error lost: %v", err)
	}
}

func TestSettings(t *testing.T) {
	s, err := ParseSettings([]byte(`{"include_private":true}`))
	if err != nil || !s.IncludePrivate || s.IntervalDays != 7 || s.MinQuota != 10 {
		t.Fatalf("%+v %v", s, err)
	}
	for _, bad := range []string{`{"interval_days":0}`, `{"events_week_above":0}`, `{"min_quota":-1}`, `{"x":1}`, `[]`} {
		if _, err := ParseSettings([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}
