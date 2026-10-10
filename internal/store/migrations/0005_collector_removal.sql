-- hne:foreign-keys-off
-- Schema v5 (feature 004, maintenance actions): collectors can be removed while their history
-- stays (deleted_at), and a removed collector's name can be used again. SQLite cannot drop the
-- UNIQUE (name) constraint in place and other tables reference collectors, so the table is
-- rebuilt with foreign keys off (the runner checks foreign_key_check before committing).
CREATE TABLE collectors_v5 (
    id                 INTEGER PRIMARY KEY,
    name               TEXT NOT NULL
                       CHECK (length(name) BETWEEN 1 AND 64 AND name NOT GLOB '*[^a-z0-9-]*'),
    kind               TEXT NOT NULL CHECK (kind IN ('builtin', 'remote')),
    token_hash         BLOB,
    created_at         TEXT NOT NULL,
    revoked_at         TEXT,
    interval_seconds   INTEGER NOT NULL DEFAULT 900,
    last_report_at     TEXT,
    last_clock_skew_ms INTEGER,
    last_version       TEXT NOT NULL DEFAULT '',
    deleted_at         TEXT
);
INSERT INTO collectors_v5 (id, name, kind, token_hash, created_at, revoked_at, interval_seconds,
                           last_report_at, last_clock_skew_ms, last_version)
    SELECT id, name, kind, token_hash, created_at, revoked_at, interval_seconds,
           last_report_at, last_clock_skew_ms, last_version FROM collectors;
DROP TABLE collectors;
ALTER TABLE collectors_v5 RENAME TO collectors;
CREATE UNIQUE INDEX collectors_name_active ON collectors (name) WHERE deleted_at IS NULL;
