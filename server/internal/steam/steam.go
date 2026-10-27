// Package steam turns the cumulative play time of a Steam profile into
// minutes per day. The profile must be public; Steam gives no per-day
// numbers, so a day is the difference between two polls (PLAN.md stage 10.3).
package steam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"lifeagent/internal/activity"
)

// ErrPrivate means Steam returns no games: the profile or its game list is private.
var ErrPrivate = errors.New("steam profile is private: set \"Game details\" to public")

// Store keeps the snapshots; *store.Store is one.
type Store interface {
	SteamHasSnapshots() (bool, error)
	SteamLatest() (map[int64]int, error)
	AddSteamSnapshot(appid int64, takenAt string, minutes int, base bool) error
	SteamValueBefore(appid int64, before string) (int, bool, error)
	SteamBaseIn(appid int64, from, to string) (int, bool, error)
	SteamChanged(from, to string) ([]int64, error)
	SteamGenres(appids []int64) (map[int64][]string, error)
	PutSteamGenres(appid int64, genres []string, at string) error
	PruneSteam(before string) (int64, error)
}

// Collector implements activity.Collector for Steam.
type Collector struct {
	Key     string // Web API key, from .env; never logged
	SteamID string
	Store   Store
	Loc     *time.Location
	HTTP    *http.Client
	// APIBase and StoreBase are the Steam hosts; tests replace them.
	APIBase, StoreBase string
	// MaxGenreLookups limits store requests per round.
	MaxGenreLookups int
}

// New returns a collector with the real Steam hosts.
func New(key, steamID string, st Store, loc *time.Location) *Collector {
	return &Collector{Key: key, SteamID: steamID, Store: st, Loc: loc,
		APIBase: "https://api.steampowered.com", StoreBase: "https://store.steampowered.com", MaxGenreLookups: 5}
}

func (c *Collector) Source() string { return activity.Steam }

// Every is the polling interval of PLAN.md section 14.
const Every = 30 * time.Minute

const keepSnapshots = 30 * 24 * time.Hour

type game struct {
	AppID   int64 `json:"appid"`
	Minutes int   `json:"playtime_forever"`
}

func (c *Collector) get(ctx context.Context, base, path string, q url.Values, out any) error {
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		// The error text carries the URL, and the URL carries the key.
		return errors.New("steam is not reachable")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return errors.New("steam rejected the API key")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("steam answered %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(out)
}

func (c *Collector) games(ctx context.Context) (map[int64]int, error) {
	q := url.Values{"key": {c.Key}, "steamid": {c.SteamID}, "format": {"json"}, "include_played_free_games": {"1"}}
	var owned struct {
		Response struct {
			GameCount *int   `json:"game_count"`
			Games     []game `json:"games"`
		} `json:"response"`
	}
	if err := c.get(ctx, c.APIBase, "/IPlayerService/GetOwnedGames/v0001/", q, &owned); err != nil {
		return nil, err
	}
	if owned.Response.GameCount == nil {
		return nil, ErrPrivate
	}
	out := map[int64]int{}
	for _, g := range owned.Response.Games {
		out[g.AppID] = g.Minutes
	}
	// The recently played list is fresher on some profiles; the larger number wins.
	q.Del("include_played_free_games")
	var recent struct {
		Response struct {
			Games []game `json:"games"`
		} `json:"response"`
	}
	if err := c.get(ctx, c.APIBase, "/IPlayerService/GetRecentlyPlayedGames/v0001/", q, &recent); err == nil {
		for _, g := range recent.Response.Games {
			if g.Minutes > out[g.AppID] {
				out[g.AppID] = g.Minutes
			}
		}
	}
	return out, nil
}

// Collect polls Steam, records the changes and returns today and yesterday.
func (c *Collector) Collect(ctx context.Context, now time.Time) ([]activity.DayRows, error) {
	games, err := c.games(ctx)
	if err != nil {
		return nil, err
	}
	at := now.UTC().Format(time.RFC3339)
	first, err := c.Store.SteamHasSnapshots()
	if err != nil {
		return nil, err
	}
	latest, err := c.Store.SteamLatest()
	if err != nil {
		return nil, err
	}
	for id, m := range games {
		prev, known := latest[id]
		if known && prev == m {
			continue
		}
		if known && m < prev {
			continue // a smaller number is a glitch of the API, not negative play time
		}
		if err := c.Store.AddSteamSnapshot(id, at, m, !first); err != nil {
			return nil, err
		}
	}
	_, _ = c.Store.PruneSteam(now.Add(-keepSnapshots).UTC().Format(time.RFC3339))

	local := now.In(c.Loc)
	var out []activity.DayRows
	for back := 1; back >= 0; back-- {
		day := time.Date(local.Year(), local.Month(), local.Day()-back, 0, 0, 0, 0, c.Loc)
		rows, err := c.day(ctx, day, at)
		if err != nil {
			return nil, err
		}
		out = append(out, activity.DayRows{Date: day.Format("2006-01-02"), Rows: rows})
	}
	return out, nil
}

// day sums the play time of one local day. All games are leisure.
func (c *Collector) day(ctx context.Context, day time.Time, at string) ([]activity.Row, error) {
	next := time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, c.Loc)
	from, to := day.UTC().Format(time.RFC3339), next.UTC().Format(time.RFC3339)
	changed, err := c.Store.SteamChanged(from, to)
	if err != nil {
		return nil, err
	}
	perGame := map[int64]int{}
	total := 0
	for _, id := range changed {
		end, ok, err := c.Store.SteamValueBefore(id, to)
		if err != nil || !ok {
			return nil, err
		}
		start, ok, err := c.Store.SteamValueBefore(id, from)
		if err != nil {
			return nil, err
		}
		if !ok {
			// No earlier number: a game of the very first poll starts at that
			// poll's value; a game that appeared later started from nothing.
			if b, isBase, err := c.Store.SteamBaseIn(id, from, to); err != nil {
				return nil, err
			} else if isBase {
				start = b
			}
		}
		if d := end - start; d > 0 {
			d = min(d, 24*60)
			perGame[id] = d
			total += d
		}
	}
	if total == 0 {
		return nil, nil
	}
	genres := c.genres(ctx, perGame, at)
	row := activity.Row{Category: activity.Leisure, Minutes: min(total, 24*60), Count: len(perGame), Confidence: 1}
	if len(genres) > 0 {
		list := make([]any, len(genres))
		for i, g := range genres {
			list[i] = g
		}
		row.Extra = map[string]any{"genres": list}
	}
	return []activity.Row{row}, nil
}

// genres returns up to five genres, those of the games played longest first.
// A genre lookup that fails only leaves the genres out.
func (c *Collector) genres(ctx context.Context, perGame map[int64]int, at string) []string {
	ids := make([]int64, 0, len(perGame))
	for id := range perGame {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if perGame[ids[i]] != perGame[ids[j]] {
			return perGame[ids[i]] > perGame[ids[j]]
		}
		return ids[i] < ids[j]
	})
	cached, err := c.Store.SteamGenres(ids)
	if err != nil {
		return nil
	}
	lookups := 0
	for _, id := range ids {
		if _, ok := cached[id]; ok || lookups >= c.MaxGenreLookups {
			continue
		}
		lookups++
		g, err := c.fetchGenres(ctx, id)
		if err != nil {
			continue
		}
		if err := c.Store.PutSteamGenres(id, g, at); err == nil {
			cached[id] = g
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		for _, g := range cached[id] {
			if len(out) < 5 && !seen[g] && activity.ValidateExtra(map[string]any{"genres": []any{g}}) == nil {
				seen[g] = true
				out = append(out, g)
			}
		}
	}
	return out
}

func (c *Collector) fetchGenres(ctx context.Context, appid int64) ([]string, error) {
	id := strconv.FormatInt(appid, 10)
	var resp map[string]struct {
		Success bool `json:"success"`
		Data    struct {
			Genres []struct {
				Description string `json:"description"`
			} `json:"genres"`
		} `json:"data"`
	}
	q := url.Values{"appids": {id}, "filters": {"genres"}, "l": {"english"}}
	if err := c.get(ctx, c.StoreBase, "/api/appdetails", q, &resp); err != nil {
		return nil, err
	}
	var out []string
	for _, g := range resp[id].Data.Genres {
		out = append(out, g.Description)
	}
	if out == nil {
		out = []string{}
	}
	return out, nil
}
