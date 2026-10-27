package store

// Health is the summary of one day. A nil field is unknown, not zero.
type Health struct {
	Date       string   `json:"date"`
	SleepHours *float64 `json:"sleep_hours"`
	AvgHR      *float64 `json:"avg_hr"`
	Stress     *float64 `json:"stress"`
	RestingHR  *float64 `json:"resting_hr"`
	Steps      *int     `json:"steps"`
	WorkoutMin *int     `json:"workout_min"`
}

// PutHealth stores the summary of a day. A field the new summary leaves out
// keeps its old value, so a partial reading never wipes a good one.
func (s *Store) PutHealth(h Health) error {
	_, err := s.DB.Exec(`INSERT INTO health_daily(date, sleep_hours, avg_hr, stress, resting_hr, steps, workout_min)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(date) DO UPDATE SET
		  sleep_hours = COALESCE(excluded.sleep_hours, health_daily.sleep_hours),
		  avg_hr      = COALESCE(excluded.avg_hr, health_daily.avg_hr),
		  stress      = COALESCE(excluded.stress, health_daily.stress),
		  resting_hr  = COALESCE(excluded.resting_hr, health_daily.resting_hr),
		  steps       = COALESCE(excluded.steps, health_daily.steps),
		  workout_min = COALESCE(excluded.workout_min, health_daily.workout_min),
		  updated_at  = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`,
		h.Date, h.SleepHours, h.AvgHR, h.Stress, h.RestingHR, h.Steps, h.WorkoutMin)
	return err
}

// Healths returns the summaries with from <= date <= to, oldest first.
func (s *Store) Healths(from, to string) ([]Health, error) {
	rows, err := s.DB.Query(`SELECT date, sleep_hours, avg_hr, stress, resting_hr, steps, workout_min FROM health_daily
		WHERE date BETWEEN ? AND ? ORDER BY date`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Health
	for rows.Next() {
		var h Health
		if err := rows.Scan(&h.Date, &h.SleepHours, &h.AvgHR, &h.Stress, &h.RestingHR, &h.Steps, &h.WorkoutMin); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// DeleteHealth removes every summary and returns how many there were.
func (s *Store) DeleteHealth() (int64, error) {
	res, err := s.DB.Exec(`DELETE FROM health_daily`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
