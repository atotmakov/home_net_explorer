-- Schema v10 (feature 015, open ports per device): the port checks of port runs, the open ports
-- they found, and each device's derived ports; port events and the owner's "ports" on/off fact.

-- One row per address checked in a port run (data-model.md). Derived from the stored run: a
-- rebuild clears and refills it.
CREATE TABLE port_checks (
    id            INTEGER PRIMARY KEY,
    collection_id TEXT NOT NULL REFERENCES collection_runs (collection_id),
    collector_id  INTEGER NOT NULL REFERENCES collectors (id),
    device_id     INTEGER NOT NULL REFERENCES devices (id),
    subnet_id     INTEGER REFERENCES subnets (id),
    ip            TEXT NOT NULL,
    started_at    TEXT NOT NULL,
    finished_at   TEXT NOT NULL,
    complete      INTEGER NOT NULL CHECK (complete IN (0, 1)),
    all_open      INTEGER NOT NULL CHECK (all_open IN (0, 1))
);
CREATE INDEX port_checks_device ON port_checks (device_id, finished_at);

-- The open ports of a check; closed ports are not stored (every port a complete check does not
-- list was closed at that time).
CREATE TABLE port_findings (
    check_id       INTEGER NOT NULL REFERENCES port_checks (id),
    port           INTEGER NOT NULL CHECK (port BETWEEN 1 AND 65535),
    proto          TEXT NOT NULL CHECK (proto IN ('tcp', 'udp')),
    service        TEXT NOT NULL DEFAULT '',
    service_source TEXT NOT NULL CHECK (service_source IN ('table', 'banner')),
    PRIMARY KEY (check_id, port, proto)
);

-- Each device's ports, derived from its checks (research R9).
CREATE TABLE device_ports (
    device_id    INTEGER NOT NULL REFERENCES devices (id),
    port         INTEGER NOT NULL,
    proto        TEXT NOT NULL CHECK (proto IN ('tcp', 'udp')),
    service      TEXT NOT NULL DEFAULT '',
    state        TEXT NOT NULL CHECK (state IN ('open', 'closed')),
    ip           TEXT NOT NULL,
    collector_id INTEGER NOT NULL REFERENCES collectors (id),
    first_open   TEXT NOT NULL,
    last_open    TEXT NOT NULL,
    closed_at    TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (device_id, port, proto)
);

-- events gains port_opened / port_closed; the owner facts gain the field "ports" (on / off).
-- SQLite cannot alter a CHECK constraint, so the three tables are rebuilt (nothing references
-- them), as 0008 rebuilt run_sources.
CREATE TABLE events_v10 (
    id           INTEGER PRIMARY KEY,
    at           TEXT NOT NULL,
    device_id    INTEGER NOT NULL REFERENCES devices (id),
    type         TEXT NOT NULL CHECK (type IN ('new_device', 'ip_changed', 'hostname_changed',
                     'mac_changed', 'went_offline', 'came_online', 'merged', 'split',
                     'port_opened', 'port_closed')),
    subnet_id    INTEGER REFERENCES subnets (id),
    old_value    TEXT NOT NULL DEFAULT '',
    new_value    TEXT NOT NULL DEFAULT '',
    collector_id INTEGER REFERENCES collectors (id),
    UNIQUE (device_id, type, at, new_value)
);
INSERT INTO events_v10 SELECT id, at, device_id, type, subnet_id, old_value, new_value, collector_id FROM events;
DROP TABLE events;
ALTER TABLE events_v10 RENAME TO events;
CREATE INDEX events_at ON events (at);

CREATE TABLE user_device_attrs_v10 (
    id           INTEGER PRIMARY KEY,
    identity_key TEXT NOT NULL,
    field        TEXT NOT NULL CHECK (field IN ('name', 'notes', 'type', 'ports')),
    value        TEXT NOT NULL,
    at           TEXT NOT NULL
);
INSERT INTO user_device_attrs_v10 SELECT id, identity_key, field, value, at FROM user_device_attrs;
DROP TABLE user_device_attrs;
ALTER TABLE user_device_attrs_v10 RENAME TO user_device_attrs;

CREATE TABLE user_subnet_attrs_v10 (
    id    INTEGER PRIMARY KEY,
    cidr  TEXT NOT NULL,
    field TEXT NOT NULL CHECK (field IN ('name', 'ignored', 'ports')),
    value TEXT NOT NULL,
    at    TEXT NOT NULL
);
INSERT INTO user_subnet_attrs_v10 SELECT id, cidr, field, value, at FROM user_subnet_attrs;
DROP TABLE user_subnet_attrs;
ALTER TABLE user_subnet_attrs_v10 RENAME TO user_subnet_attrs;
