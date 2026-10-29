package store

import "database/sql"

// Item is a confirmed event or task.
type Item struct {
	ID             int64   `json:"id"`
	Kind           string  `json:"kind"`
	Title          string  `json:"title"`
	Category       string  `json:"category"`
	Start          *string `json:"start"`
	End            *string `json:"end"`
	Recurrence     *string `json:"recurrence"`
	DurationMin    *int    `json:"duration_min"`
	Deadline       *string `json:"deadline"`
	Priority       int     `json:"priority"`
	Status         string  `json:"status"`
	IdempotencyKey string  `json:"-"`
	EventID        *string `json:"event_id"`
}

const itemCols = `id, kind, title, category, start_at, end_at, recurrence, duration_min, deadline,
	priority, status, idempotency_key, event_id`

func scanItem(row interface{ Scan(...any) error }) (Item, error) {
	var it Item
	err := row.Scan(&it.ID, &it.Kind, &it.Title, &it.Category, &it.Start, &it.End, &it.Recurrence,
		&it.DurationMin, &it.Deadline, &it.Priority, &it.Status, &it.IdempotencyKey, &it.EventID)
	return it, err
}

// ItemByKey returns the item saved under an idempotency key; ok is false when there is none.
func (s *Store) ItemByKey(key string) (it Item, ok bool, err error) {
	it, err = scanItem(s.DB.QueryRow(`SELECT `+itemCols+` FROM items WHERE idempotency_key = ?`, key))
	if err == sql.ErrNoRows {
		return Item{}, false, nil
	}
	return it, err == nil, err
}

// AddItem saves an item. When the key is already taken, the stored item is
// returned and created is false: a repeated request never makes a second row.
func (s *Store) AddItem(it Item) (saved Item, created bool, err error) {
	res, err := s.DB.Exec(`INSERT INTO items(kind, title, category, start_at, end_at, recurrence,
		duration_min, deadline, priority, status, idempotency_key, event_id)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(idempotency_key) DO NOTHING`,
		it.Kind, it.Title, it.Category, it.Start, it.End, it.Recurrence, it.DurationMin, it.Deadline,
		it.Priority, it.Status, it.IdempotencyKey, it.EventID)
	if err != nil {
		return Item{}, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Item{}, false, err
	}
	saved, _, err = s.ItemByKey(it.IdempotencyKey)
	return saved, n == 1, err
}

// CategoryOf returns the remembered category of a normalized title.
func (s *Store) CategoryOf(name string) (category string, ok bool, err error) {
	err = s.DB.QueryRow(`SELECT category FROM category_memory WHERE name = ?`, name).Scan(&category)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	return category, err == nil, err
}

// CategoryMemory returns every remembered title with its category.
func (s *Store) CategoryMemory() (map[string]string, error) {
	rows, err := s.DB.Query(`SELECT name, category FROM category_memory`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var name, cat string
		if err := rows.Scan(&name, &cat); err != nil {
			return nil, err
		}
		out[name] = cat
	}
	return out, rows.Err()
}

// RememberCategory stores an answer. The same category again adds a
// confirmation; a different one replaces the old answer.
func (s *Store) RememberCategory(name, category, source string) error {
	_, err := s.DB.Exec(`INSERT INTO category_memory(name, category, source) VALUES (?,?,?)
		ON CONFLICT(name) DO UPDATE SET
		  confirmations = CASE WHEN category = excluded.category THEN confirmations + 1 ELSE 1 END,
		  category = excluded.category, source = excluded.source,
		  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`, name, category, source)
	return err
}
