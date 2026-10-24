package store

import (
	"context"
	"path/filepath"
	"testing"
)

// Feature 004: a migration marked "-- hne:foreign-keys-off" runs with foreign keys off, is rolled
// back when it leaves a dangling reference, and foreign keys are on again afterwards.
func TestForeignKeysOffMigration(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	fkOn := func() bool {
		var v int
		if err := s.db.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v == 1
	}
	before, _ := s.Version(ctx)

	bad := migration{version: before + 1, name: "bad.sql", sql: foreignKeysOff + `
INSERT INTO collection_runs (collection_id, collector_id, schema_version, started_at, finished_at, sent_at, received_at, payload_gz)
VALUES ('x', 999, 1, 'a', 'a', 'a', 'a', x'00');`}
	if err := s.apply(ctx, bad); err == nil {
		t.Error("a migration leaving a dangling reference was committed")
	}
	var n int
	s.db.QueryRowContext(ctx, `SELECT count(*) FROM collection_runs`).Scan(&n)
	if n != 0 {
		t.Error("the failed migration was not rolled back")
	}
	if v, _ := s.Version(ctx); v != before {
		t.Errorf("user_version = %d after a failed migration, want %d", v, before)
	}
	if !fkOn() {
		t.Error("foreign keys stay off after a failed migration")
	}

	// A rebuild of a referenced table succeeds with foreign keys off.
	good := migration{version: before + 1, name: "good.sql", sql: foreignKeysOff + `
INSERT INTO collectors (id, name, kind, created_at) VALUES (1, 'nas', 'builtin', 'a');
INSERT INTO collection_runs (collection_id, collector_id, schema_version, started_at, finished_at, sent_at, received_at, payload_gz)
VALUES ('x', 1, 1, 'a', 'a', 'a', 'a', x'00');
CREATE TABLE collectors_tmp (id INTEGER PRIMARY KEY, name TEXT NOT NULL, kind TEXT NOT NULL, created_at TEXT NOT NULL);
INSERT INTO collectors_tmp SELECT id, name, kind, created_at FROM collectors;
DROP TABLE collectors;
ALTER TABLE collectors_tmp RENAME TO collectors;`}
	if err := s.apply(ctx, good); err != nil {
		t.Fatalf("rebuilding a referenced table: %v", err)
	}
	if v, _ := s.Version(ctx); v != before+1 {
		t.Errorf("user_version = %d, want %d", v, before+1)
	}
	if !fkOn() {
		t.Error("foreign keys stay off after the migration")
	}
}

// Feature 011: schema 9 holds the owner's manual devices (facts) and the devices.manual flag.
func TestManualDevicesSchema(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if v, _ := s.Version(ctx); v < 9 {
		t.Fatalf("schema version = %d, want >= 9", v)
	}
	ins := `INSERT INTO user_manual_devices (identity_key, field, value, at) VALUES ('manual:x', ?, '', 'a')`
	for _, f := range []string{"add", "mac", "remove"} {
		if _, err := s.db.ExecContext(ctx, ins, f); err != nil {
			t.Errorf("field %s refused: %v", f, err)
		}
	}
	if _, err := s.db.ExecContext(ctx, ins, "name"); err == nil {
		t.Error("field name accepted in user_manual_devices")
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO devices (identity_key, identity_strength, status, first_seen, last_seen, display_name)
		VALUES ('mac:x', 'strong', 'online', 'a', 'a', 'x')`); err != nil {
		t.Fatal(err)
	}
	var manual int
	if err := s.db.QueryRowContext(ctx, `SELECT manual FROM devices WHERE identity_key = 'mac:x'`).Scan(&manual); err != nil || manual != 0 {
		t.Errorf("devices.manual = %d, %v; want default 0", manual, err)
	}
}
