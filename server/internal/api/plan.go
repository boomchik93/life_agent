package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"lifeagent/internal/calendar"
	"lifeagent/internal/load"
	"lifeagent/internal/plan"
	"lifeagent/internal/store"
)

const settingPlanParams = "plan_params"

type itemView struct {
	store.Item
	// PlannedMin: minutes of the task already written to the calendar by the planner.
	PlannedMin int `json:"planned_min"`
}

// getItems lists items; the planner's screen needs ?kind=task&status=open.
func (s *Server) getItems(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	items, err := s.store.Items(q.Get("kind"), q.Get("status"))
	if err != nil {
		s.fail(w, err)
		return
	}
	planned, err := s.store.PlannedMinutes("")
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]itemView, len(items))
	for i, it := range items {
		out[i] = itemView{Item: it, PlannedMin: planned[it.ID]}
	}
	writeJSON(w, http.StatusOK, out)
}

type itemPatchRequest struct {
	Status      *string `json:"status"`
	DurationMin *int    `json:"duration_min"`
	Deadline    *string `json:"deadline"`
	Priority    *int    `json:"priority"`
}

func (s *Server) patchItem(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "body too large")
		return
	}
	var in itemPatchRequest
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "body must be a JSON object")
		return
	}
	switch {
	case in.Status != nil && *in.Status != "confirmed" && *in.Status != "done":
		writeError(w, http.StatusBadRequest, "status must be confirmed or done")
		return
	case in.DurationMin != nil && (*in.DurationMin < 5 || *in.DurationMin > 7*24*60):
		writeError(w, http.StatusBadRequest, "Длительность: от 5 минут до недели.")
		return
	case in.Priority != nil && (*in.Priority < 0 || *in.Priority > 3):
		writeError(w, http.StatusBadRequest, "priority must be 0..3")
		return
	}
	if in.Deadline != nil {
		if _, err := time.ParseInLocation("2006-01-02", *in.Deadline, s.loc); err != nil {
			writeError(w, http.StatusBadRequest, "deadline must be YYYY-MM-DD")
			return
		}
	}
	cur, err := s.store.ItemByID(id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "item not found")
		return
	} else if err != nil {
		s.fail(w, err)
		return
	}
	if cur.Kind != "task" {
		writeError(w, http.StatusBadRequest, "only tasks can be changed")
		return
	}
	saved, err := s.store.PatchItem(id, store.ItemPatch{
		Status: in.Status, DurationMin: in.DurationMin, Deadline: in.Deadline, Priority: in.Priority,
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	planned, err := s.store.PlannedMinutes("")
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, itemView{Item: saved, PlannedMin: planned[id]})
}

// planContext is everything the planner and its validator need for one week.
type planContext struct {
	in    plan.Input
	items map[int64]store.Item
	cats  load.CalendarCategories
	// events are the calendar events of the week, for the risk colours.
	events []calendar.Event
	// goals are the goal blocks of this plan (negative ids), skipped the goals
	// that were left out because of a red risk.
	goals     map[int64]goalMeta
	skipped   []skippedGoal
	exceptKey string
}

var errCalendars = errors.New("calendars unavailable")

// planContext reads the calendars and the open tasks. Events whose id is in
// ownIDs are left out: they are the pieces of the request being retried.
// The planner refuses to work on a partial calendar: a source that failed may
// hide the very event a chunk would land on.
func (s *Server) planContext(ctx context.Context, day time.Time, exceptKey string, ownIDs map[string]bool, withGoals bool) (planContext, error) {
	pc := planContext{items: map[int64]store.Item{}, exceptKey: exceptKey}
	params, th, cats, err := s.planSettings()
	if err != nil {
		return pc, err
	}
	pc.cats = cats
	from, to := calendar.WeekBounds(day, s.loc)
	events, warnings := s.cal.Collect(ctx, from, to)
	if len(warnings) > 0 {
		return pc, fmt.Errorf("%w: %s", errCalendars, warnings[0])
	}

	in := plan.Input{Now: s.now(), From: from, To: to, Loc: s.loc, P: params, T: th}
	for _, e := range events {
		if ownIDs[e.ID] {
			continue
		}
		pc.events = append(pc.events, e)
		if e.AllDay || !e.End.After(e.Start) {
			continue
		}
		in.Busy = append(in.Busy, plan.Busy{Start: e.Start, End: e.End, Category: eventCategory(cats, e)})
	}

	tasks, err := s.store.Items("task", "open")
	if err != nil {
		return pc, err
	}
	planned, err := s.store.PlannedMinutes(exceptKey)
	if err != nil {
		return pc, err
	}
	for _, it := range tasks {
		pc.items[it.ID] = it
		if it.DurationMin == nil || it.Deadline == nil || !load.Category(it.Category).Valid() {
			continue
		}
		deadline, err := time.ParseInLocation("2006-01-02", *it.Deadline, s.loc)
		if err != nil {
			continue
		}
		in.Tasks = append(in.Tasks, plan.Task{
			ID: it.ID, Title: it.Title, Category: load.Category(it.Category),
			RemainingMin: *it.DurationMin - planned[it.ID], Deadline: deadline, Priority: it.Priority,
		})
	}
	pc.in = in
	if withGoals {
		extra, err := s.goalTasks(&pc)
		if err != nil {
			return pc, err
		}
		pc.in.Tasks = append(pc.in.Tasks, extra...)
	}
	return pc, nil
}

func (s *Server) planSettings() (plan.Params, load.Thresholds, load.CalendarCategories, error) {
	th, cats, err := s.loadSettings()
	if err != nil {
		return plan.Params{}, th, cats, err
	}
	params := plan.DefaultParams()
	kv, err := s.store.Settings()
	if err != nil {
		return params, th, cats, err
	}
	if raw, ok := kv[settingPlanParams]; ok {
		if params, err = plan.ParseParams([]byte(raw)); err != nil {
			return params, th, cats, err
		}
	}
	return params, th, cats, nil
}

type chunkView struct {
	ItemID   int64         `json:"item_id"`
	Title    string        `json:"title"`
	Category load.Category `json:"category"`
	Start    time.Time     `json:"start"`
	End      time.Time     `json:"end"`
}

type unplacedView struct {
	ItemID     int64  `json:"item_id"`
	Title      string `json:"title"`
	MissingMin int    `json:"missing_min"`
}

type deferredView struct {
	ItemID   int64  `json:"item_id"`
	Title    string `json:"title"`
	Deadline string `json:"deadline"`
}

type planResponse struct {
	From       time.Time       `json:"from"`
	To         time.Time       `json:"to"`
	Placed     []chunkView     `json:"placed"`
	Unplaced   []unplacedView  `json:"unplaced"`
	Deferred   []deferredView  `json:"deferred"`
	Before     load.Assessment `json:"before"`
	After      load.Assessment `json:"after"`
	Violations []string        `json:"violations"`
	// SkippedGoals: goals left out of a goals plan because of a red risk.
	SkippedGoals []skippedGoal `json:"skipped_goals"`
}

func (s *Server) chunkViews(pc planContext, cs []plan.Chunk) []chunkView {
	out := make([]chunkView, 0, len(cs))
	for _, c := range cs {
		title, cat := pc.info(c.ItemID)
		out = append(out, chunkView{ItemID: c.ItemID, Title: title, Category: load.Category(cat),
			Start: c.Start.In(s.loc), End: c.End.In(s.loc)})
	}
	return out
}

// risksWith assesses the week with the plan's chunks added as events.
func (s *Server) risksWith(pc planContext, cs []plan.Chunk) (before, after load.Assessment, err error) {
	if before, err = s.assess(pc.events, pc.in.From, pc.in.To); err != nil {
		return
	}
	with := append([]calendar.Event(nil), pc.events...)
	for _, c := range cs {
		title, cat := pc.info(c.ItemID)
		with = append(with, calendar.Event{Source: "google", Title: title, Start: c.Start, End: c.End, Category: cat})
	}
	after, err = s.assess(with, pc.in.From, pc.in.To)
	return
}

func (s *Server) planFail(w http.ResponseWriter, err error) {
	if errors.Is(err, errCalendars) {
		s.logger.Error("plan failed", "err", err)
		writeCode(w, http.StatusBadGateway, "calendars_unavailable",
			"Не удалось прочитать все календари, поэтому неделю собрать нельзя: план мог бы лечь поверх занятого времени. Попробуй позже.")
		return
	}
	s.fail(w, err)
}

// weekDay reads the optional {"start":"YYYY-MM-DD","goals":true} of a plan
// request: the week, and whether the missing minutes of the goals are planned too.
func (s *Server) weekDay(w http.ResponseWriter, r *http.Request) (day time.Time, goals, ok bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "body too large")
		return time.Time{}, false, false
	}
	day = s.now()
	if len(strings.TrimSpace(string(body))) == 0 {
		return day, false, true
	}
	var in struct {
		Start string `json:"start"`
		Goals bool   `json:"goals"`
	}
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "body must be a JSON object")
		return time.Time{}, false, false
	}
	if in.Start != "" {
		t, err := time.ParseInLocation("2006-01-02", in.Start, s.loc)
		if err != nil {
			writeError(w, http.StatusBadRequest, "start must be YYYY-MM-DD")
			return time.Time{}, false, false
		}
		day = t
	}
	return day, in.Goals, true
}

// postPlanWeek builds the plan and shows it. Nothing is written anywhere.
func (s *Server) postPlanWeek(w http.ResponseWriter, r *http.Request) {
	if s.cal == nil {
		writeError(w, http.StatusServiceUnavailable, "calendars are not configured")
		return
	}
	day, withGoals, ok := s.weekDay(w, r)
	if !ok {
		return
	}
	pc, err := s.planContext(r.Context(), day, "", nil, withGoals)
	if err != nil {
		s.planFail(w, err)
		return
	}
	res := plan.Build(pc.in)

	out := planResponse{From: pc.in.From, To: pc.in.To, Placed: s.chunkViews(pc, res.Placed),
		Unplaced: []unplacedView{}, Deferred: []deferredView{}, Violations: []string{}, SkippedGoals: []skippedGoal{}}
	out.SkippedGoals = append(out.SkippedGoals, pc.skipped...)
	for _, u := range res.Unplaced {
		title, _ := pc.info(u.ItemID)
		out.Unplaced = append(out.Unplaced, unplacedView{ItemID: u.ItemID, Title: title, MissingMin: u.MissingMin})
	}
	for _, id := range res.Deferred {
		it := pc.items[id]
		out.Deferred = append(out.Deferred, deferredView{ItemID: id, Title: it.Title, Deadline: *it.Deadline})
	}
	// The same validator as in the tests: a plan that breaks a rule is never shown as fine.
	for _, v := range plan.Validate(pc.in, res.Placed) {
		out.Violations = append(out.Violations, v.Message)
	}
	if out.Before, out.After, err = s.risksWith(pc, res.Placed); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type applyRequest struct {
	// Goals: the plan holds goal blocks (negative item ids), so the server
	// checks it against the goals as well as the tasks.
	Goals  bool `json:"goals"`
	Placed []struct {
		ItemID int64  `json:"item_id"`
		Start  string `json:"start"`
		End    string `json:"end"`
	} `json:"placed"`
}

type applyResponse struct {
	Applied   []chunkView     `json:"applied"`
	Duplicate bool            `json:"duplicate"`
	After     load.Assessment `json:"after"`
}

func chunkEventID(key string, seq int) string {
	return calendar.EventID(fmt.Sprintf("%s#%d", key, seq))
}

// postPlanApply writes a plan the user confirmed. The server checks the plan
// again against the calendar as it is now; a stale plan is refused, not written.
func (s *Server) postPlanApply(w http.ResponseWriter, r *http.Request) {
	key, ok := idempotencyKey(w, r)
	if !ok {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "body too large")
		return
	}
	var in applyRequest
	if err := json.Unmarshal(body, &in); err != nil || len(in.Placed) == 0 {
		writeError(w, http.StatusBadRequest, "placed must be a non-empty list")
		return
	}
	if len(in.Placed) > 200 {
		writeError(w, http.StatusBadRequest, "too many chunks")
		return
	}
	chunks := make([]plan.Chunk, 0, len(in.Placed))
	for _, p := range in.Placed {
		st, err1 := s.parseMoment(p.Start)
		en, err2 := s.parseMoment(p.End)
		if err1 != nil || err2 != nil {
			writeError(w, http.StatusBadRequest, "start and end must be RFC 3339 times")
			return
		}
		chunks = append(chunks, plan.Chunk{ItemID: p.ItemID, Start: st.In(s.loc), End: en.In(s.loc)})
	}
	sort.SliceStable(chunks, func(i, j int) bool {
		if !chunks[i].Start.Equal(chunks[j].Start) {
			return chunks[i].Start.Before(chunks[j].Start)
		}
		return chunks[i].ItemID < chunks[j].ItemID
	})

	// The pieces of this request that are in the calendar already.
	done, err := s.store.ChunksByKey(key)
	if err != nil {
		s.fail(w, err)
		return
	}
	if len(done) == len(chunks) && len(done) > 0 {
		s.respondApplied(w, r, chunks, true, in.Goals)
		return
	}
	if s.cal == nil || s.writer == nil {
		writeCode(w, http.StatusServiceUnavailable, "write_not_configured",
			"На сервере не настроен календарь для записи.")
		return
	}

	own := map[string]bool{}
	for i := range chunks {
		own[chunkEventID(key, i)] = true
	}
	pc, err := s.planContext(r.Context(), chunks[0].Start, key, own, in.Goals)
	if err != nil {
		s.planFail(w, err)
		return
	}
	for _, c := range chunks {
		if !c.Start.Before(pc.in.To) || c.Start.Before(pc.in.From) || c.End.After(pc.in.To) {
			writeError(w, http.StatusBadRequest, "все куски плана должны лежать в одной неделе")
			return
		}
	}
	if vs := plan.Validate(pc.in, chunks); len(vs) > 0 {
		msgs := make([]string, len(vs))
		for i, v := range vs {
			msgs[i] = v.Message
		}
		writeJSON(w, http.StatusConflict, map[string]any{
			"code": "plan_stale", "violations": msgs,
			"error": "План больше не подходит: календарь или задачи изменились. Собери неделю заново.",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), writeTimeout)
	defer cancel()
	for i, c := range chunks {
		title, cat := pc.info(c.ItemID)
		ne := calendar.NewEvent{ID: chunkEventID(key, i), Title: title, Category: cat, Start: c.Start, End: c.End}
		if _, err := s.writer.Insert(ctx, ne); err != nil {
			if errors.Is(err, calendar.ErrNoWriteAccess) {
				writeCode(w, http.StatusBadGateway, "no_write_access",
					"У сервера нет права записи в этот календарь. Дай сервисному аккаунту доступ «Вносить изменения в мероприятия».")
				return
			}
			s.logger.Error("calendar insert failed", "err", err)
			writeCode(w, http.StatusBadGateway, "calendar_error",
				"Календарь принял не всё. Нажми «Применить» ещё раз: уже записанное второй раз не запишется.")
			return
		}
		rec := store.PlanChunk{ItemID: c.ItemID, Goal: goalOf(c.ItemID), ApplyKey: key, Seq: i, EventID: ne.ID,
			Start: c.Start.UTC().Format(time.RFC3339), End: c.End.UTC().Format(time.RFC3339), Minutes: int(c.End.Sub(c.Start) / time.Minute)}
		if err := s.store.AddChunk(rec); err != nil {
			s.fail(w, err)
			return
		}
	}

	// A task that is fully planned is marked written; the rest stays open.
	planned, err := s.store.PlannedMinutes("")
	if err != nil {
		s.fail(w, err)
		return
	}
	written := "written"
	for id := range planned {
		it, ok := pc.items[id]
		if ok && it.DurationMin != nil && planned[id] >= *it.DurationMin && it.Status != "written" {
			if _, err := s.store.PatchItem(id, store.ItemPatch{Status: &written}); err != nil {
				s.fail(w, err)
				return
			}
		}
	}
	s.respondApplied(w, r, chunks, false, in.Goals)
}

func (s *Server) respondApplied(w http.ResponseWriter, r *http.Request, chunks []plan.Chunk, dup, goals bool) {
	pc, err := s.planContext(r.Context(), chunks[0].Start, "", nil, goals)
	out := applyResponse{Applied: []chunkView{}, Duplicate: dup}
	if err == nil {
		out.Applied = s.chunkViews(pc, chunks)
		// The written pieces are in the calendar now, so the week already includes them.
		out.After, err = s.assess(pc.events, pc.in.From, pc.in.To)
	}
	if err != nil {
		// The plan is written; only the colours are missing.
		s.logger.Error("apply: risks failed", "err", err)
		out.Applied = s.titles(chunks)
	}
	writeJSON(w, http.StatusOK, out)
}

// titles builds chunk views from the store when the calendar is not readable.
func (s *Server) titles(chunks []plan.Chunk) []chunkView {
	out := make([]chunkView, 0, len(chunks))
	for _, c := range chunks {
		v := chunkView{ItemID: c.ItemID, Start: c.Start.In(s.loc), End: c.End.In(s.loc)}
		if m, ok := staticGoal(c.ItemID); ok {
			v.Title, v.Category = m.Title, m.Category
		} else if it, err := s.store.ItemByID(c.ItemID); err == nil {
			v.Title, v.Category = it.Title, load.Category(it.Category)
		}
		out = append(out, v)
	}
	return out
}
