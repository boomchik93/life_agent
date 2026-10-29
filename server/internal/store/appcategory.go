package store

import "strings"

// AppCategory is the category of an app, game or domain. It holds no usage data.
type AppCategory struct {
	Key        string  `json:"key"`
	Category   string  `json:"category"`
	Origin     string  `json:"origin"`
	Confidence float64 `json:"confidence"`
	At         string  `json:"at"`
}

// AppCategories returns the cached categories of the given keys; keys without
// an entry are absent from the result.
func (s *Store) AppCategories(keys []string) (map[string]AppCategory, error) {
	out := map[string]AppCategory{}
	if len(keys) == 0 {
		return out, nil
	}
	args := make([]any, len(keys))
	for i, k := range keys {
		args[i] = k
	}
	rows, err := s.DB.Query(`SELECT key, category, origin, confidence, at FROM app_category WHERE key IN (?`+
		strings.Repeat(",?", len(keys)-1)+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a AppCategory
		if err := rows.Scan(&a.Key, &a.Category, &a.Origin, &a.Confidence, &a.At); err != nil {
			return nil, err
		}
		out[a.Key] = a
	}
	return out, rows.Err()
}

// PutAppCategory caches a category. The first answer for a key stays: the
// cache is forever (PLAN.md section 3), so a later guess does not replace it.
func (s *Store) PutAppCategory(a AppCategory) error {
	_, err := s.DB.Exec(`INSERT INTO app_category(key, category, origin, confidence, at) VALUES (?,?,?,?,?)
		ON CONFLICT(key) DO NOTHING`, a.Key, a.Category, a.Origin, a.Confidence, a.At)
	return err
}

// PutDictionaryCategory stores the dictionary's answer. It replaces a guess of
// the model or an older dictionary entry, never the user's own choice.
func (s *Store) PutDictionaryCategory(a AppCategory) error {
	_, err := s.DB.Exec(`INSERT INTO app_category(key, category, origin, confidence, at) VALUES (?,?,?,?,?)
		ON CONFLICT(key) DO UPDATE SET category = excluded.category, origin = excluded.origin,
		  confidence = excluded.confidence, at = excluded.at
		WHERE app_category.origin <> 'user'`, a.Key, a.Category, a.Origin, a.Confidence, a.At)
	return err
}

// CategorizeRuns returns the model calls the categorization used on a day.
func (s *Store) CategorizeRuns(day string) (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COALESCE((SELECT runs FROM categorize_runs WHERE day = ?), 0)`, day).Scan(&n)
	return n, err
}

func (s *Store) AddCategorizeRun(day string) error {
	_, err := s.DB.Exec(`INSERT INTO categorize_runs(day, runs) VALUES (?, 1)
		ON CONFLICT(day) DO UPDATE SET runs = runs + 1`, day)
	return err
}

// SetUserAppCategory stores the user's own category of an app; it replaces any
// earlier answer, whatever its origin.
func (s *Store) SetUserAppCategory(key, category, at string) error {
	_, err := s.DB.Exec(`INSERT INTO app_category(key, category, origin, confidence, at) VALUES (?,?,'user',1,?)
		ON CONFLICT(key) DO UPDATE SET category = excluded.category, origin = 'user', confidence = 1, at = excluded.at`,
		key, category, at)
	return err
}
