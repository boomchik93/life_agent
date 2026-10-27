package store

import (
	"database/sql"
	"encoding/json"
	"strings"
)

// SteamHasSnapshots reports whether any poll has happened yet.
func (s *Store) SteamHasSnapshots() (bool, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM steam_snapshots`).Scan(&n)
	return n > 0, err
}

// SteamLatest returns the newest known play time of every game.
func (s *Store) SteamLatest() (map[int64]int, error) {
	rows, err := s.DB.Query(`SELECT appid, minutes FROM steam_snapshots s
		WHERE taken_at = (SELECT MAX(taken_at) FROM steam_snapshots WHERE appid = s.appid)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var m int
		if err := rows.Scan(&id, &m); err != nil {
			return nil, err
		}
		out[id] = m
	}
	return out, rows.Err()
}

// AddSteamSnapshot records the play time of a game. base marks the first poll
// ever: those numbers are a starting point, not play time of that day.
func (s *Store) AddSteamSnapshot(appid int64, takenAt string, minutes int, base bool) error {
	b := 0
	if base {
		b = 1
	}
	_, err := s.DB.Exec(`INSERT OR REPLACE INTO steam_snapshots(appid, taken_at, minutes, base) VALUES (?,?,?,?)`,
		appid, takenAt, minutes, b)
	return err
}

// SteamValueBefore returns the newest play time recorded before the moment.
func (s *Store) SteamValueBefore(appid int64, before string) (minutes int, ok bool, err error) {
	err = s.DB.QueryRow(`SELECT minutes FROM steam_snapshots WHERE appid = ? AND taken_at < ?
		ORDER BY taken_at DESC LIMIT 1`, appid, before).Scan(&minutes)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	return minutes, err == nil, err
}

// SteamBaseIn returns the starting-point snapshot of a game if it was taken in [from, to).
func (s *Store) SteamBaseIn(appid int64, from, to string) (minutes int, ok bool, err error) {
	err = s.DB.QueryRow(`SELECT minutes FROM steam_snapshots WHERE appid = ? AND base = 1 AND taken_at >= ? AND taken_at < ?`,
		appid, from, to).Scan(&minutes)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	return minutes, err == nil, err
}

// SteamChanged returns the games with a snapshot in [from, to).
func (s *Store) SteamChanged(from, to string) ([]int64, error) {
	rows, err := s.DB.Query(`SELECT DISTINCT appid FROM steam_snapshots WHERE taken_at >= ? AND taken_at < ? ORDER BY appid`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// PruneSteam removes snapshots taken before the moment.
func (s *Store) PruneSteam(before string) (int64, error) {
	res, err := s.DB.Exec(`DELETE FROM steam_snapshots WHERE taken_at < ?`, before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SteamGenres returns the cached genres of the games that have them.
func (s *Store) SteamGenres(appids []int64) (map[int64][]string, error) {
	out := map[int64][]string{}
	if len(appids) == 0 {
		return out, nil
	}
	args := make([]any, len(appids))
	for i, id := range appids {
		args[i] = id
	}
	rows, err := s.DB.Query(`SELECT appid, genres FROM steam_genres WHERE appid IN (?`+strings.Repeat(",?", len(appids)-1)+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var g []string
		_ = json.Unmarshal([]byte(raw), &g)
		out[id] = g
	}
	return out, rows.Err()
}

func (s *Store) PutSteamGenres(appid int64, genres []string, at string) error {
	raw, err := json.Marshal(genres)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`INSERT OR REPLACE INTO steam_genres(appid, genres, fetched_at) VALUES (?,?,?)`, appid, string(raw), at)
	return err
}
