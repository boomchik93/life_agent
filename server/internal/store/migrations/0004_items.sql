-- Events and tasks the user confirmed. The calendar stays the source of truth:
-- an event row keeps only the id of the event the agent created there.
CREATE TABLE items (
    id              INTEGER PRIMARY KEY,
    kind            TEXT NOT NULL CHECK (kind IN ('event', 'task')),
    title           TEXT NOT NULL,
    category        TEXT NOT NULL,
    start_at        TEXT,
    end_at          TEXT,
    recurrence      TEXT,
    duration_min    INTEGER,
    deadline        TEXT,
    priority        INTEGER NOT NULL DEFAULT 0,
    status          TEXT NOT NULL CHECK (status IN ('draft', 'confirmed', 'written', 'done')),
    idempotency_key TEXT NOT NULL UNIQUE,
    event_id        TEXT,
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;

-- Normalized title to category, so the question "is this study?" is asked once.
CREATE TABLE category_memory (
    name          TEXT PRIMARY KEY,
    category      TEXT NOT NULL,
    source        TEXT NOT NULL CHECK (source IN ('user', 'model')),
    confirmations INTEGER NOT NULL DEFAULT 1,
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;
