-- Schema v6 (feature 005, faked-answers defence): run_subnets.skip_reason gains 'faked_answers'
-- (the network path answered for every address). SQLite cannot alter a CHECK constraint, so the
-- table is rebuilt (nothing references it).
CREATE TABLE run_subnets_v6 (
    collection_id TEXT NOT NULL REFERENCES collection_runs (collection_id),
    cidr          TEXT NOT NULL,
    method        TEXT NOT NULL CHECK (method IN ('arp', 'icmp_tcp', 'skipped', 'router_table')),
    skip_reason   TEXT CHECK (skip_reason IN ('too_large', 'ignored', 'faked_answers')),
    complete      INTEGER NOT NULL,
    hosts_probed  INTEGER NOT NULL,
    PRIMARY KEY (collection_id, cidr)
);
INSERT INTO run_subnets_v6 (collection_id, cidr, method, skip_reason, complete, hosts_probed)
    SELECT collection_id, cidr, method, skip_reason, complete, hosts_probed FROM run_subnets;
DROP TABLE run_subnets;
ALTER TABLE run_subnets_v6 RENAME TO run_subnets;
CREATE INDEX run_subnets_cidr ON run_subnets (cidr);
