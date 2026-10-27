package store

import (
	"database/sql"
	"time"
)

// Advice is one row of the advice log.
type Advice struct {
	ID     int64  `json:"id"`
	At     string `json:"at"`     // RFC 3339
	Reason string `json:"reason"` // comma-separated reason codes
	Text   string `json:"text"`
	Model  string `json:"model"`
	Source string `json:"source"`
	Status string `json:"status"`
}

func scanAdvice(row interface{ Scan(...any) error }) (Advice, error) {
	var a Advice
	err := row.Scan(&a.ID, &a.At, &a.Reason, &a.Text, &a.Model, &a.Source, &a.Status)
	return a, err
}

const adviceCols = `id, at, reason, text, model, source, status`

// AddAdvice stores an advice and returns it with its id.
func (s *Store) AddAdvice(a Advice) (Advice, error) {
	res, err := s.DB.Exec(`INSERT INTO advice_log(at, reason, text, model, source, status) VALUES (?,?,?,?,?,?)`,
		a.At, a.Reason, a.Text, a.Model, a.Source, a.Status)
	if err != nil {
		return Advice{}, err
	}
	a.ID, err = res.LastInsertId()
	return a, err
}

// LastAdviceAt returns the time of the newest advice, if there is one.
func (s *Store) LastAdviceAt() (*time.Time, error) {
	var at string
	err := s.DB.QueryRow(`SELECT at FROM advice_log ORDER BY at DESC, id DESC LIMIT 1`).Scan(&at)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// TakePendingAdvice returns the oldest advice waiting for the phone and marks
// it delivered, so a notification is shown once. ok is false when none waits.
func (s *Store) TakePendingAdvice() (a Advice, ok bool, err error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return Advice{}, false, err
	}
	defer tx.Rollback()
	a, err = scanAdvice(tx.QueryRow(`SELECT ` + adviceCols + ` FROM advice_log WHERE status = 'pending' ORDER BY at, id LIMIT 1`))
	if err == sql.ErrNoRows {
		return Advice{}, false, nil
	}
	if err != nil {
		return Advice{}, false, err
	}
	if _, err := tx.Exec(`UPDATE advice_log SET status = 'delivered' WHERE id = ?`, a.ID); err != nil {
		return Advice{}, false, err
	}
	a.Status = "delivered"
	return a, true, tx.Commit()
}

// HasPendingAdvice reports whether a prepared advice waits for the phone.
func (s *Store) HasPendingAdvice() (bool, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM advice_log WHERE status = 'pending'`).Scan(&n)
	return n > 0, err
}

// Advices returns the newest advice first, at most limit rows.
func (s *Store) Advices(limit int) ([]Advice, error) {
	rows, err := s.DB.Query(`SELECT `+adviceCols+` FROM advice_log ORDER BY at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Advice{}
	for rows.Next() {
		a, err := scanAdvice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
