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
	"user_links", "user_acks", "user_subnet_attrs", "settings", "sessions",
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
