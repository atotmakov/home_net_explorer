package store

import (
	"context"
	"strconv"
	"time"
)

// Device states as shown (feature 006). They are computed from the stored status and the time
// of viewing, never stored.
const (
	StateOnline  = "online"  // stored online, seen within FreshScans scan intervals
	StateStale   = "stale"   // stored online, but not seen for longer: the information is old
	StateOffline = "offline" // the latest complete scan of its subnets did not see it
	StateManual  = "manual"  // added by hand, never seen by scans (feature 011)
)

// States lists the shown states (the Devices status filter).
var States = []string{StateOnline, StateStale, StateOffline}

// FreshScans is how many scan intervals "online" stays fresh (feature 006, FR-002a).
const FreshScans = 2

// defaultScanInterval is used when no interval is known (the built-in default).
const defaultScanInterval = 900

// latestScan is the most recent complete scan of a subnet.
type latestScan struct {
	finished time.Time
	interval int
}

// latestScans returns the most recent complete scan (any collector) per subnet id.
func (s *Store) latestScans(ctx context.Context) (map[int64]latestScan, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT sn.id, r.finished_at, r.interval_seconds FROM subnets sn
		JOIN run_subnets rs ON rs.cidr = sn.cidr AND rs.complete = 1
		JOIN collection_runs r ON r.collection_id = rs.collection_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]latestScan{}
	for rows.Next() {
		var id int64
		var fin string
		var ls latestScan
		if err := rows.Scan(&id, &fin, &ls.interval); err != nil {
			return nil, err
		}
		ls.finished = MustParseTime(fin)
		if cur, ok := out[id]; !ok || ls.finished.After(cur.finished) {
			out[id] = ls
		}
	}
	return out, rows.Err()
}

// builtinInterval is the built-in collector's interval from Settings (seconds).
func (s *Store) builtinInterval(ctx context.Context) int {
	if v, ok, _ := s.Setting(ctx, "builtin_interval_seconds"); ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultScanInterval
}

// stateEval computes shown states at one moment.
type stateEval struct {
	now      time.Time
	scans    map[int64]latestScan
	fallback int
}

func (s *Store) newStateEval(ctx context.Context, now time.Time) (*stateEval, error) {
	if now.IsZero() {
		return &stateEval{}, nil
	}
	scans, err := s.latestScans(ctx)
	if err != nil {
		return nil, err
	}
	return &stateEval{now: now, scans: scans, fallback: s.builtinInterval(ctx)}, nil
}

// apply sets d.State and d.Age (time since last seen, for every state). The interval is that of the most recent complete scan of any
// subnet holding one of d's current addresses, else the built-in interval. With a zero time,
// stored online devices are shown online.
func (e *stateEval) apply(d *DeviceRow) {
	if d.Manual {
		d.State = StateManual // no scan says anything about it (feature 011, research R4)
		return
	}
	if !e.now.IsZero() {
		d.Age = e.now.Sub(d.LastSeen)
	}
	if d.Status == "offline" || d.Status == "new_offline" {
		d.State = StateOffline
		return
	}
	d.State = StateOnline
	if e.now.IsZero() {
		return
	}
	interval := e.fallback
	var latest time.Time
	for _, a := range d.Addresses {
		if ls, ok := e.scans[a.SubnetID]; ok && ls.finished.After(latest) {
			latest, interval = ls.finished, ls.interval
		}
	}
	if d.Age > time.Duration(FreshScans*interval)*time.Second {
		d.State = StateStale
	}
}
