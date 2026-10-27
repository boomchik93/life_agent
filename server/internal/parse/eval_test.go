package parse

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lifeagent/internal/llm"
)

func TestReferenceSetIsConsistent(t *testing.T) {
	set, err := LoadEvalSet("../../eval/phrases.json")
	if err != nil {
		t.Fatal(err)
	}
	if n := len(set.Cases); n < 30 || n > 50 {
		t.Errorf("%d phrases, the plan wants 30 to 50", n)
	}
	if _, err := time.Parse(time.RFC3339, set.Now); err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	missing, recurring := 0, 0
	for _, c := range set.Cases {
		kinds[c.Expect.Kind]++
		if len(c.Expect.Missing) > 0 {
			missing++
		}
		if c.Expect.Recurrence != nil {
			recurring++
		}
		if c.Expect.Kind == "event" && c.Expect.Date == nil {
			t.Errorf("%q: event without a date", c.Text)
		}
		if c.Expect.Kind == "task" && (c.Expect.Date != nil || c.Expect.Start != nil) {
			t.Errorf("%q: task with event fields", c.Text)
		}
	}
	if kinds["event"] < 10 || kinds["task"] < 8 || missing < 6 || recurring < 3 {
		t.Errorf("coverage: kinds %v, incomplete %d, recurring %d (events, tasks, repeats and incomplete phrases are all required)", kinds, missing, recurring)
	}
}

// The expectations are produced by the same code that serves the app, so a
// perfect model (one that returns exactly the parts) must score 100%.
func TestReferenceSetPerfectModelScoresFull(t *testing.T) {
	set, _ := LoadEvalSet("../../eval/phrases.json")
	now, _ := time.Parse(time.RFC3339, set.Now)
	now = now.In(ekb)
	for _, c := range set.Cases {
		e := c.Expect
		ex := Extract{Kind: e.Kind, Title: e.Title, CategoryGuess: e.Category, Recurrence: e.Recurrence, DurationMin: e.DurationMin}
		if e.Kind == "event" {
			ex.Date = absDate(*e.Date)
			if e.Start != nil {
				ex.TimeFrom = s(strings.Split(*e.Start, " ")[1])
			}
			if e.End != nil {
				ex.TimeTo = s(strings.Split(*e.End, " ")[1])
			}
		} else if e.Deadline != nil {
			ex.Deadline = absDate(*e.Deadline)
		}
		got := Build(ex, now, ekb)
		if d := Diff(e, got, ekb); len(d) > 0 {
			t.Errorf("%q: %v", c.Text, d)
		}
	}
}

func TestDiff(t *testing.T) {
	sp := func(v string) *string { return &v }
	want := EvalExpect{Kind: "event", Title: "Физика", Date: sp("2026-11-02"), Start: sp("2026-11-02 08:30"), End: sp("2026-11-02 10:00"),
		Recurrence: sp("FREQ=WEEKLY;BYDAY=MO"), Missing: []string{"b", "a"}}
	got := Result{Kind: "event", Title: "Лекция по физике", Date: sp("2026-11-02"), Start: sp("2026-11-02T08:30:00+05:00"),
		End: sp("2026-11-02T10:00:00+05:00"), Recurrence: sp("BYDAY=MO;FREQ=WEEKLY"), Missing: []string{"a", "b"}}
	if d := Diff(want, got, ekb); len(d) != 0 {
		t.Fatalf("equivalent cards differ: %v", d)
	}
	for name, mod := range map[string]func(*Result){
		"kind":       func(r *Result) { r.Kind = "task" },
		"title":      func(r *Result) { r.Title = "Химия" },
		"date":       func(r *Result) { r.Date = sp("2026-11-03") },
		"start":      func(r *Result) { r.Start = sp("2026-11-02T08:00:00+05:00") },
		"end":        func(r *Result) { r.End = nil },
		"recurrence": func(r *Result) { r.Recurrence = nil },
		"missing":    func(r *Result) { r.Missing = []string{"a"} },
	} {
		g := got
		mod(&g)
		d := Diff(want, g, ekb)
		if len(d) == 0 || !strings.HasPrefix(d[0], name) {
			t.Errorf("%s: %v", name, d)
		}
	}
	task := EvalExpect{Kind: "task", Title: "Курсовая", DurationMin: ip(360), Deadline: sp("2026-10-30"), Missing: []string{}}
	tg := Result{Kind: "task", Title: "Курсовая работа", DurationMin: ip(360), Deadline: sp("2026-10-30"), Missing: []string{}}
	if d := Diff(task, tg, ekb); len(d) != 0 {
		t.Fatalf("%v", d)
	}
	tg.DurationMin = ip(300)
	if d := Diff(task, tg, ekb); len(d) != 1 {
		t.Fatalf("%v", d)
	}
}

// evalCaller returns answers per phrase and can run out of quota.
type evalCaller struct {
	calls   []string
	dailyAt int // the call number (1-based) that hits the daily limit; 0 = never
	reset   time.Time
}

func (e *evalCaller) Call(ctx context.Context, in llm.Input) (llm.Output, error) {
	e.calls = append(e.calls, in.User)
	if in.Source != llm.SourceEval {
		panic("eval must mark its calls as eval")
	}
	if e.dailyAt > 0 && len(e.calls) == e.dailyAt {
		return llm.Output{}, &llm.DailyLimitError{ResetAt: e.reset}
	}
	raw := `{"kind":"task","title":"Курсовая","date":null,"time_from":null,"time_to":null,"duration_min":360,
	 "deadline":{"type":"relative","days":1},"recurrence":null,"category_guess":"study","missing":[]}`
	if err := in.Check(raw); err != nil {
		return llm.Output{}, err
	}
	return llm.Output{JSON: raw, Model: "a/m:free", Calls: 1}, nil
}

func TestRunEvalStopsOnDailyLimitAndResumes(t *testing.T) {
	sp := func(v string) *string { return &v }
	set := EvalSet{Now: "2026-10-28T12:00:00+05:00"}
	for _, text := range []string{"one", "two", "three", "four"} {
		set.Cases = append(set.Cases, EvalCase{Text: text, Expect: EvalExpect{
			Kind: "task", Title: "Курсовая", DurationMin: ip(360), Deadline: sp("2026-10-29"), Missing: []string{}, Category: "study"}})
	}
	set.Cases[3].Expect.Deadline = sp("2026-10-30") // the stub answers "tomorrow": this one is wrong
	progress := filepath.Join(t.TempDir(), "p.jsonl")
	reset := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)

	f := &evalCaller{dailyAt: 3, reset: reset}
	run := func(f *evalCaller, limit int) EvalSummary {
		sum, err := RunEval(context.Background(), EvalOptions{
			Set: set, Progress: progress, Limit: limit,
			Parser: &Parser{LLM: f, Loc: ekb, Now: func() time.Time { return now }},
		})
		if err != nil {
			t.Fatal(err)
		}
		return sum
	}
	sum := run(f, 0)
	if sum.Done != 2 || sum.Correct != 2 || sum.ResetAt == nil || !sum.ResetAt.Equal(reset) || sum.Total != 4 {
		t.Fatalf("first day: %+v", sum)
	}

	// Next day: only the phrases without a record are asked.
	f2 := &evalCaller{}
	sum = run(f2, 0)
	if len(f2.calls) != 2 || !strings.Contains(f2.calls[0], "three") || !strings.Contains(f2.calls[1], "four") {
		t.Fatalf("resume asked %v", f2.calls)
	}
	if sum.Done != 4 || sum.Correct != 3 || sum.CategoryOK != 4 || sum.ResetAt != nil || sum.Accuracy() != 0.75 {
		t.Fatalf("second day: %+v", sum)
	}
	if len(sum.Failures) != 1 || sum.Failures[0].Text != "four" || !strings.HasPrefix(sum.Failures[0].Diffs[0], "deadline") {
		t.Fatalf("failures %+v", sum.Failures)
	}

	// Nothing is left: no calls at all.
	f3 := &evalCaller{}
	run(f3, 0)
	if len(f3.calls) != 0 {
		t.Fatalf("a finished run asked %v", f3.calls)
	}
	if raw, _ := os.ReadFile(progress); strings.Count(string(raw), "\n") != 4 {
		t.Fatalf("progress file:\n%s", raw)
	}
}

func TestRunEvalLimitAndModelFailure(t *testing.T) {
	set := EvalSet{Cases: []EvalCase{{Text: "a", Expect: EvalExpect{Kind: "task", Title: "x"}}, {Text: "b", Expect: EvalExpect{Kind: "task", Title: "x"}}}}
	progress := filepath.Join(t.TempDir(), "p.jsonl")
	f := &evalCaller{}
	sum, err := RunEval(context.Background(), EvalOptions{Set: set, Progress: progress, Limit: 1,
		Parser: &Parser{LLM: f, Loc: ekb, Now: func() time.Time { return now }}})
	if err != nil || sum.Done != 1 || len(f.calls) != 1 {
		t.Fatalf("limit: %+v %v", sum, err)
	}

	// A busy model is not a verdict: nothing is recorded, the phrases are asked again.
	busy := &failCaller{last: "limit"}
	progress3 := filepath.Join(t.TempDir(), "p3.jsonl")
	sum, err = RunEval(context.Background(), EvalOptions{Set: set, Progress: progress3,
		Parser: &Parser{LLM: busy, Loc: ekb, Now: func() time.Time { return now }}})
	if err != nil || sum.Done != 0 || sum.Skipped != 2 {
		t.Fatalf("busy model: %+v %v", sum, err)
	}
	if raw, _ := os.ReadFile(progress3); len(raw) != 0 {
		t.Fatalf("skipped phrases were recorded: %s", raw)
	}

	// A chain that answers wrongly counts as a wrong answer and the run goes on.
	bad := &failCaller{last: "invalid_json"}
	progress2 := filepath.Join(t.TempDir(), "p2.jsonl")
	sum, err = RunEval(context.Background(), EvalOptions{Set: set, Progress: progress2,
		Parser: &Parser{LLM: bad, Loc: ekb, Now: func() time.Time { return now }}})
	if err != nil || sum.Done != 2 || sum.Correct != 0 || len(sum.Failures) != 2 || !strings.Contains(sum.Failures[0].Diffs[0], "no card") {
		t.Fatalf("failing chain: %+v %v", sum, err)
	}
}

type failCaller struct{ last string }

func (f failCaller) Call(context.Context, llm.Input) (llm.Output, error) {
	return llm.Output{}, &llm.ExhaustedError{Calls: 3, Last: f.last}
}

func TestRunEvalAbortsOnAuthError(t *testing.T) {
	set := EvalSet{Cases: []EvalCase{{Text: "a", Expect: EvalExpect{Kind: "task", Title: "x"}}}}
	p := &Parser{LLM: authCaller{}, Loc: ekb, Now: func() time.Time { return now }}
	_, err := RunEval(context.Background(), EvalOptions{Set: set, Progress: filepath.Join(t.TempDir(), "p.jsonl"), Parser: p})
	if err == nil {
		t.Fatal("a rejected key must stop the run, not score every phrase as wrong")
	}
}

type authCaller struct{}

func (authCaller) Call(context.Context, llm.Input) (llm.Output, error) {
	return llm.Output{}, llm.ErrAuth
}
