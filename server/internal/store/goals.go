package store

import "database/sql"

// Goal is one of the four equal goals. A nil WeeklyNormMin means no norm yet:
// the review then shows only the fact.
type Goal struct {
	Goal           string  `json:"goal"`
	WeeklyNormMin  *int    `json:"weekly_norm_min"`
	NormOrigin     *string `json:"norm_origin"`
	SuggestedMin   *int    `json:"suggested_min"`
	SuggestedAt    *string `json:"suggested_at"`
	RecalculatedAt *string `json:"recalculated_at"`
}

// GoalNames are the goals in the order they are shown. None is the main one.
var GoalNames = []string{"study", "career", "sport", "work"}

// Goals returns the four goals; one never touched has no norm.
func (s *Store) Goals() ([]Goal, error) {
	rows, err := s.DB.Query(`SELECT goal, weekly_norm_min, norm_origin, suggested_min, suggested_at, recalculated_at FROM goals`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	known := map[string]Goal{}
	for rows.Next() {
		var g Goal
		if err := rows.Scan(&g.Goal, &g.WeeklyNormMin, &g.NormOrigin, &g.SuggestedMin, &g.SuggestedAt, &g.RecalculatedAt); err != nil {
			return nil, err
		}
		known[g.Goal] = g
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]Goal, 0, len(GoalNames))
	for _, name := range GoalNames {
		g, ok := known[name]
		if !ok {
			g = Goal{Goal: name}
		}
		out = append(out, g)
	}
	return out, nil
}

// SetGoalNorm sets the weekly norm, confirmed by the user, and clears the
// suggestion it answers.
func (s *Store) SetGoalNorm(goal string, minutes int, now string) error {
	_, err := s.DB.Exec(`INSERT INTO goals(goal, weekly_norm_min, norm_origin, recalculated_at) VALUES (?, ?, 'confirmed', ?)
		ON CONFLICT(goal) DO UPDATE SET weekly_norm_min = excluded.weekly_norm_min, norm_origin = 'confirmed',
		  suggested_min = NULL, suggested_at = NULL, recalculated_at = excluded.recalculated_at`, goal, minutes, now)
	return err
}

// SetGoalSuggestion stores a norm the server proposes.
func (s *Store) SetGoalSuggestion(goal string, minutes int, now string) error {
	_, err := s.DB.Exec(`INSERT INTO goals(goal, suggested_min, suggested_at) VALUES (?, ?, ?)
		ON CONFLICT(goal) DO UPDATE SET suggested_min = excluded.suggested_min, suggested_at = excluded.suggested_at`,
		goal, minutes, now)
	return err
}

// KeepGoalNorm answers a suggestion with "keep the current norm": the
// suggestion goes away and the four weeks start again.
func (s *Store) KeepGoalNorm(goal, now string) error {
	_, err := s.DB.Exec(`UPDATE goals SET suggested_min = NULL, suggested_at = NULL, recalculated_at = ? WHERE goal = ?`, now, goal)
	return err
}

// ClearGoalNorm removes the norm: the goal shows only the fact again.
func (s *Store) ClearGoalNorm(goal string) error {
	_, err := s.DB.Exec(`UPDATE goals SET weekly_norm_min = NULL, norm_origin = NULL, suggested_min = NULL,
		suggested_at = NULL, recalculated_at = NULL WHERE goal = ?`, goal)
	return err
}

// SaveReview keeps the result of a week's calculation.
func (s *Store) SaveReview(weekStart, result, at string) error {
	_, err := s.DB.Exec(`INSERT OR REPLACE INTO weekly_review(week_start, result, computed_at) VALUES (?,?,?)`, weekStart, result, at)
	return err
}

// Review returns the stored result of a week.
func (s *Store) Review(weekStart string) (string, bool, error) {
	var r string
	err := s.DB.QueryRow(`SELECT result FROM weekly_review WHERE week_start = ?`, weekStart).Scan(&r)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	return r, err == nil, err
}

// FirstActivityDate returns the date of the oldest daily sum of the timed
// sources (not the weather, which also holds future days), or "".
func (s *Store) FirstActivityDate() (string, error) {
	var d sql.NullString
	err := s.DB.QueryRow(`SELECT MIN(date) FROM activity_daily WHERE source <> 'weather' AND (minutes > 0 OR count > 0)`).Scan(&d)
	return d.String, err
}
