-- The "Время" tab: activities the user follows by hand (a name, a category, a
-- weekly goal), the time entries of the timer and notes. Titles here are
-- written by the user for the user; they never leave the server.
CREATE TABLE tracked_activity (
    id           INTEGER PRIMARY KEY,
    name         TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
    category     TEXT NOT NULL CHECK (category IN ('study', 'work', 'hobby', 'rest', 'chores', 'other')),
    goal_minutes INTEGER NOT NULL DEFAULT 0 CHECK (goal_minutes >= 0 AND goal_minutes <= 10080),
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;

CREATE TABLE time_entry (
    id          INTEGER PRIMARY KEY,
    activity_id INTEGER REFERENCES tracked_activity(id) ON DELETE SET NULL,
    title       TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 80),
    category    TEXT NOT NULL CHECK (category IN ('study', 'work', 'hobby', 'rest', 'chores', 'other')),
    start_at    TEXT NOT NULL,
    end_at      TEXT
) STRICT;
CREATE INDEX time_entry_start ON time_entry(start_at);
-- Only one timer runs at a time.
CREATE UNIQUE INDEX time_entry_running ON time_entry((1)) WHERE end_at IS NULL;

CREATE TABLE activity_note (
    id          INTEGER PRIMARY KEY,
    activity_id INTEGER NOT NULL REFERENCES tracked_activity(id) ON DELETE CASCADE,
    at          TEXT NOT NULL,
    text        TEXT NOT NULL CHECK (length(text) BETWEEN 1 AND 500)
) STRICT;
CREATE INDEX activity_note_activity ON activity_note(activity_id, at);

-- The user may correct the category of an app: origin "user" wins forever.
ALTER TABLE app_category RENAME TO app_category_old;
CREATE TABLE app_category (
    key        TEXT PRIMARY KEY,
    category   TEXT NOT NULL CHECK (category IN
               ('study', 'career', 'sport', 'work', 'leisure', 'social', 'sleep', 'unknown')),
    origin     TEXT NOT NULL CHECK (origin IN ('android_meta', 'dictionary', 'steam_genre', 'context', 'llm', 'user')),
    confidence REAL NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
    at         TEXT NOT NULL
) STRICT;
INSERT INTO app_category(key, category, origin, confidence, at)
    SELECT key, category, origin, confidence, at FROM app_category_old;
DROP TABLE app_category_old;
