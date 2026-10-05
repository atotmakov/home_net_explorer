-- Schema v1 (data-model.md). Timestamps are UTC RFC 3339 with milliseconds, e.g.
-- 2026-10-05T10:00:00.000Z, so lexical order equals chronological order.

------------------------------------------------------------------ facts (append-only)

CREATE TABLE collectors (
    id                 INTEGER PRIMARY KEY,
    name               TEXT NOT NULL UNIQUE
                       CHECK (length(name) BETWEEN 1 AND 64 AND name NOT GLOB '*[^a-z0-9-]*'),
    kind               TEXT NOT NULL CHECK (kind IN ('builtin', 'remote')),
    token_hash         BLOB,
    created_at         TEXT NOT NULL,
    revoked_at         TEXT,
    interval_seconds   INTEGER NOT NULL DEFAULT 900,
    last_report_at     TEXT,
    last_clock_skew_ms INTEGER
);

CREATE TABLE collection_runs (
    collection_id  TEXT PRIMARY KEY,
    collector_id   INTEGER NOT NULL REFERENCES collectors (id),
    schema_version INTEGER NOT NULL,
    started_at     TEXT NOT NULL,
    finished_at    TEXT NOT NULL,
    sent_at        TEXT NOT NULL,
    received_at    TEXT NOT NULL,
    interval_seconds INTEGER NOT NULL DEFAULT 0, -- the run's own interval (offline rule, R7)
    payload_gz     BLOB NOT NULL
);
CREATE INDEX collection_runs_received ON collection_runs (received_at);

CREATE TABLE run_subnets (
    collection_id TEXT NOT NULL REFERENCES collection_runs (collection_id),
    cidr          TEXT NOT NULL,
    method        TEXT NOT NULL CHECK (method IN ('arp', 'icmp_tcp', 'skipped')),
    skip_reason   TEXT CHECK (skip_reason IN ('too_large', 'ignored')),
    complete      INTEGER NOT NULL,
    hosts_probed  INTEGER NOT NULL,
    PRIMARY KEY (collection_id, cidr)
);
CREATE INDEX run_subnets_cidr ON run_subnets (cidr);

CREATE TABLE user_device_attrs (
    id           INTEGER PRIMARY KEY,
    identity_key TEXT NOT NULL,
    field        TEXT NOT NULL CHECK (field IN ('name', 'notes', 'type')),
    value        TEXT NOT NULL,
    at           TEXT NOT NULL
);

CREATE TABLE user_identity_alias (
    id                  INTEGER PRIMARY KEY,
    identity_key        TEXT NOT NULL,
    target_identity_key TEXT NOT NULL,
    kind                TEXT NOT NULL CHECK (kind IN ('merge', 'split')),
    at                  TEXT NOT NULL
);

CREATE TABLE user_links (
    id                INTEGER PRIMARY KEY,
    from_identity_key TEXT NOT NULL,
    to_identity_key   TEXT NOT NULL,
    removed           INTEGER NOT NULL DEFAULT 0,
    at                TEXT NOT NULL
);

CREATE TABLE user_acks (
    id           INTEGER PRIMARY KEY,
    identity_key TEXT NOT NULL,
    at           TEXT NOT NULL
);

CREATE TABLE user_subnet_attrs (
    id    INTEGER PRIMARY KEY,
    cidr  TEXT NOT NULL,
    field TEXT NOT NULL CHECK (field IN ('name', 'ignored')),
    value TEXT NOT NULL,
    at    TEXT NOT NULL
);

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE sessions (
    id_hash    BLOB PRIMARY KEY,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);

------------------------------------------------------------------ projections (rebuildable)

CREATE TABLE subnets (
    id            INTEGER PRIMARY KEY,
    cidr          TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL DEFAULT '',
    ignored       INTEGER NOT NULL DEFAULT 0,
    first_seen_at TEXT NOT NULL,
    discovered_by INTEGER NOT NULL REFERENCES collectors (id)
);

CREATE TABLE devices (
    id                INTEGER PRIMARY KEY,
    identity_key      TEXT NOT NULL UNIQUE,
    identity_strength TEXT NOT NULL CHECK (identity_strength IN ('strong', 'weak')),
    mac               TEXT,
    mac_randomized    INTEGER NOT NULL DEFAULT 0,
    manufacturer      TEXT NOT NULL DEFAULT '',
    hostname          TEXT NOT NULL DEFAULT '',
    hostname_at       TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL
                      CHECK (status IN ('new', 'new_offline', 'online', 'offline', 'merged_away')),
    first_seen        TEXT NOT NULL,
    last_seen         TEXT NOT NULL,
    display_name      TEXT NOT NULL,
    user_name         TEXT NOT NULL DEFAULT '',
    notes             TEXT NOT NULL DEFAULT '',
    type              TEXT NOT NULL DEFAULT '',
    merged_into       INTEGER REFERENCES devices (id)
);
CREATE INDEX devices_status ON devices (status);

CREATE TABLE device_addresses (
    device_id INTEGER NOT NULL REFERENCES devices (id),
    subnet_id INTEGER NOT NULL REFERENCES subnets (id),
    ip        TEXT NOT NULL,
    current   INTEGER NOT NULL,
    as_of     TEXT NOT NULL,
    PRIMARY KEY (device_id, subnet_id, ip)
);
CREATE INDEX device_addresses_ip ON device_addresses (subnet_id, ip, current);

CREATE TABLE sightings (
    id           INTEGER PRIMARY KEY,
    device_id    INTEGER NOT NULL REFERENCES devices (id),
    collector_id INTEGER NOT NULL REFERENCES collectors (id),
    subnet_id    INTEGER NOT NULL REFERENCES subnets (id),
    ip           TEXT NOT NULL,
    mac          TEXT NOT NULL DEFAULT '',
    hostname     TEXT NOT NULL DEFAULT '',
    first_seen   TEXT NOT NULL,
    last_seen    TEXT NOT NULL,
    seen_count   INTEGER NOT NULL CHECK (seen_count >= 1)
);
CREATE INDEX sightings_device ON sightings (device_id, collector_id, subnet_id, last_seen);

CREATE TABLE events (
    id           INTEGER PRIMARY KEY,
    at           TEXT NOT NULL,
    device_id    INTEGER NOT NULL REFERENCES devices (id),
    type         TEXT NOT NULL CHECK (type IN ('new_device', 'ip_changed', 'hostname_changed',
                     'mac_changed', 'went_offline', 'came_online', 'merged', 'split')),
    subnet_id    INTEGER REFERENCES subnets (id),
    old_value    TEXT NOT NULL DEFAULT '',
    new_value    TEXT NOT NULL DEFAULT '',
    collector_id INTEGER REFERENCES collectors (id),
    UNIQUE (device_id, type, at, new_value)
);
CREATE INDEX events_at ON events (at);

CREATE TABLE links (
    id             INTEGER PRIMARY KEY,
    kind           TEXT NOT NULL CHECK (kind IN ('gateway', 'bridge', 'manual')),
    from_device_id INTEGER NOT NULL REFERENCES devices (id),
    to_device_id   INTEGER REFERENCES devices (id),
    subnet_id      INTEGER REFERENCES subnets (id),
    source         TEXT NOT NULL CHECK (source IN ('inferred', 'user'))
);
