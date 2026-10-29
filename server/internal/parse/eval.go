package parse

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"lifeagent/internal/llm"
)

// EvalCase is one reference phrase with the expected card. Dates are resolved
// against a fixed "now", so the expectations never go stale.
type EvalCase struct {
	Text   string     `json:"text"`
	Expect EvalExpect `json:"expect"`
}

type EvalExpect struct {
	Kind        string   `json:"kind"`
	Title       string   `json:"title"`
	Date        *string  `json:"date"`  // event: YYYY-MM-DD
	Start       *string  `json:"start"` // event: "YYYY-MM-DD HH:MM"
	End         *string  `json:"end"`
	DurationMin *int     `json:"duration_min"` // task
	Deadline    *string  `json:"deadline"`     // task: YYYY-MM-DD
	Recurrence  *string  `json:"recurrence"`
	Missing     []string `json:"missing"`
	// Guessed are the fields the phrase does not name: any value is accepted
	// there, only an empty one is wrong.
	Guessed  []string `json:"guessed"`
	Category string   `json:"category"` // reported on its own, not part of the verdict
}

type EvalSet struct {
	Now   string     `json:"now"` // RFC 3339
	Cases []EvalCase `json:"cases"`
}

func LoadEvalSet(path string) (EvalSet, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return EvalSet{}, err
	}
	var s EvalSet
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return s, fmt.Errorf("%s: %w", path, err)
	}
	seen := map[string]bool{}
	for _, c := range s.Cases {
		if seen[c.Text] {
			return s, fmt.Errorf("%s: duplicate phrase %q", path, c.Text)
		}
		seen[c.Text] = true
	}
	return s, nil
}

const evalTime = "2006-01-02 15:04"

// Diff lists what in the parsed card differs from the expectation; empty means correct.
// The category is not compared here.
func Diff(want EvalExpect, got Result, loc *time.Location) []string {
	var d []string
	add := func(field string, w, g any) { d = append(d, fmt.Sprintf("%s: want %v, got %v", field, w, g)) }
	str := func(p *string) string {
		if p == nil {
			return "null"
		}
		return *p
	}
	inum := func(p *int) string {
		if p == nil {
			return "null"
		}
		return fmt.Sprint(*p)
	}
	clock := func(p *string) *string {
		if p == nil {
			return nil
		}
		t, err := time.Parse(time.RFC3339, *p)
		if err != nil {
			return p
		}
		s := t.In(loc).Format(evalTime)
		return &s
	}

	if got.Kind != want.Kind {
		add("kind", want.Kind, got.Kind)
	}
	if !titleMatches(want.Title, got.Title) {
		add("title", want.Title, got.Title)
	}
	guessed := func(fields ...string) bool {
		for _, f := range fields {
			if slices.Contains(want.Guessed, f) {
				return true
			}
		}
		return false
	}
	// cmp compares a field, or for a guessed one only checks that it is filled.
	cmp := func(name, w, g string, guess bool) {
		if guess && g == "null" {
			add(name, "any", g)
		} else if !guess && w != g {
			add(name, w, g)
		}
	}
	if want.Kind == "event" {
		cmp("date", str(want.Date), str(got.Date), guessed("date"))
		cmp("start", str(want.Start), str(clock(got.Start)), guessed("date", "time_from"))
		cmp("end", str(want.End), str(clock(got.End)), guessed("date", "time_from", "time_to", "duration_min"))
	} else {
		cmp("duration_min", inum(want.DurationMin), inum(got.DurationMin), guessed("duration_min"))
		cmp("deadline", str(want.Deadline), str(got.Deadline), guessed("deadline"))
	}
	if normRule(want.Recurrence) != normRule(got.Recurrence) {
		add("recurrence", normRule(want.Recurrence), normRule(got.Recurrence))
	}
	wm, gm := slices.Clone(want.Missing), slices.Clone(got.Missing)
	sort.Strings(wm)
	sort.Strings(gm)
	if strings.Join(wm, ",") != strings.Join(gm, ",") {
		add("missing", wm, gm)
	}
	return d
}

func normTitle(s string) string {
	s = strings.ToLower(strings.ReplaceAll(s, "ё", "е"))
	return strings.Join(strings.Fields(strings.Trim(s, " .,!?:;«»\"'")), " ")
}

// titleMatches compares titles by word stems (first four letters), so that
// "Физика" matches "Лекция по физике".
func titleMatches(want, got string) bool {
	g := stems(got)
	for st := range stems(want) {
		if g[st] {
			return true
		}
	}
	return false
}

func stems(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(normTitle(s)) {
		r := []rune(w)
		if len(r) < 3 {
			continue
		}
		out[string(r[:min(len(r), 4)])] = true
	}
	return out
}

// normRule makes the order of RRULE parts irrelevant.
func normRule(p *string) string {
	if p == nil {
		return "null"
	}
	parts := strings.Split(strings.ToUpper(*p), ";")
	sort.Strings(parts)
	return strings.Join(parts, ";")
}

// EvalRecord is one line of the progress file.
type EvalRecord struct {
	Text       string   `json:"text"`
	Correct    bool     `json:"correct"`
	CategoryOK bool     `json:"category_ok"`
	Diffs      []string `json:"diffs,omitempty"`
	Error      string   `json:"error,omitempty"` // no card: the models failed
	Model      string   `json:"model,omitempty"`
	Calls      int      `json:"calls"`
	Got        *Result  `json:"got,omitempty"`
}

type EvalSummary struct {
	Total, Done, Correct, CategoryOK int
	Skipped                          int        // the model was unavailable: not a verdict, asked again next run
	ResetAt                          *time.Time // set when the daily limit stopped the run
	Failures                         []EvalRecord
}

func (s EvalSummary) Accuracy() float64 {
	if s.Done == 0 {
		return 0
	}
	return float64(s.Correct) / float64(s.Done)
}

type EvalOptions struct {
	Set      EvalSet
	Parser   *Parser
	Progress string        // JSONL file; phrases already in it are skipped, so a run can continue later
	Pause    time.Duration // between model requests, to stay under the per-minute limit
	Limit    int           // at most this many new phrases in this run; 0 = no limit
	Out      io.Writer
}

// RunEval parses the phrases that have no record yet. It stops, keeping the
// progress, when the account's daily limit is spent; the next run continues there.
func RunEval(ctx context.Context, o EvalOptions) (EvalSummary, error) {
	loc := o.Parser.Loc
	done, err := readProgress(o.Progress)
	if err != nil {
		return EvalSummary{}, err
	}
	f, err := os.OpenFile(o.Progress, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return EvalSummary{}, err
	}
	defer f.Close()

	sum := EvalSummary{Total: len(o.Set.Cases)}
	spent, first := 0, true
	for _, c := range o.Set.Cases {
		if _, ok := done[c.Text]; ok {
			continue
		}
		if o.Limit > 0 && spent >= o.Limit {
			break
		}
		if !first && o.Pause > 0 {
			select {
			case <-time.After(o.Pause):
			case <-ctx.Done():
				return finish(sum, done), ctx.Err()
			}
		}
		first = false

		rec := EvalRecord{Text: c.Text}
		res, err := o.Parser.Parse(ctx, c.Text, llm.SourceEval)
		var dl *llm.DailyLimitError
		var ex *llm.ExhaustedError
		switch {
		case errors.As(err, &dl):
			t := dl.ResetAt
			sum.ResetAt = &t
			return finish(sum, done), nil
		case errors.Is(err, llm.ErrNotConfigured), errors.Is(err, llm.ErrAuth), ctx.Err() != nil:
			if ctx.Err() != nil {
				err = ctx.Err()
			}
			return finish(sum, done), err
		case errors.As(err, &ex) && unavailable(ex.Last):
			// A busy or down model says nothing about its quality: keep no record.
			sum.Skipped++
			spent++
			if o.Out != nil {
				fmt.Fprintf(o.Out, "skip %s (%s)\n", c.Text, ex.Last)
			}
			continue
		case errors.As(err, &ex):
			rec.Error, rec.Calls = ex.Error(), ex.Calls
		case err != nil:
			rec.Error = err.Error()
		default:
			rec.Diffs = Diff(c.Expect, res.Result, loc)
			rec.Correct = len(rec.Diffs) == 0
			rec.CategoryOK = res.CategoryGuess == c.Expect.Category
			rec.Model, rec.Calls, rec.Got = res.Model, res.Calls, &res.Result
		}
		if rec.Error != "" {
			rec.Diffs = []string{"no card: " + rec.Error}
		}
		spent++
		done[c.Text] = rec
		line, _ := json.Marshal(rec)
		if _, err := f.Write(append(line, '\n')); err != nil {
			return finish(sum, done), err
		}
		if o.Out != nil {
			mark := "ok  "
			if !rec.Correct {
				mark = "FAIL"
			}
			fmt.Fprintf(o.Out, "%s %s\n", mark, c.Text)
		}
	}
	return finish(sum, done), nil
}

func unavailable(last string) bool { return last == "limit" || last == "timeout" || last == "error" }

func finish(sum EvalSummary, done map[string]EvalRecord) EvalSummary {
	sum.Done, sum.Correct, sum.CategoryOK, sum.Failures = 0, 0, 0, nil // Skipped and ResetAt stay
	texts := make([]string, 0, len(done))
	for t := range done {
		texts = append(texts, t)
	}
	sort.Strings(texts)
	for _, t := range texts {
		r := done[t]
		sum.Done++
		if r.Correct {
			sum.Correct++
		} else {
			sum.Failures = append(sum.Failures, r)
		}
		if r.CategoryOK {
			sum.CategoryOK++
		}
	}
	return sum
}

func readProgress(path string) (map[string]EvalRecord, error) {
	out := map[string]EvalRecord{}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var r EvalRecord
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out[r.Text] = r
	}
	return out, sc.Err()
}
