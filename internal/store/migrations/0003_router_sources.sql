-- Schema v3 (feature 002, router device lists): router_table scans, sightings.via and
-- run_sources (the outcome of each router read, shown on the Collectors page).

-- run_subnets.method gains 'router_table'. SQLite cannot alter a CHECK constraint, so the
-- table is rebuilt (nothing references it).
CREATE TABLE run_subnets_v3 (
    collection_id TEXT NOT NULL REFERENCES collection_runs (collection_id),
    cidr          TEXT NOT NULL,
    method        TEXT NOT NULL CHECK (method IN ('arp', 'icmp_tcp', 'skipped', 'router_table')),
    skip_reason   TEXT CHECK (skip_reason IN ('too_large', 'ignored')),
    complete      INTEGER NOT NULL,
    hosts_probed  INTEGER NOT NULL,
    PRIMARY KEY (collection_id, cidr)
);
INSERT INTO run_subnets_v3 (collection_id, cidr, method, skip_reason, complete, hosts_probed)
    SELECT collection_id, cidr, method, skip_reason, complete, hosts_probed FROM run_subnets;
DROP TABLE run_subnets;
ALTER TABLE run_subnets_v3 RENAME TO run_subnets;
CREATE INDEX run_subnets_cidr ON run_subnets (cidr);

-- Part of the sighting tuple (ip, mac, hostname, via): a move from LAN1 to LAN2 starts a new
-- sighting.
ALTER TABLE sightings ADD COLUMN via TEXT NOT NULL DEFAULT '';

CREATE TABLE run_sources (
    collection_id TEXT NOT NULL REFERENCES collection_runs (collection_id),
    idx           INTEGER NOT NULL,
    type          TEXT NOT NULL CHECK (type IN ('router')),
    model         TEXT NOT NULL,
    address       TEXT NOT NULL,
    subnet        TEXT NOT NULL,
    outcome       TEXT NOT NULL CHECK (outcome IN ('ok', 'unreachable', 'login_rejected',
                      'locked', 'session_busy', 'page_not_understood', 'skipped_after_rejection')),
    online        INTEGER NOT NULL CHECK (online >= 0),
    offline       INTEGER NOT NULL CHECK (offline >= 0),
    PRIMARY KEY (collection_id, idx)
);
