-- Daily health summaries from Health Connect. Only sums and means, never raw
-- samples. date is the day the user woke up on, in the user's time zone.
CREATE TABLE health_daily (
    date        TEXT PRIMARY KEY,
    sleep_hours REAL,
    avg_hr      REAL,
    stress      REAL,
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;
