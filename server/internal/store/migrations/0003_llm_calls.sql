-- One row per call to a model. Request and response texts are never stored.
CREATE TABLE llm_calls (
    id                INTEGER PRIMARY KEY,
    ts                TEXT NOT NULL,
    task              TEXT NOT NULL,
    provider          TEXT NOT NULL,
    model             TEXT NOT NULL,
    latency_ms        INTEGER NOT NULL,
    prompt_tokens     INTEGER,
    completion_tokens INTEGER,
    result            TEXT NOT NULL CHECK (result IN ('ok', 'invalid_json', 'check_failed', 'timeout', 'limit', 'error')),
    attempt           INTEGER NOT NULL,
    chain_pos         INTEGER NOT NULL,
    source            TEXT NOT NULL CHECK (source IN ('user', 'auto', 'eval'))
) STRICT;

CREATE INDEX llm_calls_ts ON llm_calls(ts);
