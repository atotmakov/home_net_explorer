package ingest

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
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

		interval := run.IntervalSeconds
		if _, err := tx.ExecContext(ctx, `UPDATE collectors SET last_report_at = ?, last_clock_skew_ms = ?, last_version = ?,
			interval_seconds = CASE WHEN ? > 0 THEN ? ELSE interval_seconds END WHERE id = ?`,
			store.FormatTime(receivedAt), res.ClockSkewMs, run.Collector.Version, interval, interval, collectorID); err != nil {
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
