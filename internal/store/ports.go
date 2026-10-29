package store

import (
	"context"
	"database/sql"
	"hash/fnv"
	"strconv"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// portRecheck is the least time between two scheduled checks of a device (research R5).
const portRecheck = 20 * time.Hour

// PortSlot is the minute of the day at which a device's daily port check falls due: FNV-1a of
// its identity key, so devices spread over the day (feature 015, FR-008).
func PortSlot(identityKey string) int {
	h := fnv.New32a()
	h.Write([]byte(identityKey))
	return int(h.Sum32() % 1440)
}

// portDue reports whether a device whose last complete check finished at last (zero: never) is
// due at now: never checked, or not checked since its slot last came round and not in the last
// 20 hours.
func portDue(identityKey string, last, now time.Time) bool {
	if last.IsZero() {
		return true
	}
	day := now.Truncate(24 * time.Hour)
	slot := day.Add(time.Duration(PortSlot(identityKey)) * time.Minute)
	if slot.After(now) {
		slot = slot.Add(-24 * time.Hour)
	}
	return last.Before(slot) && !last.After(now.Add(-portRecheck))
}

// PortPlan is the port plan of a collector at now (feature 015, research R5): the current
// addresses of due devices on the subnets the collector reached directly (ARP or ICMP/TCP) in its
// latest presence run.
func (s *Store) PortPlan(ctx context.Context, collectorID int64, now time.Time) (contract.PortScan, error) {
	plan := contract.PortScan{Enabled: true, Due: []string{}, Excluded: []string{}}
	var runID string
	err := s.db.QueryRowContext(ctx, `SELECT r.collection_id FROM collection_runs r WHERE r.collector_id = ?
		AND EXISTS (SELECT 1 FROM run_subnets rs WHERE rs.collection_id = r.collection_id AND rs.method IN ('arp', 'icmp_tcp'))
		ORDER BY r.received_at DESC, r.rowid DESC LIMIT 1`, collectorID).Scan(&runID)
	if err == sql.ErrNoRows {
		return plan, nil
	}
	if err != nil {
		return plan, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT d.identity_key, a.ip,
			COALESCE((SELECT max(c.finished_at) FROM port_checks c WHERE c.device_id = d.id AND c.complete = 1), '')
		FROM device_addresses a
		JOIN devices d ON d.id = a.device_id
		JOIN subnets sn ON sn.id = a.subnet_id
		JOIN run_subnets rs ON rs.cidr = sn.cidr AND rs.collection_id = ? AND rs.method IN ('arp', 'icmp_tcp')
		WHERE a.current = 1 AND d.merged_into IS NULL AND d.manual = 0 AND sn.ignored = 0
		ORDER BY a.ip`, runID)
	if err != nil {
		return plan, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, ip, last string
		if err := rows.Scan(&key, &ip, &last); err != nil {
			return plan, err
		}
		var lastAt time.Time
		if last != "" {
			lastAt = MustParseTime(last)
		}
		if portDue(key, lastAt, now) {
			plan.Due = append(plan.Due, ip)
		}
	}
	return plan, rows.Err()
}

// DevicePort is one port of a device on its page.
type DevicePort struct {
	Port      int
	Proto     string
	Service   string
	IP        string
	Collector string
	FirstOpen time.Time
	LastOpen  time.Time
	ClosedAt  time.Time
}

// Label is "80/tcp".
func (p DevicePort) Label() string { return strconv.Itoa(p.Port) + "/" + p.Proto }

// DevicePorts is the "Open ports" card of a device page (feature 015, contracts/web-ui-changes.md).
type DevicePorts struct {
	Open          []DevicePort
	Closed        []DevicePort // previously open (US2)
	Checked       bool         // some collector completed a check of the device
	LastCheck     time.Time
	LastCollector string
	AllOpen       bool // the latest complete check found the device answering on every port
	Reachable     bool // a collector reaches one of its subnets directly (ARP or ICMP/TCP)
}

// DevicePorts reads a device's ports, including those of devices merged into it.
func (s *Store) DevicePorts(ctx context.Context, deviceID int64) (DevicePorts, error) {
	var v DevicePorts
	ids := `(SELECT ? UNION SELECT id FROM devices WHERE merged_into = ?)`
	rows, err := s.db.QueryContext(ctx, `SELECT p.port, p.proto, p.service, p.state, p.ip, COALESCE(c.name, ''),
			p.first_open, p.last_open, p.closed_at
		FROM device_ports p LEFT JOIN collectors c ON c.id = p.collector_id
		WHERE p.device_id IN `+ids+` ORDER BY p.port, p.proto`, deviceID, deviceID)
	if err != nil {
		return v, err
	}
	for rows.Next() {
		var p DevicePort
		var state, first, last, closed string
		if err := rows.Scan(&p.Port, &p.Proto, &p.Service, &state, &p.IP, &p.Collector, &first, &last, &closed); err != nil {
			rows.Close()
			return v, err
		}
		p.FirstOpen, p.LastOpen = MustParseTime(first), MustParseTime(last)
		if closed != "" {
			p.ClosedAt = MustParseTime(closed)
		}
		if state == "open" {
			v.Open = append(v.Open, p)
		} else {
			v.Closed = append(v.Closed, p)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return v, err
	}
	var finished, collector string
	var allOpen bool
	err = s.db.QueryRowContext(ctx, `SELECT pc.finished_at, COALESCE(c.name, ''), pc.all_open FROM port_checks pc
		LEFT JOIN collectors c ON c.id = pc.collector_id
		WHERE pc.device_id IN `+ids+` AND pc.complete = 1 ORDER BY pc.finished_at DESC LIMIT 1`, deviceID, deviceID).
		Scan(&finished, &collector, &allOpen)
	switch {
	case err == nil:
		v.Checked, v.LastCheck, v.LastCollector, v.AllOpen = true, MustParseTime(finished), collector, allOpen
	case err != sql.ErrNoRows:
		return v, err
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM device_addresses a JOIN subnets sn ON sn.id = a.subnet_id
		WHERE a.device_id = ? AND a.current = 1 AND EXISTS (SELECT 1 FROM run_subnets rs
			WHERE rs.cidr = sn.cidr AND rs.method IN ('arp', 'icmp_tcp'))`, deviceID).Scan(&n); err != nil {
		return v, err
	}
	v.Reachable = n > 0
	return v, nil
}
