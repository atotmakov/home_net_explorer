package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver ("sqlite")
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type migration struct {
	version int
	name    string
	sql     string
}

var migrations = loadMigrations()

func loadMigrations() []migration {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		panic(err)
	}
	var ms []migration
	for _, e := range entries {
		num, _, ok := strings.Cut(e.Name(), "_")
		v, err := strconv.Atoi(num)
		if !ok || err != nil {
			panic("store: bad migration file name " + e.Name())
		}
		b, err := migrationFS.ReadFile(path.Join("migrations", e.Name()))
		if err != nil {
			panic(err)
		}
		ms = append(ms, migration{version: v, name: e.Name(), sql: string(b)})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].version < ms[j].version })
	for i, m := range ms {
		if m.version != i+1 {
			panic(fmt.Sprintf("store: migrations must be numbered 1..n, found %s", m.name))
		}
	}
	return ms
}

// LatestVersion is the schema version after all migrations.
func LatestVersion() int { return len(migrations) }

// Store wraps the SQLite database.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the database at path and applies all migrations.
func Open(path string) (*Store, error) {
	return OpenAt(path, LatestVersion())
}

// OpenAt opens the database and migrates it only up to version (used by the migration harness).
func OpenAt(path string, version int) (*Store, error) {
	q := url.Values{}
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(10000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Set("_txlock", "immediate")
	db, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.MigrateTo(context.Background(), version); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// DB exposes the underlying database.
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Version returns the schema version (PRAGMA user_version).
func (s *Store) Version(ctx context.Context) (int, error) {
	var v int
	err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v)
	return v, err
}

// MigrateTo applies migrations up to and including version, each in its own transaction.
func (s *Store) MigrateTo(ctx context.Context, version int) error {
	if version < 0 || version > LatestVersion() {
		return fmt.Errorf("store: unknown schema version %d", version)
	}
	cur, err := s.Version(ctx)
	if err != nil {
		return err
	}
	for _, m := range migrations {
		if m.version <= cur || m.version > version {
			continue
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			tx.Rollback()
			return fmt.Errorf("store: migration %s: %w", m.name, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", m.version)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// Tx runs f in a transaction, committing on success.
func (s *Store) Tx(ctx context.Context, f func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := f(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

const timeLayout = "2006-01-02T15:04:05.000Z"

// FormatTime renders t as UTC RFC 3339 with milliseconds (lexically sortable).
func FormatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

// ParseTime parses a FormatTime string. The empty string gives the zero time.
func ParseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(timeLayout, s)
}

// MustParseTime is ParseTime for values read back from the database.
func MustParseTime(s string) time.Time {
	t, err := ParseTime(s)
	if err != nil {
		panic(err)
	}
	return t
}
