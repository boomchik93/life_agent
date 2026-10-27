package plan

import (
	"math/rand"
	"reflect"
	"testing"
	"time"

	"lifeagent/internal/load"
)

var yekat, _ = time.LoadLocation("Asia/Yekaterinburg")

// at: day of October 2026 (Mon 26 .. Sun 1 Nov is day 32), local time.
func at(day, h, m int) time.Time { return time.Date(2026, 10, day, h, m, 0, 0, yekat) }

// week: Monday 26 Oct 2026 to Monday 2 Nov, "now" is Monday 08:00.
func week() Input {
	return Input{
		Now: at(26, 8, 0), From: at(26, 0, 0), To: at(33, 0, 0), Loc: yekat,
		P: DefaultParams(), T: load.DefaultThresholds(),
	}
}

func task(id int64, cat load.Category, mins, deadlineDay int) Task {
	return Task{ID: id, Title: "t", Category: cat, RemainingMin: mins, Deadline: at(deadlineDay, 0, 0)}
}

func sumFor(r Result, id int64) int {
	n := 0
	for _, c := range r.Placed {
		if c.ItemID == id {
			n += int(c.End.Sub(c.Start) / time.Minute)
		}
	}
	return n
}

func mustValid(t *testing.T, in Input, r Result) {
	t.Helper()
	if vs := Validate(in, r.Placed); len(vs) > 0 {
		t.Fatalf("plan breaks the rules: %+v\nplan: %+v", vs, r.Placed)
	}
}

func TestSixHoursStudyBeforeFriday(t *testing.T) {
	in := week()
	in.Tasks = []Task{task(1, load.Study, 360, 30)}
	r := Build(in)
	mustValid(t, in, r)
	if got := sumFor(r, 1); got != 360 || len(r.Unplaced) != 0 {
		t.Fatalf("placed %d min, unplaced %+v", got, r.Unplaced)
	}
	for _, c := range r.Placed {
		if m := c.End.Sub(c.Start); m > 2*time.Hour || m < 30*time.Minute {
			t.Errorf("chunk %v..%v has bad length", c.Start, c.End)
		}
	}
	// Earliest window first: the plan starts at the next quarter after "now".
	if !r.Placed[0].Start.Equal(at(26, 8, 0)) {
		t.Errorf("first chunk starts %v", r.Placed[0].Start)
	}
}

func TestBreakBetweenStudyChunks(t *testing.T) {
	in := week()
	in.Tasks = []Task{task(1, load.Study, 240, 30)}
	r := Build(in)
	mustValid(t, in, r)
	for i := 1; i < len(r.Placed); i++ {
		if gap := r.Placed[i].Start.Sub(r.Placed[i-1].End); gap < 15*time.Minute {
			t.Errorf("gap %v between study chunks", gap)
		}
	}
}

func TestStudyDayLimit(t *testing.T) {
	in := week()
	in.Now = at(26, 7, 0)
	in.Tasks = []Task{task(1, load.Study, 600, 26)} // 10 h on one day: only 6 fit
	r := Build(in)
	mustValid(t, in, r)
	if got := sumFor(r, 1); got != 360 {
		t.Errorf("placed %d min, want 360 (day limit 6 h)", got)
	}
	if len(r.Unplaced) != 1 || r.Unplaced[0].MissingMin != 240 {
		t.Errorf("unplaced = %+v, want 240 missing", r.Unplaced)
	}
}

func TestExistingStudyCountsAgainstTheLimit(t *testing.T) {
	in := week()
	in.Busy = []Busy{{Start: at(26, 9, 0), End: at(26, 14, 0), Category: load.Study}}
	in.Tasks = []Task{task(1, load.Study, 180, 26)}
	r := Build(in)
	mustValid(t, in, r)
	// 5 h of lessons leave 1 h of the 6 h day: 45 min fit before the lessons
	// (a 15-min break is kept), the 15 min left after them are too short for a chunk.
	if got := sumFor(r, 1); got != 45 {
		t.Errorf("placed %d min, want 45", got)
	}
}

func TestNeverTouchesEventsOrSleep(t *testing.T) {
	in := week()
	in.Busy = []Busy{{Start: at(26, 8, 0), End: at(26, 22, 30), Category: load.Other}}
	in.Tasks = []Task{task(1, load.Work, 120, 26)}
	r := Build(in)
	mustValid(t, in, r)
	if got := sumFor(r, 1); got != 30 {
		t.Errorf("only 22:30..23:00 is free before the deadline; placed %d", got)
	}
	if len(r.Unplaced) != 1 || r.Unplaced[0].MissingMin != 90 {
		t.Errorf("unplaced = %+v", r.Unplaced)
	}
}

func TestSortedByDeadlineThenPriority(t *testing.T) {
	in := week()
	in.Now = at(26, 7, 0)
	a := task(1, load.Work, 60, 28)
	b := task(2, load.Work, 60, 27)
	c := task(3, load.Work, 60, 27)
	c.Priority = 2
	in.Tasks = []Task{a, b, c}
	r := Build(in)
	mustValid(t, in, r)
	order := []int64{}
	for _, ch := range r.Placed {
		order = append(order, ch.ItemID)
	}
	if !reflect.DeepEqual(order, []int64{3, 2, 1}) {
		t.Errorf("order %v, want [3 2 1]", order)
	}
}

func TestFreeEveningsStayFree(t *testing.T) {
	in := week()
	// 40 h of work with a deadline on Sunday: far more than the free time before 18:00.
	in.Tasks = []Task{task(1, load.Work, 40*60, 32)}
	r := Build(in)
	mustValid(t, in, r)
	free := 0
	for _, d := range daysOf(in.From, in.To, yekat) {
		ev := in.evening(d)
		taken := false
		for _, c := range r.Placed {
			if c.End.After(ev.s) && c.Start.Before(ev.e) {
				taken = true
			}
		}
		if !taken {
			free++
		}
	}
	if free < 2 {
		t.Errorf("%d free evenings, want at least 2", free)
	}
}

func TestPastEveningsCountAsFree(t *testing.T) {
	// Saturday afternoon: Mon..Fri evenings are over and empty, nothing must be reserved.
	in := week()
	in.Now = at(31, 12, 0)
	need, protected := in.evenings(nil)
	if need != 2 || len(protected) != 0 {
		t.Errorf("need %d protected %v", need, protected)
	}
}

func TestDeferredAndOverdue(t *testing.T) {
	in := week()
	in.Now = at(28, 12, 0)
	in.Tasks = []Task{task(1, load.Study, 60, 40), task(2, load.Study, 60, 26)}
	r := Build(in)
	mustValid(t, in, r)
	if !reflect.DeepEqual(r.Deferred, []int64{1}) {
		t.Errorf("deferred = %v", r.Deferred)
	}
	if len(r.Unplaced) != 1 || r.Unplaced[0].ItemID != 2 || r.Unplaced[0].MissingMin != 60 {
		t.Errorf("overdue task must be unplaced in full: %+v", r.Unplaced)
	}
}

func TestShortTaskIsOneChunk(t *testing.T) {
	in := week()
	in.Tasks = []Task{task(1, load.Chores, 20, 30)}
	r := Build(in)
	mustValid(t, in, r)
	if len(r.Placed) != 1 || sumFor(r, 1) != 20 {
		t.Errorf("plan = %+v", r.Placed)
	}
}

func TestNoTinyLeftover(t *testing.T) {
	in := week()
	in.Tasks = []Task{task(1, load.Work, 130, 30)} // 120 + 10 would leave a 10-min crumb
	r := Build(in)
	mustValid(t, in, r)
	for _, c := range r.Placed {
		if c.End.Sub(c.Start) < 30*time.Minute {
			t.Errorf("crumb chunk %v..%v", c.Start, c.End)
		}
	}
}

func TestMidnightAndDST(t *testing.T) {
	// Berlin leaves summer time on Sun 25 Oct 2026: that day has 25 hours.
	berlin, _ := time.LoadLocation("Europe/Berlin")
	in := Input{
		Now: time.Date(2026, 10, 19, 6, 0, 0, 0, berlin), From: time.Date(2026, 10, 19, 0, 0, 0, 0, berlin),
		To: time.Date(2026, 10, 26, 0, 0, 0, 0, berlin), Loc: berlin, P: DefaultParams(), T: load.DefaultThresholds(),
		Tasks: []Task{{ID: 1, Title: "x", Category: load.Work, RemainingMin: 600, Deadline: time.Date(2026, 10, 25, 0, 0, 0, 0, berlin)}},
	}
	r := Build(in)
	mustValid(t, in, r)
	if sumFor(r, 1) != 600 {
		t.Errorf("placed %d", sumFor(r, 1))
	}
}

func TestDeterministic(t *testing.T) {
	in := week()
	in.Busy = []Busy{{Start: at(27, 10, 0), End: at(27, 12, 0), Category: load.Study}}
	in.Tasks = []Task{task(1, load.Study, 300, 30), task(2, load.Work, 200, 29), task(3, load.Hobby, 90, 31)}
	a, b := Build(in), Build(in)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("same input, different plans:\n%+v\n%+v", a, b)
	}
}

func TestValidateCatchesBrokenPlans(t *testing.T) {
	in := week()
	in.Busy = []Busy{{Start: at(26, 10, 0), End: at(26, 12, 0), Category: load.Other}}
	in.Tasks = []Task{task(1, load.Study, 600, 26)}
	cases := []struct {
		name string
		c    []Chunk
	}{
		{"overlaps an event", []Chunk{{1, at(26, 11, 0), at(26, 12, 30)}}},
		{"in sleep", []Chunk{{1, at(26, 23, 0), at(26, 23, 30)}}},
		{"already over", []Chunk{{1, at(26, 7, 0), at(26, 7, 30)}}},
		{"after the deadline", []Chunk{{1, at(27, 9, 0), at(27, 10, 0)}}},
		{"too long", []Chunk{{1, at(26, 12, 0), at(26, 15, 0)}}},
		{"unknown task", []Chunk{{9, at(26, 12, 0), at(26, 13, 0)}}},
		{"no break", []Chunk{{1, at(26, 12, 0), at(26, 13, 0)}, {1, at(26, 13, 5), at(26, 14, 0)}}},
		{"off the grid", []Chunk{{1, at(26, 12, 7), at(26, 13, 0)}}},
		{"more than needed", []Chunk{{1, at(26, 12, 0), at(26, 14, 0)}, {1, at(26, 14, 15), at(26, 16, 15)}, {1, at(26, 16, 30), at(26, 18, 30)}, {1, at(26, 18, 45), at(26, 20, 45)}, {1, at(26, 21, 0), at(26, 22, 30)}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if len(Validate(in, tc.c)) == 0 {
				t.Errorf("plan %+v must be rejected", tc.c)
			}
		})
	}
	if vs := Validate(in, []Chunk{{1, at(26, 12, 0), at(26, 14, 0)}}); len(vs) != 0 {
		t.Errorf("a fine plan was rejected: %+v", vs)
	}
}

// Whatever the input, the plan keeps every rule and never loses minutes.
func TestRandomInputsAlwaysValid(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	cats := []load.Category{load.Study, load.Work, load.Hobby, load.Chores, load.Rest, load.Other}
	for n := 0; n < 300; n++ {
		in := week()
		in.Now = at(26+rng.Intn(5), rng.Intn(24), rng.Intn(60))
		for i := rng.Intn(14); i > 0; i-- {
			s := at(26+rng.Intn(7), rng.Intn(22), []int{0, 15, 20, 45}[rng.Intn(4)])
			in.Busy = append(in.Busy, Busy{Start: s, End: s.Add(time.Duration(30+rng.Intn(240)) * time.Minute), Category: cats[rng.Intn(len(cats))]})
		}
		for i := 0; i < 1+rng.Intn(6); i++ {
			in.Tasks = append(in.Tasks, Task{
				ID: int64(i + 1), Title: "t", Category: cats[rng.Intn(len(cats))],
				RemainingMin: 5 + rng.Intn(900), Deadline: at(26+rng.Intn(8), 0, 0), Priority: rng.Intn(4),
			})
		}
		r := Build(in)
		if vs := Validate(in, r.Placed); len(vs) > 0 {
			t.Fatalf("case %d breaks the rules: %+v\ninput: %+v\nplan: %+v", n, vs, in, r.Placed)
		}
		missing := map[int64]int{}
		for _, u := range r.Unplaced {
			missing[u.ItemID] = u.MissingMin
		}
		deferred := map[int64]bool{}
		for _, id := range r.Deferred {
			deferred[id] = true
		}
		for _, tk := range in.Tasks {
			if deferred[tk.ID] {
				continue
			}
			if got := sumFor(r, tk.ID) + missing[tk.ID]; got != tk.RemainingMin {
				t.Fatalf("case %d task %d: placed+missing = %d, want %d", n, tk.ID, got, tk.RemainingMin)
			}
		}
	}
}

func TestPreferredWindowsGoFirstButAreOnlyAWish(t *testing.T) {
	in := week()
	sport := task(1, load.Hobby, 60, 30)
	// Dry and light on Wednesday 28 only, 14:00-16:00.
	sport.Prefer = []Window{{at(28, 14, 0), at(28, 16, 0)}}
	in.Tasks = []Task{sport}
	r := Build(in)
	mustValid(t, in, r)
	if len(r.Placed) != 1 || !r.Placed[0].Start.Equal(at(28, 14, 0)) {
		t.Fatalf("the preferred window is later but wins: %+v", r.Placed)
	}

	// Busy there: the wish is dropped, not the task.
	in.Busy = []Busy{{Start: at(28, 13, 0), End: at(28, 17, 0), Category: load.Other}}
	r = Build(in)
	mustValid(t, in, r)
	if len(r.Placed) != 1 || !r.Placed[0].Start.Equal(at(26, 8, 0)) || len(r.Unplaced) != 0 {
		t.Fatalf("fallback to the earliest window: %+v", r.Placed)
	}

	// A preferred window that is too short for a chunk is no help.
	in.Busy = nil
	sport.Prefer = []Window{{at(28, 14, 0), at(28, 14, 20)}}
	in.Tasks = []Task{sport}
	r = Build(in)
	mustValid(t, in, r)
	if !r.Placed[0].Start.Equal(at(26, 8, 0)) {
		t.Errorf("a 20-minute window must not take a 60-minute task: %+v", r.Placed)
	}
}
