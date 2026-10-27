package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ErrNotFound means there is no row with the given id.
var ErrNotFound = errors.New("store: not found")

// Items lists items of one kind (empty: all) in creation order. status "open"
// means not done; any other non-empty value is matched exactly.
func (s *Store) Items(kind, status string) ([]Item, error) {
	var where []string
	var args []any
	if kind != "" {
		where = append(where, "kind = ?")
		args = append(args, kind)
	}
	switch status {
	case "":
	case "open":
		where = append(where, "status IN ('confirmed', 'written')")
	default:
		where = append(where, "status = ?")
		args = append(args, status)
	}
	q := `SELECT ` + itemCols + ` FROM items`
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, " AND ")
	}
	rows, err := s.DB.Query(q+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *Store) ItemByID(id int64) (Item, error) {
	it, err := scanItem(s.DB.QueryRow(`SELECT `+itemCols+` FROM items WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return Item{}, ErrNotFound
	}
	return it, err
}

// ItemPatch changes the fields that are set; nil fields stay as they are.
type ItemPatch struct {
	Status      *string
	DurationMin *int
	Deadline    *string
	Priority    *int
}

func (s *Store) PatchItem(id int64, p ItemPatch) (Item, error) {
	var sets []string
	var args []any
	add := func(col string, v any) {
		sets = append(sets, col+" = ?")
		args = append(args, v)
	}
	if p.Status != nil {
		add("status", *p.Status)
	}
	if p.DurationMin != nil {
		add("duration_min", *p.DurationMin)
	}
	if p.Deadline != nil {
		add("deadline", *p.Deadline)
	}
	if p.Priority != nil {
		add("priority", *p.Priority)
	}
	if len(sets) > 0 {
		res, err := s.DB.Exec(fmt.Sprintf(`UPDATE items SET %s WHERE id = ?`, strings.Join(sets, ", ")), append(args, id)...)
		if err != nil {
			return Item{}, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return Item{}, ErrNotFound
		}
	}
	return s.ItemByID(id)
}

// PlanChunk is one piece of a task, or of a goal when Goal is set (then
// ItemID is 0), written to the calendar.
type PlanChunk struct {
	ItemID   int64
	Goal     string
	ApplyKey string
	Seq      int
	EventID  string
	Start    string // RFC 3339
	End      string
	Minutes  int
}

// AddChunk records a written piece. Recording the same (key, seq) again is a no-op.
func (s *Store) AddChunk(c PlanChunk) error {
	var item any = c.ItemID
	var goal any
	if c.Goal != "" {
		item, goal = nil, c.Goal
	}
	_, err := s.DB.Exec(`INSERT INTO plan_chunks(item_id, goal, apply_key, seq, event_id, start_at, end_at, minutes)
		VALUES (?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`,
		item, goal, c.ApplyKey, c.Seq, c.EventID, c.Start, c.End, c.Minutes)
	return err
}

// ChunksByKey returns the pieces written under an apply key, in order.
func (s *Store) ChunksByKey(key string) ([]PlanChunk, error) {
	rows, err := s.DB.Query(`SELECT item_id, goal, apply_key, seq, event_id, start_at, end_at, minutes
		FROM plan_chunks WHERE apply_key = ? ORDER BY seq`, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlanChunk
	for rows.Next() {
		var c PlanChunk
		var item sql.NullInt64
		var goal sql.NullString
		if err := rows.Scan(&item, &goal, &c.ApplyKey, &c.Seq, &c.EventID, &c.Start, &c.End, &c.Minutes); err != nil {
			return nil, err
		}
		c.ItemID, c.Goal = item.Int64, goal.String
		out = append(out, c)
	}
	return out, rows.Err()
}

// PlannedMinutes sums the written pieces per task, leaving out one apply key
// (so a retry of that request does not count its own pieces).
func (s *Store) PlannedMinutes(exceptKey string) (map[int64]int, error) {
	rows, err := s.DB.Query(`SELECT item_id, SUM(minutes) FROM plan_chunks WHERE apply_key <> ? AND item_id IS NOT NULL GROUP BY item_id`, exceptKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// PlannedGoalMinutes sums the goal blocks that start in [from, to) per goal,
// leaving out one apply key (a retry does not count its own pieces).
func (s *Store) PlannedGoalMinutes(from, to, exceptKey string) (map[string]int, error) {
	rows, err := s.DB.Query(`SELECT goal, SUM(minutes) FROM plan_chunks
		WHERE goal IS NOT NULL AND start_at >= ? AND start_at < ? AND apply_key <> ? GROUP BY goal`, from, to, exceptKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var g string
		var n int
		if err := rows.Scan(&g, &n); err != nil {
			return nil, err
		}
		out[g] = n
	}
	return out, rows.Err()
}
