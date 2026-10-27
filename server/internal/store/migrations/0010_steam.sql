-- Raw cumulative play time per game, taken every 30 minutes. A row is added
-- only when the number changes. Pruned after 30 days; the daily sums live in
-- activity_daily. No game names are stored, only ids.
CREATE TABLE steam_snapshots (
    appid    INTEGER NOT NULL,
    taken_at TEXT NOT NULL,
    minutes  INTEGER NOT NULL CHECK (minutes >= 0),
    base     INTEGER NOT NULL DEFAULT 0 CHECK (base IN (0, 1)),
    PRIMARY KEY (appid, taken_at)
) STRICT;

CREATE TABLE steam_genres (
    appid      INTEGER PRIMARY KEY,
    genres     TEXT NOT NULL,
    fetched_at TEXT NOT NULL
) STRICT;
