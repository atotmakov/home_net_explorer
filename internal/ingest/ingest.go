package ingest

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/store"
)

// Applier folds a stored run into the projections, inside the ingest transaction. It returns
// the subnets that this run made known for the first time.
type Applier interface {
	Apply(ctx context.Context, tx *sql.Tx, collectorID int64, run *contract.CollectionRun, receivedAt time.Time) (newSubnets []string, err error)
}

// Ingester stores runs and applies them. Ingest calls are serialized, so processing order is
// received_at order, the same order a rebuild replays (data-model.md "Rebuild invariant").
type Ingester struct {
	mu      sync.Mutex
	store   *store.Store
	applier Applier
}

// New returns an Ingester.
func New(s *store.Store, a Applier) *Ingester {
	return &Ingester{store: s, applier: a}
}

// Do runs f in a transaction serialized with ingest. User facts are written through Do so
// their order relative to runs is the order a rebuild replays.
func (in *Ingester) Do(ctx context.Context, f func(tx *sql.Tx) error) error {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.store.Tx(ctx, f)
}

// Ingest stores the run unchanged (raw body gzip-compressed, plus its run_subnets) and applies
// it. A run whose collection_id is already stored is a no-op that reports "duplicate".
// Ingest never filters observations: ignored subnets are skipped only by the Applier.
func (in *Ingester) Ingest(ctx context.Context, collectorID int64, raw []byte, run *contract.CollectionRun, receivedAt time.Time) (contract.UploadResult, error) {
	in.mu.Lock()
	defer in.mu.Unlock()

	res := contract.UploadResult{
		CollectionID: run.CollectionID,
		ClockSkewMs:  run.SentAt.Sub(receivedAt).Milliseconds(),
	}
	payload, err := gzipBytes(raw)
	if err != nil {
		return res, err
	}

	err = in.store.Tx(ctx, func(tx *sql.Tx) error {
		discard, err := beforeReset(ctx, tx, run)
		if err != nil {
			return err
		}
		if discard {
			res.Status = contract.StatusDiscarded
			return touchCollector(ctx, tx, collectorID, run, receivedAt, res.ClockSkewMs)
		}
		r, err := tx.ExecContext(ctx, `INSERT INTO collection_runs
			(collection_id, collector_id, schema_version, started_at, finished_at, sent_at, received_at, interval_seconds, payload_gz)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (collection_id) DO NOTHING`,
			run.CollectionID, collectorID, run.SchemaVersion, store.FormatTime(run.StartedAt),
			store.FormatTime(run.FinishedAt), store.FormatTime(run.SentAt), store.FormatTime(receivedAt), run.IntervalSeconds, payload)
		if err != nil {
			return err
		}
		if n, _ := r.RowsAffected(); n == 0 {
			res.Status = contract.StatusDuplicate
			return nil
		}
		res.Status = contract.StatusStored

		for _, s := range run.Subnets {
			var reason any
			if s.SkipReason != "" {
				reason = s.SkipReason
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO run_subnets
				(collection_id, cidr, method, skip_reason, complete, hosts_probed) VALUES (?, ?, ?, ?, ?, ?)`,
				run.CollectionID, s.CIDR, s.Method, reason, s.Complete, s.HostsProbed); err != nil {
				return err
			}
		}
		for i, s := range run.Sources { // router read outcomes (feature 002, FR-013)
			if _, err := tx.ExecContext(ctx, `INSERT INTO run_sources
				(collection_id, idx, type, model, address, subnet, outcome, online, offline) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				run.CollectionID, i, s.Type, s.Model, s.Address, s.Subnet, s.Outcome, s.Online, s.Offline); err != nil {
				return err
			}
		}

		if err := touchCollector(ctx, tx, collectorID, run, receivedAt, res.ClockSkewMs); err != nil {
			return err
		}

		newSubnets, err := in.applier.Apply(ctx, tx, collectorID, run, receivedAt)
		if err != nil {
			return err
		}
		res.NewSubnets = newSubnets
		return nil
	})
	return res, err
}

// touchCollector records that the collector reported (time, clock skew, build, interval).
func touchCollector(ctx context.Context, tx *sql.Tx, collectorID int64, run *contract.CollectionRun, receivedAt time.Time, skewMs int64) error {
	interval := run.IntervalSeconds
	_, err := tx.ExecContext(ctx, `UPDATE collectors SET last_report_at = ?, last_clock_skew_ms = ?, last_version = ?,
		interval_seconds = CASE WHEN ? > 0 THEN ? ELSE interval_seconds END WHERE id = ?`,
		store.FormatTime(receivedAt), skewMs, run.Collector.Version, interval, interval, collectorID)
	return err
}

// beforeReset reports whether the run started before the owner's latest data reset ("remove all
// devices" or "drop all data", feature 004). Such a run, e.g. spooled by a collector while the
// server was unreachable, must not bring removed devices back. The run's own started_at is used
// without skew correction: sent_at is set before spooling, so sent_at - received_at measures
// the spool delay, not the clock error (research R2).
func beforeReset(ctx context.Context, tx *sql.Tx, run *contract.CollectionRun) (bool, error) {
	var v string
	err := tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, store.SettingDataResetAt).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	reset, err := store.ParseTime(v)
	if err != nil {
		return false, err
	}
	return run.StartedAt.Before(reset), nil
}

func gzipBytes(b []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(b); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Gunzip decompresses a stored payload.
func Gunzip(b []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	var out bytes.Buffer
	if _, err := out.ReadFrom(zr); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
