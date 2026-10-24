-- Schema v8 (feature 012, access point client lists): the access point behind a Wi-Fi sighting,
-- and access point reads in run_sources.

-- The access point's address for ap_client sightings, '' otherwise (research R8). Part of the
-- sighting tuple (ip, mac, hostname, via, via_source): a move from one access point to the other
-- starts a new sighting, as a move from LAN1 to LAN2 does.
ALTER TABLE sightings ADD COLUMN via_source TEXT NOT NULL DEFAULT '';

-- run_sources gains type 'access_point' and outcome 'controller_managed'. SQLite cannot alter a
-- CHECK constraint, so the table is rebuilt (nothing references it), as in 0004.
CREATE TABLE run_sources_v8 (
    collection_id TEXT NOT NULL REFERENCES collection_runs (collection_id),
    idx           INTEGER NOT NULL,
    type          TEXT NOT NULL CHECK (type IN ('router', 'access_point')),
    model         TEXT NOT NULL,
    address       TEXT NOT NULL,
    subnet        TEXT NOT NULL,
    outcome       TEXT NOT NULL CHECK (outcome IN ('ok', 'unreachable', 'login_rejected', 'locked',
                      'session_busy', 'page_not_understood', 'skipped_after_rejection',
                      'login_unavailable', 'controller_managed')),
    online        INTEGER NOT NULL CHECK (online >= 0),
    offline       INTEGER NOT NULL CHECK (offline >= 0),
    PRIMARY KEY (collection_id, idx)
);
INSERT INTO run_sources_v8 SELECT collection_id, idx, type, model, address, subnet, outcome, online, offline
    FROM run_sources;
DROP TABLE run_sources;
ALTER TABLE run_sources_v8 RENAME TO run_sources;
