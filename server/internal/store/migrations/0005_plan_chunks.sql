-- Pieces of tasks the week planner wrote to the calendar. The sum of a task's
-- minutes is what has been planned; the rest is still to place.
CREATE TABLE plan_chunks (
    id         INTEGER PRIMARY KEY,
    item_id    INTEGER NOT NULL REFERENCES items(id),
    apply_key  TEXT NOT NULL,
    seq        INTEGER NOT NULL,
    event_id   TEXT NOT NULL UNIQUE,
    start_at   TEXT NOT NULL,
    end_at     TEXT NOT NULL,
    minutes    INTEGER NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (apply_key, seq)
) STRICT;

CREATE INDEX plan_chunks_item ON plan_chunks(item_id);
