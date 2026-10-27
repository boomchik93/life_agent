package store

import (
	"database/sql"
	"fmt"
)

// ActivityRow is one daily sum.
type ActivityRow struct {
	Date         string  `json:"date"`
	Source       string  `json:"source"`
	Category     string  `json:"category"`
	Minutes      int     `json:"minutes"`
	NightMinutes int     `json:"night_minutes"`
	Count        int     `json:"count"`
	Confidence   float64 `json:"confidence"`
	Extra        string  `json:"extra"` // JSON object
}

// ReplaceActivity swaps every row of (date, source) for the given rows: a new
// summary of a day is a full picture of it, never a partial add-on.
func (s *Store) ReplaceActivity(date, source string, rows []ActivityRow) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM activity_daily WHERE date = ? AND source = ?`, date, source); err != nil {
		return err
	}
	for _, r := range rows {
		extra := r.Extra
		if extra == "" {
			extra = "{}"
		}
		if _, err := tx.Exec(`INSERT INTO activity_daily(date, source, category, minutes, night_minutes, count, confidence, extra)
			VALUES (?,?,?,?,?,?,?,?)`, date, source, r.Category, r.Minutes, r.NightMinutes, r.Count, r.Confidence, extra); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Activity returns the rows with from <= date <= to, oldest first. An empty
// source means all sources.
func (s *Store) Activity(from, to, source string) ([]ActivityRow, error) {
	q := `SELECT date, source, category, minutes, night_minutes, count, confidence, extra
		FROM activity_daily WHERE date BETWEEN ? AND ?`
	args := []any{from, to}
	if source != "" {
		q += ` AND source = ?`
		args = append(args, source)
	}
	rows, err := s.DB.Query(q+` ORDER BY date, source, category`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ActivityRow{}
	for rows.Next() {
		var r ActivityRow
		if err := rows.Scan(&r.Date, &r.Source, &r.Category, &r.Minutes, &r.NightMinutes, &r.Count, &r.Confidence, &r.Extra); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteActivity removes every row of a source and clears its sync state. The
// switch of the source stays as it was.
func (s *Store) DeleteActivity(source string) (int64, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM activity_daily WHERE source = ?`, source)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if _, err := tx.Exec(`UPDATE source_state SET last_sync = NULL, last_error = NULL WHERE source = ?`, source); err != nil {
		return 0, err
	}
	return n, tx.Commit()
}

// PruneActivity removes the sums of days before the given date.
func (s *Store) PruneActivity(beforeDate string) (int64, error) {
	res, err := s.DB.Exec(`DELETE FROM activity_daily WHERE date < ?`, beforeDate)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SourceState is the switch and the health of one source.
type SourceState struct {
	Source    string  `json:"source"`
	Enabled   bool    `json:"enabled"`
	LastSync  *string `json:"last_sync"`
	LastError *string `json:"last_error"`
}

// SourceStates returns the state of the given sources; a source without a row
// is enabled and has never synced.
func (s *Store) SourceStates(sources []string) ([]SourceState, error) {
	rows, err := s.DB.Query(`SELECT source, enabled, last_sync, last_error FROM source_state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	known := map[string]SourceState{}
	for rows.Next() {
		var st SourceState
		var en int
		if err := rows.Scan(&st.Source, &en, &st.LastSync, &st.LastError); err != nil {
			return nil, err
		}
		st.Enabled = en == 1
		known[st.Source] = st
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]SourceState, 0, len(sources))
	for _, name := range sources {
		st, ok := known[name]
		if !ok {
			st = SourceState{Source: name, Enabled: true}
		}
		out = append(out, st)
	}
	return out, nil
}

// SourceEnabled reports the switch; a source never touched is on.
func (s *Store) SourceEnabled(source string) (bool, error) {
	var en int
	err := s.DB.QueryRow(`SELECT enabled FROM source_state WHERE source = ?`, source).Scan(&en)
	if err == sql.ErrNoRows {
		return true, nil
	}
	return en == 1, err
}

func (s *Store) SetSourceEnabled(source string, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	_, err := s.DB.Exec(`INSERT INTO source_state(source, enabled) VALUES (?, ?)
		ON CONFLICT(source) DO UPDATE SET enabled = excluded.enabled`, source, v)
	return err
}

// RecordSync stores the time of the last attempt and its error, if it failed.
// A success clears the error.
func (s *Store) RecordSync(source, at string, errMsg *string) error {
	if errMsg == nil {
		_, err := s.DB.Exec(`INSERT INTO source_state(source, last_sync, last_error) VALUES (?, ?, NULL)
			ON CONFLICT(source) DO UPDATE SET last_sync = excluded.last_sync, last_error = NULL`, source, at)
		return err
	}
	_, err := s.DB.Exec(`INSERT INTO source_state(source, last_error) VALUES (?, ?)
		ON CONFLICT(source) DO UPDATE SET last_error = excluded.last_error`, source, *errMsg)
	return err
}

// exportTables are the tables of the user's data, in the order of the export.
var exportTables = []string{
	"settings", "checkins", "health_daily", "activity_daily", "app_category", "source_state",
	"items", "plan_chunks", "category_memory", "advice_log", "llm_calls", "goals", "weekly_review",
}

// dataTables are wiped by DeleteAllData: all of the above except the settings
// (models, thresholds), which are configuration rather than data about a day.
// plan_chunks goes before items because of the foreign key.
var dataTables = []string{
	"weekly_review", "goals", "plan_chunks", "items", "category_memory", "checkins", "health_daily", "activity_daily",
	"app_category", "source_state", "advice_log", "llm_calls",
}

// Export returns every table of the user's data as lists of rows.
func (s *Store) Export() (map[string][]map[string]any, error) {
	out := make(map[string][]map[string]any, len(exportTables))
	for _, t := range exportTables {
		rows, err := s.DB.Query(`SELECT * FROM ` + t)
		if err != nil {
			return nil, fmt.Errorf("export %s: %w", t, err)
		}
		cols, err := rows.Columns()
		if err != nil {
			rows.Close()
			return nil, err
		}
		list := []map[string]any{}
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				rows.Close()
				return nil, err
			}
			m := make(map[string]any, len(cols))
			for i, c := range cols {
				if b, ok := vals[i].([]byte); ok {
					m[c] = string(b)
				} else {
					m[c] = vals[i]
				}
			}
			list = append(list, m)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		out[t] = list
	}
	return out, nil
}

// DeleteAllData erases the user's data and returns the number of rows removed
// per table. The settings stay.
func (s *Store) DeleteAllData() (map[string]int64, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	counts := make(map[string]int64, len(dataTables))
	for _, t := range dataTables {
		res, err := tx.Exec(`DELETE FROM ` + t)
		if err != nil {
			return nil, fmt.Errorf("delete %s: %w", t, err)
		}
		counts[t], _ = res.RowsAffected()
	}
	return counts, tx.Commit()
}
