package inventory

import (
	"context"
	"database/sql"
	"sort"
	"strconv"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/store"
)

// DefaultOfflineMultiplier is used when the offline_multiplier setting is unset.
const DefaultOfflineMultiplier = 3

// OfflineMultiplier reads the offline_multiplier setting.
func OfflineMultiplier(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) int {
	var v string
	if err := q.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'offline_multiplier'`).Scan(&v); err != nil {
		return DefaultOfflineMultiplier
	}
	if n, err := strconv.Atoi(v); err == nil && n >= 1 {
		return n
	}
	return DefaultOfflineMultiplier
}

type candidate struct {
	id       int64
	lastSeen time.Time
	status   string
}

// evaluateOffline applies the rule of research R7 after completed scan R of subnet sn. It reads
// only stored scans that were ingested no later than R (rowid <= runRow) and finished no later
// than R, so a rebuild reaches exactly the same decisions; it never reads the wall clock.
func evaluateOffline(ctx context.Context, tx *sql.Tx, run *contract.CollectionRun, runRow, collectorID int64, sn *scannedSubnet, seen map[int64]bool) error {
	mult := OfflineMultiplier(ctx, tx)
	rFinished := run.FinishedAt.UTC()
	rFinishedS := store.FormatTime(rFinished)

	rows, err := tx.QueryContext(ctx, `SELECT d.id, d.last_seen, d.status FROM devices d
		JOIN device_addresses a ON a.device_id = d.id
		WHERE a.subnet_id = ? AND a.current = 1 AND d.status IN ('online', 'new')`, sn.id)
	if err != nil {
		return err
	}
	var cands []candidate
	for rows.Next() {
		var c candidate
		var ls string
		if err := rows.Scan(&c.id, &ls, &c.status); err != nil {
			rows.Close()
			return err
		}
		if seen[c.id] {
			continue
		}
		c.lastSeen = store.MustParseTime(ls)
		// Condition (2): R.finished_at − last_seen ≥ mult × the interval of R's collector.
		if rFinished.Sub(c.lastSeen) < time.Duration(mult*run.IntervalSeconds)*time.Second {
			continue
		}
		cands = append(cands, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(cands) == 0 {
		return err
	}

	// Active collectors for S at R.finished_at: latest completed scan within mult × interval.
	type active struct {
		id     int64
		starts []time.Time // started_at of completed scans of S after the oldest candidate's last_seen
	}
	var actives []*active
	arows, err := tx.QueryContext(ctx, `SELECT r.collector_id, MAX(r.finished_at), r.interval_seconds
		FROM collection_runs r JOIN run_subnets s ON s.collection_id = r.collection_id
		WHERE s.cidr = ? AND s.complete = 1 AND r.rowid <= ? AND r.finished_at <= ?
		GROUP BY r.collector_id`, sn.scan.CIDR, runRow, rFinishedS)
	if err != nil {
		return err
	}
	for arows.Next() {
		var cid int64
		var fin string
		var interval int
		if err := arows.Scan(&cid, &fin, &interval); err != nil {
			arows.Close()
			return err
		}
		window := time.Duration(mult*interval) * time.Second
		if rFinished.Sub(store.MustParseTime(fin)) <= window {
			actives = append(actives, &active{id: cid})
		}
	}
	arows.Close()
	if err := arows.Err(); err != nil {
		return err
	}

	oldest := cands[0].lastSeen
	for _, c := range cands {
		if c.lastSeen.Before(oldest) {
			oldest = c.lastSeen
		}
	}
	for _, ac := range actives {
		srows, err := tx.QueryContext(ctx, `SELECT r.started_at FROM collection_runs r
			JOIN run_subnets s ON s.collection_id = r.collection_id
			WHERE s.cidr = ? AND s.complete = 1 AND r.collector_id = ? AND r.rowid <= ?
			AND r.finished_at <= ? AND r.started_at > ?`, sn.scan.CIDR, ac.id, runRow, rFinishedS, store.FormatTime(oldest))
		if err != nil {
			return err
		}
		for srows.Next() {
			var s string
			if err := srows.Scan(&s); err != nil {
				srows.Close()
				return err
			}
			ac.starts = append(ac.starts, store.MustParseTime(s))
		}
		srows.Close()
		sort.Slice(ac.starts, func(i, j int) bool { return ac.starts[i].Before(ac.starts[j]) })
	}

	for _, c := range cands {
		// Condition (1): every active collector completed ≥ mult scans that started after
		// last_seen (a scan that saw the device would have moved last_seen past its start).
		ok := len(actives) > 0
		for _, ac := range actives {
			after := len(ac.starts) - sort.Search(len(ac.starts), func(i int) bool { return ac.starts[i].After(c.lastSeen) })
			if after < mult {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		next := StatusOffline
		if c.status == StatusNew {
			next = StatusNewOffline
		}
		if _, err := tx.ExecContext(ctx, `UPDATE devices SET status = ? WHERE id = ?`, next, c.id); err != nil {
			return err
		}
		if err := addEvent(ctx, tx, rFinishedS, c.id, EventWentOffline, sn.id, "", "", collectorID); err != nil {
			return err
		}
	}
	return nil
}
