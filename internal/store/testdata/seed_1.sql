-- Sample rows at schema v1, used by TestMigrationHarness to check that migration 0002 keeps them.
INSERT INTO collectors (id, name, kind, created_at, interval_seconds, last_report_at, last_clock_skew_ms)
VALUES (1, 'nas', 'builtin', '2026-10-05T10:00:00.000Z', 900, '2026-10-05T10:01:01.000Z', -2000),
       (2, 'desktop', 'remote', '2026-10-05T10:00:00.000Z', 900, NULL, NULL);
INSERT INTO collection_runs (collection_id, collector_id, schema_version, started_at, finished_at, sent_at, received_at, interval_seconds, payload_gz)
VALUES ('3f2b8c1e-5d4a-4c3b-9a1f-0e2d4c6b8a01', 1, 1, '2026-10-05T10:00:00.000Z', '2026-10-05T10:01:00.000Z', '2026-10-05T10:01:01.000Z', '2026-10-05T10:01:03.000Z', 900, x'00');
INSERT INTO run_subnets (collection_id, cidr, method, skip_reason, complete, hosts_probed)
VALUES ('3f2b8c1e-5d4a-4c3b-9a1f-0e2d4c6b8a01', '192.168.1.0/24', 'arp', NULL, 1, 254);
INSERT INTO settings (key, value) VALUES ('offline_multiplier', '3');
INSERT INTO subnets (id, cidr, first_seen_at, discovered_by) VALUES (1, '192.168.1.0/24', '2026-10-05T10:00:00.000Z', 1);
INSERT INTO devices (id, identity_key, identity_strength, mac, status, first_seen, last_seen, display_name)
VALUES (1, 'mac:00:11:32:aa:bb:cc', 'strong', '00:11:32:aa:bb:cc', 'online', '2026-10-05T10:00:10.000Z', '2026-10-05T10:00:10.000Z', 'nas.lan');
