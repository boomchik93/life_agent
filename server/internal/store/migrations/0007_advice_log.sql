-- Advice given to the user. status: pending (prepared, the phone has not
-- fetched it yet) or delivered. source: auto or user (asked for).
CREATE TABLE advice_log (
    id      INTEGER PRIMARY KEY,
    at      TEXT NOT NULL,
    reason  TEXT NOT NULL,
    text    TEXT NOT NULL,
    model   TEXT NOT NULL,
    source  TEXT NOT NULL CHECK (source IN ('auto', 'user')),
    status  TEXT NOT NULL CHECK (status IN ('pending', 'delivered'))
) STRICT;
