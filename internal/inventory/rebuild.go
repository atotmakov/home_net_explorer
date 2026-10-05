package inventory

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/ingest"
	"github.com/atotmakov/home_net_explorer/internal/store"
)

// Rebuild deletes all projections and replays the facts: collection_runs ordered by
// received_at and user facts ordered by at, interleaved, with the scan first on ties — the
// order in which live processing happened (data-model.md "Rebuild invariant"). Callers that
// may race with ingest must hold the ingester lock (see RebuildTx and ingest.Ingester.Do).
func Rebuild(ctx context.Context, s *store.Store, a *Applier) error {
	return s.Tx(ctx, func(tx *sql.Tx) error { return RebuildTx(ctx, tx, a) })
}

type runRef struct {
	id          string
	collectorID int64
	receivedAt  string
}

type userFact struct {
	table string
	at    string
	key   string // identity_key or cidr
	field string
	value string
}

// RebuildTx is Rebuild inside an existing transaction.
func RebuildTx(ctx context.Context, tx *sql.Tx, a *Applier) error {
	if err := store.ResetProjections(ctx, tx); err != nil {
		return err
	}
	runs, err := loadRunRefs(ctx, tx)
	if err != nil {
		return err
	}
	facts, err := loadUserFacts(ctx, tx)
	if err != nil {
		return err
	}
	i, j := 0, 0
	for i < len(runs) || j < len(facts) {
		if j >= len(facts) || (i < len(runs) && runs[i].receivedAt <= facts[j].at) {
			if err := replayRun(ctx, tx, a, runs[i]); err != nil {
				return err
			}
			i++
			continue
		}
		if err := applyFact(ctx, tx, facts[j]); err != nil {
			return err
		}
		j++
	}
	return nil
}

func loadRunRefs(ctx context.Context, tx *sql.Tx) ([]runRef, error) {
	rows, err := tx.QueryContext(ctx, `SELECT collection_id, collector_id, received_at FROM collection_runs ORDER BY received_at, rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []runRef
	for rows.Next() {
		var r runRef
		if err := rows.Scan(&r.id, &r.collectorID, &r.receivedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// loadUserFacts merges the user fact tables in time order (ties: table order, then id).
func loadUserFacts(ctx context.Context, tx *sql.Tx) ([]userFact, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT 'device_attr' AS t, 1 AS o, id, at, identity_key, field, value FROM user_device_attrs
		UNION ALL
		SELECT 'subnet_attr', 2, id, at, cidr, field, value FROM user_subnet_attrs
		ORDER BY at, o, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []userFact
	for rows.Next() {
		var f userFact
		var order, id int64
		if err := rows.Scan(&f.table, &order, &id, &f.at, &f.key, &f.field, &f.value); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func replayRun(ctx context.Context, tx *sql.Tx, a *Applier, r runRef) error {
	var gz []byte
	if err := tx.QueryRowContext(ctx, `SELECT payload_gz FROM collection_runs WHERE collection_id = ?`, r.id).Scan(&gz); err != nil {
		return err
	}
	raw, err := ingest.Gunzip(gz)
	if err != nil {
		return fmt.Errorf("inventory: run %s: %w", r.id, err)
	}
	run, err := contract.Decode(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("inventory: run %s: %w", r.id, err)
	}
	_, err = a.Apply(ctx, tx, r.collectorID, run, store.MustParseTime(r.receivedAt))
	return err
}

func applyFact(ctx context.Context, tx *sql.Tx, f userFact) error {
	switch f.table {
	case "device_attr":
		return applyDeviceAttr(ctx, tx, f.key, f.field, f.value)
	case "subnet_attr":
		return applySubnetAttr(ctx, tx, f.key, f.field, f.value)
	}
	return fmt.Errorf("inventory: unknown fact table %s", f.table)
}
