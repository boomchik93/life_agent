-- Stage 10.6: weekly goals and the weekly review.
--
-- A goal has a weekly norm in minutes. It starts empty; after 14 days of data
-- the server only suggests one (suggested_min), and the user confirms it once.
CREATE TABLE goals (
    goal            TEXT PRIMARY KEY CHECK (goal IN ('study', 'career', 'sport', 'work')),
    weekly_norm_min INTEGER CHECK (weekly_norm_min IS NULL OR weekly_norm_min > 0),
    norm_origin     TEXT CHECK (norm_origin IS NULL OR norm_origin IN ('auto', 'confirmed')),
    suggested_min   INTEGER CHECK (suggested_min IS NULL OR suggested_min > 0),
    suggested_at    TEXT,
    recalculated_at TEXT
) STRICT;

-- The result of the review calculation of a week (JSON), kept as history.
CREATE TABLE weekly_review (
    week_start  TEXT PRIMARY KEY,
    result      TEXT NOT NULL,
    computed_at TEXT NOT NULL
) STRICT;

-- Pieces of a week plan may now belong to a goal instead of a task, so
-- item_id can be empty and goal tells what the block is for.
DROP INDEX plan_chunks_item;
ALTER TABLE plan_chunks RENAME TO plan_chunks_old;
CREATE TABLE plan_chunks (
    id         INTEGER PRIMARY KEY,
    item_id    INTEGER REFERENCES items(id),
    goal       TEXT CHECK (goal IS NULL OR goal IN ('study', 'career', 'sport', 'work')),
    apply_key  TEXT NOT NULL,
    seq        INTEGER NOT NULL,
    event_id   TEXT NOT NULL UNIQUE,
    start_at   TEXT NOT NULL,
    end_at     TEXT NOT NULL,
    minutes    INTEGER NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (apply_key, seq),
    CHECK ((item_id IS NULL) <> (goal IS NULL))
) STRICT;
INSERT INTO plan_chunks(id, item_id, apply_key, seq, event_id, start_at, end_at, minutes, created_at)
    SELECT id, item_id, apply_key, seq, event_id, start_at, end_at, minutes, created_at FROM plan_chunks_old;
DROP TABLE plan_chunks_old;
CREATE INDEX plan_chunks_item ON plan_chunks(item_id);
