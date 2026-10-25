package inventory

import (
	"context"
	"database/sql"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/store"
)

// evaluateOffline applies the offline rule of feature 006 (research R1) after completed scan R
// of subnet sn: a device with a current address in sn that R did not see goes offline when
//   - its last_seen is before R.started_at (a late upload of an older scan changes nothing), and
//   - it is not current in another subnet whose latest complete scan saw it (that scan, among
//     those ingested no later than R and finished no later than R, started at or before the
//     device's last_seen).
//
// It reads only stored scans ingested no later than R (rowid <= runRow), so a rebuild reaches
// the same decisions; it never reads the wall clock.
func evaluateOffline(ctx context.Context, tx *sql.Tx, run *contract.CollectionRun, runRow, collectorID int64, sn *scannedSubnet, seen map[int64]bool) error {
	rStarted := store.FormatTime(run.StartedAt.UTC())
	rFinished := store.FormatTime(run.FinishedAt.UTC())

	rows, err := tx.QueryContext(ctx, `SELECT d.id, d.last_seen FROM devices d
		JOIN device_addresses a ON a.device_id = d.id
		WHERE a.subnet_id = ? AND a.current = 1 AND d.status IN ('online', 'new') AND d.last_seen < ?`, sn.id, rStarted)
	if err != nil {
		return err
	}
	type candidate struct {
		id       int64
		lastSeen string
	}
	var cands []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.lastSeen); err != nil {
			rows.Close()
			return err
		}
		if !seen[c.id] {
			cands = append(cands, c)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, c := range cands {
		var elsewhere int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM device_addresses a JOIN subnets s ON s.id = a.subnet_id
			WHERE a.device_id = ? AND a.current = 1 AND a.subnet_id != ? AND s.ignored = 0
			AND (SELECT r.started_at FROM collection_runs r JOIN run_subnets rs ON rs.collection_id = r.collection_id
			     WHERE rs.cidr = s.cidr AND rs.complete = 1 AND r.rowid <= ? AND r.finished_at <= ?
			     ORDER BY r.finished_at DESC, r.rowid DESC LIMIT 1) <= ?`,
			c.id, sn.id, runRow, rFinished, c.lastSeen).Scan(&elsewhere); err != nil {
			return err
		}
		if elsewhere > 0 {
			continue
		}
		next := StatusOffline
		var cur string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM devices WHERE id = ?`, c.id).Scan(&cur); err != nil {
			return err
		}
		if cur == StatusNew {
			next = StatusNewOffline
		}
		if _, err := tx.ExecContext(ctx, `UPDATE devices SET status = ? WHERE id = ?`, next, c.id); err != nil {
			return err
		}
		if err := addEvent(ctx, tx, rFinished, c.id, EventWentOffline, sn.id, "", "", collectorID); err != nil {
			return err
		}
	}
	return nil
}

// endMovedAddresses ends the current address on subnet sn of each device the complete scan did
// not see there but that has a current address on another subnet seen after its address on sn: it
// moved (a Dune player that left 192.168.1.0/24 for 192.168.0.0/24 kept both addresses and was
// drawn in two subnets). A device seen nowhere since keeps its last address (it goes offline).
func endMovedAddresses(ctx context.Context, tx *sql.Tx, sn *scannedSubnet, seen map[int64]bool) error {
	rows, err := tx.QueryContext(ctx, `SELECT a.device_id FROM device_addresses a
		WHERE a.subnet_id = ? AND a.current = 1 AND EXISTS (
			SELECT 1 FROM device_addresses b JOIN subnets s ON s.id = b.subnet_id
			WHERE b.device_id = a.device_id AND b.current = 1 AND b.subnet_id != a.subnet_id
			AND s.ignored = 0 AND b.as_of > a.as_of)`, sn.id)
	if err != nil {
		return err
	}
	var moved []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		if !seen[id] {
			moved = append(moved, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range moved {
		if _, err := tx.ExecContext(ctx, `UPDATE device_addresses SET current = 0 WHERE device_id = ? AND subnet_id = ?`, id, sn.id); err != nil {
			return err
		}
		if err := refreshDisplayName(ctx, tx, id); err != nil {
			return err
		}
	}
	return nil
}
