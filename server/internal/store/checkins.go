package store

type Checkin struct {
	Date       string   `json:"date"` // YYYY-MM-DD in the user's time zone
	Energy     int      `json:"energy"`
	Mood       int      `json:"mood"`
	SleepHours *float64 `json:"sleep_hours"`
}

// PutCheckin stores the check-in of a day; a second one for the same day replaces the first.
func (s *Store) PutCheckin(c Checkin) error {
	_, err := s.DB.Exec(`INSERT INTO checkins(date, energy, mood, sleep_hours) VALUES (?, ?, ?, ?)
		ON CONFLICT(date) DO UPDATE SET energy = excluded.energy, mood = excluded.mood,
		sleep_hours = excluded.sleep_hours, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`,
		c.Date, c.Energy, c.Mood, c.SleepHours)
	return err
}

// Checkins returns the check-ins with from <= date <= to (YYYY-MM-DD strings), oldest first.
func (s *Store) Checkins(from, to string) ([]Checkin, error) {
	rows, err := s.DB.Query(`SELECT date, energy, mood, sleep_hours FROM checkins
		WHERE date BETWEEN ? AND ? ORDER BY date`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Checkin
	for rows.Next() {
		var c Checkin
		if err := rows.Scan(&c.Date, &c.Energy, &c.Mood, &c.SleepHours); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
