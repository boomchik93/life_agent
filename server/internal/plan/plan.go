// Package plan lays open tasks into the free windows of a week (PLAN.md
// section 4). It is pure code: no I/O, no clock, no LLM. One input always
// gives one plan, and Validate checks any plan against the same rules.
package plan

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"lifeagent/internal/load"
)

// Params are the planner settings of PLAN.md section 14. Stored in settings
// under "plan_params"; a stored object overrides only the fields it names.
type Params struct {
	// Sleep window: the planner never places anything in it. It may wrap midnight.
	SleepStartHour int `json:"sleep_start_hour"`
	SleepEndHour   int `json:"sleep_end_hour"`
	// A chunk of a task is ChunkMinMin..ChunkMaxMin long.
	ChunkMinMin int `json:"chunk_min_min"`
	ChunkMaxMin int `json:"chunk_max_min"`
}

func DefaultParams() Params {
	return Params{SleepStartHour: 23, SleepEndHour: 7, ChunkMinMin: 30, ChunkMaxMin: 120}
}

func ParseParams(raw []byte) (Params, error) {
	p := DefaultParams()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return Params{}, err
	}
	return p, p.Validate()
}

func (p Params) Validate() error {
	switch {
	case p.SleepStartHour < 0 || p.SleepStartHour > 23 || p.SleepEndHour < 0 || p.SleepEndHour > 23:
		return errors.New("sleep hours must be 0..23")
	case p.SleepStartHour == p.SleepEndHour:
		return errors.New("sleep window must not be empty")
	case p.ChunkMinMin < 5 || p.ChunkMaxMin < p.ChunkMinMin || p.ChunkMaxMin > 24*60:
		return errors.New("chunks: need 5 <= chunk_min_min <= chunk_max_min <= 1440")
	}
	return nil
}

// Task is an open task. RemainingMin is what is still to place.
type Task struct {
	ID           int64
	Title        string
	Category     load.Category
	RemainingMin int
	// Deadline is the last day the work may happen on (local midnight).
	Deadline time.Time
	Priority int // higher goes first
	// Prefer lists periods the chunks should go to if they can (dry, light
	// hours for sport). It is only a wish: when nothing fits there, any free
	// window is used. The rules apply to every window alike.
	Prefer []Window
}

// Window is a period of time.
type Window struct{ Start, End time.Time }

// Busy is a timed calendar event; all-day events do not block the planner.
type Busy struct {
	Start, End time.Time
	Category   load.Category
}

type Input struct {
	// Now is the earliest moment a chunk may start; From..To is the week.
	Now      time.Time
	From, To time.Time
	Loc      *time.Location
	Busy     []Busy
	Tasks    []Task
	P        Params
	T        load.Thresholds
}

type Chunk struct {
	ItemID     int64
	Start, End time.Time
}

type Unplaced struct {
	ItemID     int64 `json:"item_id"`
	MissingMin int   `json:"missing_min"`
}

type Result struct {
	Placed   []Chunk
	Unplaced []Unplaced
	// Deferred are tasks due after this week: they wait for their own week.
	Deferred []int64
}

type Violation struct {
	ItemID  int64  `json:"item_id,omitempty"`
	Message string `json:"message"`
}

type span struct{ s, e time.Time }

func (p span) mins() int { return int(p.e.Sub(p.s) / time.Minute) }

// ceil15 rounds up to a quarter of an hour on the local clock.
func ceil15(t time.Time) time.Time {
	if cut := t.Truncate(time.Minute); cut.Before(t) {
		t = cut.Add(time.Minute)
	}
	for t.Minute()%15 != 0 {
		t = t.Add(time.Minute)
	}
	return t
}

func floor15(t time.Time) time.Time {
	t = t.Truncate(time.Minute)
	for t.Minute()%15 != 0 {
		t = t.Add(-time.Minute)
	}
	return t
}

func startOfDay(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

func addDays(d time.Time, n int, loc *time.Location) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day()+n, 0, 0, 0, 0, loc)
}

func hourAt(d time.Time, h int, loc *time.Location) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), h, 0, 0, 0, loc)
}

func daysOf(from, to time.Time, loc *time.Location) []time.Time {
	var out []time.Time
	for d := startOfDay(from, loc); d.Before(to); d = addDays(d, 1, loc) {
		out = append(out, d)
	}
	return out
}

// subtract removes the busy spans from the windows.
func subtract(windows []span, busy []span) []span {
	for _, b := range busy {
		var next []span
		for _, w := range windows {
			if !b.e.After(w.s) || !b.s.Before(w.e) {
				next = append(next, w)
				continue
			}
			if b.s.After(w.s) {
				next = append(next, span{w.s, b.s})
			}
			if b.e.Before(w.e) {
				next = append(next, span{b.e, w.e})
			}
		}
		windows = next
	}
	return windows
}

// awake returns the waking hours of every day of the week, as one window per day.
func (in Input) awake() []span {
	var out []span
	for _, d := range daysOf(in.From, in.To, in.Loc) {
		day := span{d, addDays(d, 1, in.Loc)}
		var sleep []span
		if in.P.SleepStartHour > in.P.SleepEndHour {
			sleep = []span{{d, hourAt(d, in.P.SleepEndHour, in.Loc)}, {hourAt(d, in.P.SleepStartHour, in.Loc), day.e}}
		} else {
			sleep = []span{{hourAt(d, in.P.SleepStartHour, in.Loc), hourAt(d, in.P.SleepEndHour, in.Loc)}}
		}
		out = append(out, subtract([]span{day}, sleep)...)
	}
	return out
}

func (in Input) start() time.Time {
	lo := ceil15(in.Now)
	if lo.Before(in.From) {
		lo = in.From
	}
	return lo
}

func (in Input) timedBusy() []span {
	var out []span
	for _, b := range in.Busy {
		if b.End.After(b.Start) {
			out = append(out, span{b.Start, b.End})
		}
	}
	return out
}

func touches(sp []span, from, to time.Time) bool {
	for _, p := range sp {
		if p.e.After(from) && p.s.Before(to) {
			return true
		}
	}
	return false
}

// evening is the part of day d from EveningStartHour to midnight.
func (in Input) evening(d time.Time) span {
	return span{hourAt(d, in.T.EveningStartHour, in.Loc), addDays(d, 1, in.Loc)}
}

// evenings counts the evenings of the week free of events and returns the
// number the plan has to keep free (FreeEveningsMin, or fewer if the week
// has fewer to begin with) and the evenings it protects to do that.
func (in Input) evenings(busy []span) (need int, protected []span) {
	lo := in.start()
	free, plannable := 0, []span{}
	for _, d := range daysOf(in.From, in.To, in.Loc) {
		ev := in.evening(d)
		if touches(busy, ev.s, ev.e) {
			continue
		}
		free++
		if ev.e.After(lo) {
			plannable = append(plannable, ev)
		}
	}
	need = min(in.T.FreeEveningsMin, free)
	// Evenings that are already over stay free by themselves; the rest is
	// protected, latest first, so the weekend evenings stay clear.
	fixed := free - len(plannable)
	for i := len(plannable) - 1; i >= 0 && fixed+len(protected) < need; i-- {
		protected = append(protected, plannable[i])
	}
	return need, protected
}

type planner struct {
	in        Input
	busy      []span // every timed event and every placed chunk
	study     []span // study events and study chunks
	protected []span
	chunks    []Chunk
}

func Build(in Input) Result {
	pl := &planner{in: in}
	pl.busy = in.timedBusy()
	for _, b := range in.Busy {
		if b.Category == load.Study && b.End.After(b.Start) {
			pl.study = append(pl.study, span{b.Start, b.End})
		}
	}
	_, pl.protected = in.evenings(pl.busy)

	tasks := append([]Task(nil), in.Tasks...)
	sort.SliceStable(tasks, func(i, j int) bool {
		a, b := tasks[i], tasks[j]
		if !a.Deadline.Equal(b.Deadline) {
			return a.Deadline.Before(b.Deadline)
		}
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		return a.ID < b.ID
	})

	var res Result
	for _, t := range tasks {
		if t.RemainingMin <= 0 {
			continue
		}
		end := addDays(t.Deadline, 1, in.Loc)
		if end.After(in.To) {
			res.Deferred = append(res.Deferred, t.ID)
			continue
		}
		missing := pl.place(t, end)
		if missing > 0 {
			res.Unplaced = append(res.Unplaced, Unplaced{ItemID: t.ID, MissingMin: missing})
		}
	}
	res.Placed = pl.chunks
	sort.SliceStable(res.Placed, func(i, j int) bool { return res.Placed[i].Start.Before(res.Placed[j].Start) })
	return res
}

// place puts chunks of the task into the earliest windows before end and
// returns the minutes that did not fit.
func (pl *planner) place(t Task, end time.Time) int {
	remaining := t.RemainingMin
	for remaining > 0 {
		placed := false
		for _, w := range pl.ordered(t, pl.windows(t, end)) {
			size := pl.chunkSize(t, w, remaining)
			if size <= 0 {
				continue
			}
			c := Chunk{ItemID: t.ID, Start: w.s, End: w.s.Add(time.Duration(size) * time.Minute)}
			pl.chunks = append(pl.chunks, c)
			pl.busy = append(pl.busy, span{c.Start, c.End})
			if t.Category == load.Study {
				pl.study = append(pl.study, span{c.Start, c.End})
			}
			remaining -= size
			placed = true
			break
		}
		if !placed {
			break
		}
	}
	return remaining
}

// windows lists the free windows the task may use, earliest first, on the
// quarter-hour grid. Study keeps a break around every other study block.
func (pl *planner) windows(t Task, end time.Time) []span {
	in := pl.in
	lo, hi := in.start(), end
	var ws []span
	for _, a := range in.awake() {
		if a.s.Before(lo) {
			a.s = lo
		}
		if a.e.After(hi) {
			a.e = hi
		}
		if a.e.After(a.s) {
			ws = append(ws, a)
		}
	}
	ws = subtract(ws, pl.protected)
	ws = subtract(ws, pl.busy)
	if t.Category == load.Study {
		gap := time.Duration(in.T.BreakMin) * time.Minute
		var padded []span
		for _, s := range pl.study {
			padded = append(padded, span{s.s.Add(-gap), s.e.Add(gap)})
		}
		ws = subtract(ws, padded)
	}
	var out []span
	for _, w := range ws {
		w.s, w.e = ceil15(w.s), floor15(w.e)
		if w.e.After(w.s) {
			out = append(out, w)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].s.Before(out[j].s) })
	return out
}

// ordered puts the preferred parts of the free windows first, then all the
// windows. Without a preference it returns the windows as they are.
func (pl *planner) ordered(t Task, ws []span) []span {
	if len(t.Prefer) == 0 {
		return ws
	}
	var pref []span
	for _, w := range ws {
		for _, p := range t.Prefer {
			s, e := ceil15(maxT(w.s, p.Start)), floor15(minT(w.e, p.End))
			if e.After(s) {
				pref = append(pref, span{s, e})
			}
		}
	}
	sort.SliceStable(pref, func(i, j int) bool { return pref[i].s.Before(pref[j].s) })
	return append(pref, ws...)
}

func maxT(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minT(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// chunkSize is the length of the next chunk at the start of window w, or 0
// when no chunk fits there.
func (pl *planner) chunkSize(t Task, w span, remaining int) int {
	p, th := pl.in.P, pl.in.T
	avail := min(w.mins(), p.ChunkMaxMin)
	if t.Category == load.Study {
		avail = min(avail, int(th.StudyStreakYellowAbove*60))
		day := startOfDay(w.s, pl.in.Loc)
		avail = min(avail, int(th.StudyDayYellowAbove*60)-studyMins(pl.study, day, addDays(day, 1, pl.in.Loc)))
		avail = min(avail, int(th.StudyWeekYellowAbove*60)-studyMins(pl.study, pl.in.From, pl.in.To))
	}
	avail -= avail % 5
	if avail <= 0 {
		return 0
	}
	if avail >= remaining {
		return remaining
	}
	size := avail
	if remaining-size < p.ChunkMinMin {
		size = remaining - p.ChunkMinMin
	}
	if size < p.ChunkMinMin {
		return 0
	}
	return size
}

// studyMins is the study time inside [from, to), overlaps counted once.
func studyMins(study []span, from, to time.Time) int {
	merged := mergeSpans(study, 0)
	n := 0
	for _, s := range merged {
		if s.s.Before(from) {
			s.s = from
		}
		if s.e.After(to) {
			s.e = to
		}
		if s.e.After(s.s) {
			n += s.mins()
		}
	}
	return n
}

// mergeSpans joins spans that overlap or lie closer than gap.
func mergeSpans(in []span, gap time.Duration) []span {
	if len(in) == 0 {
		return nil
	}
	sp := append([]span(nil), in...)
	sort.Slice(sp, func(i, j int) bool { return sp[i].s.Before(sp[j].s) })
	out := []span{sp[0]}
	for _, p := range sp[1:] {
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

// Validate checks a plan against the rules of PLAN.md sections 4 and 14 and
// returns every violation; an empty result means the plan is allowed. It does
// not depend on how the plan was made, so it also guards plans sent by the app.
func Validate(in Input, placed []Chunk) []Violation {
	var vs []Violation
	bad := func(id int64, format string, a ...any) {
		vs = append(vs, Violation{ItemID: id, Message: fmt.Sprintf(format, a...)})
	}
	tasks := map[int64]Task{}
	for _, t := range in.Tasks {
		tasks[t.ID] = t
	}
	awake := in.awake()
	lo := in.start()
	label := func(t time.Time) string { return t.In(in.Loc).Format("02.01 15:04") }

	perTask := map[int64][]Chunk{}
	var studyChunks []span
	for _, c := range placed {
		t, ok := tasks[c.ItemID]
		if !ok {
			bad(c.ItemID, "задача %d не найдена среди открытых", c.ItemID)
			continue
		}
		perTask[c.ItemID] = append(perTask[c.ItemID], c)
		mins := int(c.End.Sub(c.Start) / time.Minute)
		in15 := c.Start.Equal(ceil15(c.Start))
		switch {
		case !c.End.After(c.Start):
			bad(t.ID, "«%s»: конец раньше начала", t.Title)
			continue
		case c.Start.Before(lo):
			bad(t.ID, "«%s»: %s уже прошло", t.Title, label(c.Start))
		case c.End.After(addDays(t.Deadline, 1, in.Loc)) || c.End.After(in.To):
			bad(t.ID, "«%s»: %s позже дедлайна или конца недели", t.Title, label(c.Start))
		case !insideAny(awake, c.Start, c.End):
			bad(t.ID, "«%s»: %s попадает в окно сна", t.Title, label(c.Start))
		case !in15:
			bad(t.ID, "«%s»: начало %s не кратно 15 минутам", t.Title, label(c.Start))
		}
		if mins > in.P.ChunkMaxMin {
			bad(t.ID, "«%s»: кусок %d мин длиннее %d", t.Title, mins, in.P.ChunkMaxMin)
		}
		if t.Category == load.Study {
			studyChunks = append(studyChunks, span{c.Start, c.End})
		}
	}

	for id, cs := range perTask {
		t := tasks[id]
		sum := 0
		for _, c := range cs {
			m := int(c.End.Sub(c.Start) / time.Minute)
			sum += m
			if m < in.P.ChunkMinMin && len(cs) > 1 {
				bad(id, "«%s»: кусок %d мин короче %d", t.Title, m, in.P.ChunkMinMin)
			}
		}
		if sum > t.RemainingMin {
			bad(id, "«%s»: в плане %d мин, нужно %d", t.Title, sum, t.RemainingMin)
		}
	}

	// Overlaps with events and between chunks.
	busy := in.timedBusy()
	sorted := append([]Chunk(nil), placed...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Start.Before(sorted[j].Start) })
	for i, c := range sorted {
		if touches(busy, c.Start, c.End) {
			bad(c.ItemID, "«%s»: %s пересекается с событием календаря", tasks[c.ItemID].Title, label(c.Start))
		}
		if i > 0 && c.Start.Before(sorted[i-1].End) {
			bad(c.ItemID, "«%s»: %s пересекается с другим куском плана", tasks[c.ItemID].Title, label(c.Start))
		}
	}

	vs = append(vs, in.validateStudy(studyChunks)...)

	// Free evenings.
	need, _ := in.evenings(busy)
	all := append([]span(nil), busy...)
	for _, c := range placed {
		all = append(all, span{c.Start, c.End})
	}
	free := 0
	for _, d := range daysOf(in.From, in.To, in.Loc) {
		ev := in.evening(d)
		if !touches(all, ev.s, ev.e) {
			free++
		}
	}
	if free < need {
		bad(0, "свободных вечеров %d, нужно хотя бы %d", free, need)
	}

	sort.SliceStable(vs, func(i, j int) bool {
		if vs[i].ItemID != vs[j].ItemID {
			return vs[i].ItemID < vs[j].ItemID
		}
		return vs[i].Message < vs[j].Message
	})
	return vs
}

func insideAny(windows []span, s, e time.Time) bool {
	for _, w := range windows {
		if !s.Before(w.s) && !e.After(w.e) {
			return true
		}
	}
	return false
}

// validateStudy checks the study limits and breaks wherever the plan adds study time.
func (in Input) validateStudy(chunks []span) []Violation {
	if len(chunks) == 0 {
		return nil
	}
	var vs []Violation
	bad := func(format string, a ...any) { vs = append(vs, Violation{Message: fmt.Sprintf(format, a...)}) }
	var existing []span
	for _, b := range in.Busy {
		if b.Category == load.Study && b.End.After(b.Start) {
			existing = append(existing, span{b.Start, b.End})
		}
	}
	all := append(append([]span(nil), existing...), chunks...)

	if h := float64(studyMins(all, in.From, in.To)) / 60; h > in.T.StudyWeekYellowAbove {
		bad("учёба за неделю %.1f ч, предел %.1f ч", h, in.T.StudyWeekYellowAbove)
	}
	for _, d := range daysOf(in.From, in.To, in.Loc) {
		next := addDays(d, 1, in.Loc)
		if studyMins(chunks, d, next) == 0 {
			continue
		}
		if h := float64(studyMins(all, d, next)) / 60; h > in.T.StudyDayYellowAbove {
			bad("учёба %s %.1f ч, предел %.1f ч", d.Format("02.01"), h, in.T.StudyDayYellowAbove)
		}
	}

	gap := time.Duration(in.T.BreakMin) * time.Minute
	for _, m := range mergeSpans(all, gap) {
		if !touchesAny(chunks, m) {
			continue
		}
		if h := m.e.Sub(m.s).Hours(); h > in.T.StudyStreakYellowAbove {
			bad("учёба подряд %.1f ч с %s, предел %.1f ч", h, m.s.In(in.Loc).Format("02.01 15:04"), in.T.StudyStreakYellowAbove)
		}
	}
	// A break of BreakMin must separate a new study chunk from any other study block.
	sort.Slice(all, func(i, j int) bool { return all[i].s.Before(all[j].s) })
	isChunk := func(p span) bool {
		for _, c := range chunks {
			if c.s.Equal(p.s) && c.e.Equal(p.e) {
				return true
			}
		}
		return false
	}
	for i := 1; i < len(all); i++ {
		a, b := all[i-1], all[i]
		if b.s.Sub(a.e) < gap && (isChunk(a) || isChunk(b)) {
			bad("между учебными блоками около %s нет перерыва в %d мин", b.s.In(in.Loc).Format("02.01 15:04"), in.T.BreakMin)
		}
	}
	return vs
}

func touchesAny(chunks []span, m span) bool {
	for _, c := range chunks {
		if c.e.After(m.s) && c.s.Before(m.e) {
			return true
		}
	}
	return false
}
