-- How many model calls the categorization of unknown names used on a day
-- (at most 2 a day, PLAN.md section 3).
CREATE TABLE categorize_runs (
    day  TEXT PRIMARY KEY,
    runs INTEGER NOT NULL DEFAULT 0
) STRICT;
