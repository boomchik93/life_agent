CREATE TABLE checkins (
    date        TEXT PRIMARY KEY,
    energy      INTEGER NOT NULL CHECK (energy BETWEEN 1 AND 5),
    mood        INTEGER NOT NULL CHECK (mood BETWEEN 1 AND 5),
    sleep_hours REAL CHECK (sleep_hours IS NULL OR sleep_hours BETWEEN 0 AND 24),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;
