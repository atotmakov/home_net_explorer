package store_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/atotmakov/home_net_explorer/internal/store"
)

var expectedTables = []string{
	// facts
	"collectors", "collection_runs", "run_subnets", "user_device_attrs", "user_identity_alias",
	"user_links", "user_acks", "user_subnet_attrs", "settings", "sessions", "run_sources",
	"router_settings",
	// projections
	"subnets", "sightings", "devices", "device_addresses", "events", "links",
}

func pragma(t *testing.T, s *store.Store, name string) string {
	t.Helper()
	var v string
	if err := s.DB().QueryRow("PRAGMA " + name).Scan(&v); err != nil {
		t.Fatalf("PRAGMA %s: %v", name, err)
	}
	return v
}

func TestOpenCreatesSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hne.db")
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := pragma(t, s, "journal_mode"); got != "wal" {
		t.Errorf("journal_mode = %q, want wal", got)
	}
	if got := pragma(t, s, "foreign_keys"); got != "1" {
		t.Errorf("foreign_keys = %q, want 1", got)
	}
	if got, _ := strconv.Atoi(pragma(t, s, "busy_timeout")); got < 5000 {
		t.Errorf("busy_timeout = %d, want >= 5000", got)
	}
	for _, name := range expectedTables {
		var n int
		if err := s.DB().QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n); err != nil || n != 1 {
			t.Errorf("table %s missing (n=%d, err=%v)", name, n, err)
		}
	}
	v1, err := s.Version(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v1 != store.LatestVersion() {
		t.Errorf("user_version = %d, want %d", v1, store.LatestVersion())
	}
	s.Close()

	s2, err := store.Open(path) // second open must be a no-op migration
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if v2, _ := s2.Version(context.Background()); v2 != v1 {
		t.Errorf("user_version after reopen = %d, want %d", v2, v1)
	}
}

// TestMigrationHarness: for every migration k, a database seeded with sample rows at version
// k-1 migrates to k with row counts preserved (Constitution, Development Workflow). Seeds live
// in testdata/seed_<k-1>.sql; version 0 is the empty database.
func TestMigrationHarness(t *testing.T) {
	ctx := context.Background()
	for k := 1; k <= store.LatestVersion(); k++ {
		path := filepath.Join(t.TempDir(), "m.db")
		s, err := store.OpenAt(path, k-1)
		if err != nil {
			t.Fatalf("open at %d: %v", k-1, err)
		}
		if seed, err := os.ReadFile(filepath.Join("testdata", "seed_"+strconv.Itoa(k-1)+".sql")); err == nil {
			if _, err := s.DB().ExecContext(ctx, string(seed)); err != nil {
				t.Fatalf("seed %d: %v", k-1, err)
			}
		}
		before := rowCounts(t, s)
		if err := s.MigrateTo(ctx, k); err != nil {
			t.Fatalf("migrate %d -> %d: %v", k-1, k, err)
		}
		after := rowCounts(t, s)
		for table, n := range before {
			if after[table] != n {
				t.Errorf("migration %d: table %s rows %d -> %d", k, table, n, after[table])
			}
		}
		if v, _ := s.Version(ctx); v != k {
			t.Errorf("after migration %d: user_version = %d", k, v)
		}
		s.Close()
	}
}

func rowCounts(t *testing.T, s *store.Store) map[string]int {
	t.Helper()
	rows, err := s.DB().Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	rows.Close()
	counts := map[string]int{}
	for _, n := range names {
		var c int
		if err := s.DB().QueryRow(`SELECT count(*) FROM "` + n + `"`).Scan(&c); err != nil {
			t.Fatal(err)
		}
		counts[n] = c
	}
	return counts
}

// TestMigration0003 (feature 002): seeded v2 data survives, sightings gain via = ”, run_subnets
// accepts router_table (and still rejects unknown methods), and run_sources is keyed by
// (collection_id, idx).
func TestMigration0003(t *testing.T) {
	ctx := context.Background()
	s, err := store.OpenAt(filepath.Join(t.TempDir(), "m.db"), 2)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	seed, err := os.ReadFile(filepath.Join("testdata", "seed_2.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, string(seed)); err != nil {
		t.Fatal(err)
	}
	if err := s.MigrateTo(ctx, 3); err != nil {
		t.Fatal(err)
	}
	db := s.DB()
	var via string
	if err := db.QueryRow(`SELECT via FROM sightings WHERE id = 1`).Scan(&via); err != nil || via != "" {
		t.Errorf("sightings.via = %q, %v; want empty", via, err)
	}
	var reason string
	if err := db.QueryRow(`SELECT skip_reason FROM run_subnets WHERE cidr = '10.0.0.0/16'`).Scan(&reason); err != nil || reason != "too_large" {
		t.Errorf("run_subnets skipped row = %q, %v", reason, err)
	}
	const id = "3f2b8c1e-5d4a-4c3b-9a1f-0e2d4c6b8a01"
	if _, err := db.Exec(`INSERT INTO run_subnets (collection_id, cidr, method, complete, hosts_probed)
		VALUES (?, '192.168.0.0/24', 'router_table', 1, 30)`, id); err != nil {
		t.Errorf("run_subnets rejects router_table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO run_subnets (collection_id, cidr, method, complete, hosts_probed)
		VALUES (?, '192.168.7.0/24', 'snmp', 1, 30)`, id); err == nil {
		t.Error("run_subnets accepted an unknown method")
	}
	ins := `INSERT INTO run_sources (collection_id, idx, type, model, address, subnet, outcome, online, offline)
		VALUES (?, ?, 'router', 'huawei-hg8145v5', '192.168.0.1', '192.168.0.0/24', ?, 14, 16)`
	if _, err := db.Exec(ins, id, 0, "ok"); err != nil {
		t.Fatalf("insert run_sources: %v", err)
	}
	if _, err := db.Exec(ins, id, 0, "ok"); err == nil {
		t.Error("run_sources accepted a duplicate (collection_id, idx)")
	}
	if _, err := db.Exec(ins, id, 1, "rebooted"); err == nil {
		t.Error("run_sources accepted an unknown outcome")
	}
}

// TestMigration0004 (feature 003): seeded v3 data survives, run_sources accepts the outcome
// login_unavailable, and router_settings exists with one row per device identity.
func TestMigration0004(t *testing.T) {
	ctx := context.Background()
	s, err := store.OpenAt(filepath.Join(t.TempDir(), "m.db"), 3)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	seed, err := os.ReadFile(filepath.Join("testdata", "seed_3.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, string(seed)); err != nil {
		t.Fatal(err)
	}
	if err := s.MigrateTo(ctx, 4); err != nil {
		t.Fatal(err)
	}
	db := s.DB()
	var outcome string
	if err := db.QueryRow(`SELECT outcome FROM run_sources WHERE idx = 0`).Scan(&outcome); err != nil || outcome != "ok" {
		t.Errorf("seeded run_sources row = %q, %v", outcome, err)
	}
	const id = "3f2b8c1e-5d4a-4c3b-9a1f-0e2d4c6b8a01"
	ins := `INSERT INTO run_sources (collection_id, idx, type, model, address, subnet, outcome, online, offline)
		VALUES (?, ?, 'router', 'huawei-hg8145v5', '192.168.0.1', '192.168.0.0/24', ?, 0, 0)`
	if _, err := db.Exec(ins, id, 1, "login_unavailable"); err != nil {
		t.Errorf("run_sources rejects login_unavailable: %v", err)
	}
	if _, err := db.Exec(ins, id, 2, "rebooted"); err == nil {
		t.Error("run_sources accepted an unknown outcome")
	}
	rs := `INSERT INTO router_settings (identity_key, model, subnet, username, password, updated_at)
		VALUES ('mac:00:00:5e:10:00:01', 'huawei-hg8145v5', '', 'root', 'pw', '2026-10-10T10:00:00.000Z')`
	if _, err := db.Exec(rs); err != nil {
		t.Fatalf("insert router_settings: %v", err)
	}
	if _, err := db.Exec(rs); err == nil {
		t.Error("router_settings accepted a second row for the same device")
	}
	if _, err := db.Exec(`INSERT INTO router_settings (identity_key, model, username, password, updated_at)
		VALUES ('mac:00:00:5e:10:00:09', 'huawei-hg8145v5', 'root', '', '2026-10-10T10:00:00.000Z')`); err == nil {
		t.Error("router_settings accepted an empty password")
	}
}

// TestMigration0005 (feature 004): collectors is rebuilt with deleted_at and names unique among
// active collectors only; every row, id and reference survives.
func TestMigration0005(t *testing.T) {
	ctx := context.Background()
	s, err := store.OpenAt(filepath.Join(t.TempDir(), "m.db"), 4)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	seed, err := os.ReadFile(filepath.Join("testdata", "seed_4.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, string(seed)); err != nil {
		t.Fatal(err)
	}
	if err := s.MigrateTo(ctx, 5); err != nil {
		t.Fatal(err)
	}
	db := s.DB()
	if got := pragma(t, s, "foreign_keys"); got != "1" {
		t.Errorf("foreign_keys = %q after the migration, want 1", got)
	}
	if r, err := db.Query(`PRAGMA foreign_key_check`); err != nil {
		t.Fatal(err)
	} else {
		if r.Next() {
			t.Error("foreign_key_check reports dangling references after migration 0005")
		}
		r.Close()
	}
	var name, revoked, version string
	var token []byte
	var skew int
	if err := db.QueryRow(`SELECT name, revoked_at, token_hash, last_clock_skew_ms, last_version FROM collectors WHERE id = 3 AND deleted_at IS NULL`).
		Scan(&name, &revoked, &token, &skew, &version); err != nil {
		t.Fatal(err)
	}
	if name != "laptop" || revoked != "2026-10-06T10:00:00.000Z" || len(token) != 2 || skew != 1500 || version != "0.4.63" {
		t.Errorf("collector 3 = %s %s %x %d %s", name, revoked, token, skew, version)
	}
	ins := `INSERT INTO collectors (name, kind, created_at) VALUES (?, 'remote', '2026-10-10T10:00:00.000Z')`
	if _, err := db.Exec(ins, "desktop"); err == nil {
		t.Error("a second active collector named desktop was accepted")
	}
	if _, err := db.Exec(`UPDATE collectors SET deleted_at = '2026-10-10T10:00:00.000Z' WHERE name = 'desktop'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ins, "desktop"); err != nil {
		t.Errorf("the name of a removed collector cannot be reused: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO collectors (name, kind, created_at) VALUES ('Bad Name', 'remote', '2026-10-10T10:00:00.000Z')`); err == nil {
		t.Error("the name CHECK was lost")
	}
}
