-- Schema v4 (feature 003, router setup from the web UI): router settings and the outcome
-- login_unavailable.

-- run_sources.outcome gains 'login_unavailable'. SQLite cannot alter a CHECK constraint, so the
-- table is rebuilt (nothing references it).
CREATE TABLE run_sources_v4 (
    collection_id TEXT NOT NULL REFERENCES collection_runs (collection_id),
    idx           INTEGER NOT NULL,
    type          TEXT NOT NULL CHECK (type IN ('router')),
    model         TEXT NOT NULL,
    address       TEXT NOT NULL,
    subnet        TEXT NOT NULL,
    outcome       TEXT NOT NULL CHECK (outcome IN ('ok', 'unreachable', 'login_rejected', 'locked',
                      'session_busy', 'page_not_understood', 'skipped_after_rejection', 'login_unavailable')),
    online        INTEGER NOT NULL CHECK (online >= 0),
    offline       INTEGER NOT NULL CHECK (offline >= 0),
    PRIMARY KEY (collection_id, idx)
);
INSERT INTO run_sources_v4 SELECT collection_id, idx, type, model, address, subnet, outcome, online, offline
    FROM run_sources;
DROP TABLE run_sources;
ALTER TABLE run_sources_v4 RENAME TO run_sources;

-- Owner configuration, not a projection: Rebuild neither reads nor clears it, and it keeps only
-- the current login (no history of old passwords). The password is stored in PLAIN TEXT by owner
-- decision (specs/003-router-ui-setup/spec.md, TODO-SEC-1); it is never rendered by any page.
CREATE TABLE router_settings (
    id           INTEGER PRIMARY KEY,
    identity_key TEXT NOT NULL UNIQUE,
    model        TEXT NOT NULL,
    subnet       TEXT NOT NULL DEFAULT '',
    username     TEXT NOT NULL CHECK (length(username) BETWEEN 1 AND 128),
    password     TEXT NOT NULL CHECK (length(password) BETWEEN 1 AND 128),
    updated_at   TEXT NOT NULL
);
