package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Collector kinds.
const (
	KindBuiltin = "builtin"
	KindRemote  = "remote"
)

// Collector is a source of observations.
type Collector struct {
	ID              int64
	Name            string
	Kind            string
	TokenHash       []byte
	CreatedAt       time.Time
	RevokedAt       time.Time // zero when active
	IntervalSeconds int
	LastReportAt    time.Time // zero when never reported
	LastClockSkewMs int64
}

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("store: not found")

// EnsureCollector returns the id of the collector with this name, creating it if needed.
func (s *Store) EnsureCollector(ctx context.Context, name, kind string, interval int) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM collectors WHERE name = ?`, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO collectors (name, kind, created_at, interval_seconds) VALUES (?, ?, ?, ?)`,
		name, kind, FormatTime(time.Now()), interval)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

const collectorCols = `id, name, kind, token_hash, created_at, COALESCE(revoked_at, ''),
	interval_seconds, COALESCE(last_report_at, ''), COALESCE(last_clock_skew_ms, 0)`

func scanCollector(row interface{ Scan(...any) error }) (Collector, error) {
	var c Collector
	var created, revoked, last string
	if err := row.Scan(&c.ID, &c.Name, &c.Kind, &c.TokenHash, &created, &revoked,
		&c.IntervalSeconds, &last, &c.LastClockSkewMs); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c, ErrNotFound
		}
		return c, err
	}
	c.CreatedAt = MustParseTime(created)
	c.RevokedAt = MustParseTime(revoked)
	c.LastReportAt = MustParseTime(last)
	return c, nil
}

// GetCollector loads a collector by id.
func (s *Store) GetCollector(ctx context.Context, id int64) (Collector, error) {
	return scanCollector(s.db.QueryRowContext(ctx, `SELECT `+collectorCols+` FROM collectors WHERE id = ?`, id))
}

// CollectorByName loads a collector by name.
func (s *Store) CollectorByName(ctx context.Context, name string) (Collector, error) {
	return scanCollector(s.db.QueryRowContext(ctx, `SELECT `+collectorCols+` FROM collectors WHERE name = ?`, name))
}

// ListCollectors returns all collectors ordered by name.
func (s *Store) ListCollectors(ctx context.Context) ([]Collector, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+collectorCols+` FROM collectors ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Collector
	for rows.Next() {
		c, err := scanCollector(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Setting reads a settings value; ok is false when unset.
func (s *Store) Setting(ctx context.Context, key string) (value string, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return value, err == nil, err
}

// SetSetting writes a settings value.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`,
		key, value)
	return err
}

// SetDefaultSetting writes a settings value only if it is unset.
func (s *Store) SetDefaultSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO NOTHING`, key, value)
	return err
}
