-- Stage 10: what the user did, as daily sums. Never titles, URLs or names of
-- windows: only a category, minutes and counts. One row per day, source and
-- category; a new summary of the same day and source replaces the old rows.
CREATE TABLE activity_daily (
    date          TEXT NOT NULL,
    source        TEXT NOT NULL CHECK (source IN
                  ('android_usage', 'mac_activitywatch', 'steam', 'github', 'leetcode', 'weather')),
    category      TEXT NOT NULL CHECK (category IN
                  ('study', 'career', 'sport', 'work', 'leisure', 'social', 'sleep', 'unknown')),
    minutes       INTEGER NOT NULL DEFAULT 0 CHECK (minutes >= 0),
    night_minutes INTEGER NOT NULL DEFAULT 0 CHECK (night_minutes >= 0),
    count         INTEGER NOT NULL DEFAULT 0 CHECK (count >= 0),
    confidence    REAL NOT NULL DEFAULT 1 CHECK (confidence >= 0 AND confidence <= 1),
    extra         TEXT NOT NULL DEFAULT '{}',
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (date, source, category)
) STRICT;

-- Normalized identifier of an app, game or domain (an Android package, a
-- domain, steam:<appid>, a Mac app name) and its category. No usage data.
CREATE TABLE app_category (
    key        TEXT PRIMARY KEY,
    category   TEXT NOT NULL CHECK (category IN
               ('study', 'career', 'sport', 'work', 'leisure', 'social', 'sleep', 'unknown')),
    origin     TEXT NOT NULL CHECK (origin IN ('android_meta', 'dictionary', 'steam_genre', 'context', 'llm')),
    confidence REAL NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
    at         TEXT NOT NULL
) STRICT;

-- The switch and the health of every source.
CREATE TABLE source_state (
    source     TEXT PRIMARY KEY,
    enabled    INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    last_sync  TEXT,
    last_error TEXT
) STRICT;
