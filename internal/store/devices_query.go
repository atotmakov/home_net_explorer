package store

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Address is a device address on a subnet.
type Address struct {
	IP       string
	CIDR     string
	SubnetID int64
	Current  bool
	AsOf     time.Time
	Via      string // router port or Wi-Fi interface of the latest sighting there (feature 002)
}

// DeviceRow is a device as listed in the inventory.
type DeviceRow struct {
	ID            int64
	IdentityKey   string
	DisplayName   string
	Hostname      string
	MAC           string
	MACRandomized bool
	Weak          bool
	Manufacturer  string
	Status        string
	UserName      string
	Notes         string
	Type          string
	FirstSeen     time.Time
	LastSeen      time.Time
	Addresses     []Address // current addresses on visible (not ignored) subnets
}

// PrimaryIP is the first current address, or "".
func (d DeviceRow) PrimaryIP() string {
	if len(d.Addresses) == 0 {
		return ""
	}
	return d.Addresses[0].IP
}

// DeviceFilter selects and orders devices (FR-017).
type DeviceFilter struct {
	Q            string // matches name, hostname, MAC, manufacturer or IP
	Subnet       string
	Status       string
	Manufacturer string
	Randomized   bool
	Weak         bool
	SeenAfter    time.Time // last_seen >= SeenAfter
	SeenBefore   time.Time // last_seen < SeenBefore
	Sort         string    // ip (default), mac, hostname, manufacturer, first_seen, last_seen, name
	Desc         bool
}

// SortKeys lists the accepted DeviceFilter.Sort values.
var SortKeys = []string{"ip", "name", "mac", "hostname", "manufacturer", "first_seen", "last_seen"}

// ErrBadSort is returned for an unknown sort key.
var ErrBadSort = errors.New("store: unknown sort key")

const deviceCols = `id, identity_key, display_name, hostname, COALESCE(mac, ''), mac_randomized,
	identity_strength = 'weak', manufacturer, status, user_name, notes, type, first_seen, last_seen`

func scanDevice(row interface{ Scan(...any) error }) (DeviceRow, error) {
	var d DeviceRow
	var first, last string
	err := row.Scan(&d.ID, &d.IdentityKey, &d.DisplayName, &d.Hostname, &d.MAC, &d.MACRandomized, &d.Weak,
		&d.Manufacturer, &d.Status, &d.UserName, &d.Notes, &d.Type, &first, &last)
	if err != nil {
		return d, err
	}
	d.FirstSeen, d.LastSeen = MustParseTime(first), MustParseTime(last)
	return d, nil
}

// ListDevices returns the inventory (excluding merged-away records and devices seen only on
// ignored subnets), filtered and sorted. Home networks are small (≈250 devices), so filtering
// and sorting happen in memory.
func (s *Store) ListDevices(ctx context.Context, f DeviceFilter) ([]DeviceRow, error) {
	if f.Sort != "" && !slices.Contains(SortKeys, f.Sort) {
		return nil, ErrBadSort
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+deviceCols+` FROM devices WHERE status != 'merged_away'`)
	if err != nil {
		return nil, err
	}
	var all []DeviceRow
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		all = append(all, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	addrs, hidden, err := s.currentAddresses(ctx)
	if err != nil {
		return nil, err
	}

	q := strings.ToLower(strings.TrimSpace(f.Q))
	man := strings.ToLower(strings.TrimSpace(f.Manufacturer))
	var out []DeviceRow
	for _, d := range all {
		d.Addresses = addrs[d.ID]
		if len(d.Addresses) == 0 && hidden[d.ID] {
			continue // only seen on ignored subnets
		}
		if q != "" && !matches(d, q) {
			continue
		}
		if f.Subnet != "" && !slices.ContainsFunc(d.Addresses, func(a Address) bool { return a.CIDR == f.Subnet }) {
			continue
		}
		if f.Status != "" && d.Status != f.Status {
			continue
		}
		if man != "" && !strings.Contains(strings.ToLower(d.Manufacturer), man) {
			continue
		}
		if (f.Randomized && !d.MACRandomized) || (f.Weak && !d.Weak) {
			continue
		}
		if !f.SeenAfter.IsZero() && d.LastSeen.Before(f.SeenAfter) {
			continue
		}
		if !f.SeenBefore.IsZero() && !d.LastSeen.Before(f.SeenBefore) {
			continue
		}
		out = append(out, d)
	}
	sortDevices(out, f.Sort, f.Desc)
	return out, nil
}

func matches(d DeviceRow, q string) bool {
	for _, s := range []string{d.DisplayName, d.Hostname, d.MAC, d.UserName, d.Manufacturer} {
		if strings.Contains(strings.ToLower(s), q) {
			return true
		}
	}
	for _, a := range d.Addresses {
		if strings.Contains(a.IP, q) {
			return true
		}
	}
	return false
}

func ipKey(d DeviceRow) netip.Addr {
	a, _ := netip.ParseAddr(d.PrimaryIP())
	return a
}

func sortDevices(ds []DeviceRow, key string, desc bool) {
	cmpFn := func(a, b DeviceRow) int {
		var c int
		switch key {
		case "name":
			c = cmp.Compare(strings.ToLower(a.DisplayName), strings.ToLower(b.DisplayName))
		case "mac":
			c = cmp.Compare(a.MAC, b.MAC)
		case "hostname":
			c = cmp.Compare(strings.ToLower(a.Hostname), strings.ToLower(b.Hostname))
		case "manufacturer":
			c = cmp.Compare(strings.ToLower(a.Manufacturer), strings.ToLower(b.Manufacturer))
		case "first_seen":
			c = a.FirstSeen.Compare(b.FirstSeen)
		case "last_seen":
			c = a.LastSeen.Compare(b.LastSeen)
		default: // ip, numerically
			c = ipKey(a).Compare(ipKey(b))
		}
		if c == 0 {
			c = cmp.Compare(a.ID, b.ID)
		}
		if desc {
			return -c
		}
		return c
	}
	slices.SortStableFunc(ds, cmpFn)
}

// currentAddresses returns current addresses per device on visible subnets, plus the set of
// devices that have addresses only on ignored subnets.
func (s *Store) currentAddresses(ctx context.Context) (map[int64][]Address, map[int64]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.device_id, a.ip, sn.cidr, a.subnet_id, a.as_of, sn.ignored,
		COALESCE((SELECT g.via FROM sightings g WHERE g.device_id = a.device_id AND g.subnet_id = a.subnet_id
			ORDER BY g.last_seen DESC, g.id DESC LIMIT 1), '')
		FROM device_addresses a JOIN subnets sn ON sn.id = a.subnet_id WHERE a.current = 1`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	addrs := map[int64][]Address{}
	hidden := map[int64]bool{}
	for rows.Next() {
		var devID int64
		var a Address
		var asOf string
		var ignored bool
		if err := rows.Scan(&devID, &a.IP, &a.CIDR, &a.SubnetID, &asOf, &ignored, &a.Via); err != nil {
			return nil, nil, err
		}
		if ignored {
			hidden[devID] = true
			continue
		}
		a.Current, a.AsOf = true, MustParseTime(asOf)
		addrs[devID] = append(addrs[devID], a)
	}
	for id := range addrs {
		slices.SortFunc(addrs[id], func(x, y Address) int {
			ax, _ := netip.ParseAddr(x.IP)
			ay, _ := netip.ParseAddr(y.IP)
			return ax.Compare(ay)
		})
	}
	return addrs, hidden, rows.Err()
}

// GetDevice loads one device with its current addresses.
func (s *Store) GetDevice(ctx context.Context, id int64) (DeviceRow, error) {
	d, err := scanDevice(s.db.QueryRowContext(ctx, `SELECT `+deviceCols+` FROM devices WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	if err != nil {
		return d, err
	}
	addrs, _, err := s.currentAddresses(ctx)
	if err != nil {
		return d, err
	}
	d.Addresses = addrs[d.ID]
	return d, nil
}

// Counts summarizes the inventory for the home page.
type Counts struct {
	Total, Online, Offline, New int
}

// HomeCounts counts devices by status.
func (s *Store) HomeCounts(ctx context.Context) (Counts, error) {
	var c Counts
	err := s.db.QueryRowContext(ctx, `SELECT
		count(*),
		count(*) FILTER (WHERE status IN ('online', 'new')),
		count(*) FILTER (WHERE status IN ('offline', 'new_offline')),
		count(*) FILTER (WHERE status IN ('new', 'new_offline'))
		FROM devices WHERE status != 'merged_away'`).Scan(&c.Total, &c.Online, &c.Offline, &c.New)
	return c, err
}

// SubnetInfo is a subnet with its scan status.
type SubnetInfo struct {
	ID            int64
	CIDR          string
	Name          string
	Ignored       bool
	FirstSeen     time.Time
	DiscoveredBy  string
	LastScanned   time.Time
	LastScannedBy string
	Stale         bool // no collector covering it is still reporting (computed from now, never stored)
	Devices       int
}

// SubnetStatus lists subnets with their last completed scan and staleness at time now.
func (s *Store) SubnetStatus(ctx context.Context, now time.Time) ([]SubnetInfo, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT sn.id, sn.cidr, sn.name, sn.ignored, sn.first_seen_at, c.name,
		(SELECT count(*) FROM device_addresses a JOIN devices d ON d.id = a.device_id
		 WHERE a.subnet_id = sn.id AND a.current = 1 AND d.status != 'merged_away')
		FROM subnets sn JOIN collectors c ON c.id = sn.discovered_by`)
	if err != nil {
		return nil, err
	}
	var out []SubnetInfo
	for rows.Next() {
		var si SubnetInfo
		var first string
		if err := rows.Scan(&si.ID, &si.CIDR, &si.Name, &si.Ignored, &first, &si.DiscoveredBy, &si.Devices); err != nil {
			rows.Close()
			return nil, err
		}
		si.FirstSeen = MustParseTime(first)
		out = append(out, si)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	mult := 3
	if v, ok, _ := s.Setting(ctx, "offline_multiplier"); ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			mult = n
		}
	}
	for i := range out {
		si := &out[i]
		si.Stale = true
		crows, err := s.db.QueryContext(ctx, `SELECT c.name, MAX(r.finished_at), r.interval_seconds
			FROM collection_runs r JOIN run_subnets rs ON rs.collection_id = r.collection_id
			JOIN collectors c ON c.id = r.collector_id
			WHERE rs.cidr = ? AND rs.complete = 1 GROUP BY r.collector_id`, si.CIDR)
		if err != nil {
			return nil, err
		}
		for crows.Next() {
			var name, fin string
			var interval int
			if err := crows.Scan(&name, &fin, &interval); err != nil {
				crows.Close()
				return nil, err
			}
			t := MustParseTime(fin)
			if t.After(si.LastScanned) {
				si.LastScanned, si.LastScannedBy = t, name
			}
			if now.Sub(t) <= time.Duration(mult*interval)*time.Second {
				si.Stale = false
			}
		}
		crows.Close()
	}
	slices.SortFunc(out, func(a, b SubnetInfo) int {
		pa, _ := netip.ParsePrefix(a.CIDR)
		pb, _ := netip.ParsePrefix(b.CIDR)
		return pa.Addr().Compare(pb.Addr())
	})
	return out, nil
}

// IgnoredSubnets lists the subnets the owner ignored (returned to collectors by /api/v1/ping).
func (s *Store) IgnoredSubnets(ctx context.Context) ([]netip.Prefix, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT cidr FROM subnets WHERE ignored = 1 ORDER BY cidr`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []netip.Prefix
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		p, err := netip.ParsePrefix(c)
		if err != nil {
			return nil, fmt.Errorf("store: bad subnet %q: %w", c, err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SkippedSubnet is a too-large subnet reported in a collector's latest run.
type SkippedSubnet struct {
	Collector string
	CIDR      string
}

// SkippedSubnets lists too-large subnets from each collector's latest run (data-model.md).
func (s *Store) SkippedSubnets(ctx context.Context) ([]SkippedSubnet, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.name, rs.cidr FROM run_subnets rs
		JOIN collection_runs r ON r.collection_id = rs.collection_id
		JOIN collectors c ON c.id = r.collector_id
		WHERE rs.method = 'skipped' AND rs.skip_reason = 'too_large'
		AND r.rowid = (SELECT MAX(r2.rowid) FROM collection_runs r2 WHERE r2.collector_id = r.collector_id)
		ORDER BY c.name, rs.cidr`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SkippedSubnet
	for rows.Next() {
		var sk SkippedSubnet
		if err := rows.Scan(&sk.Collector, &sk.CIDR); err != nil {
			return nil, err
		}
		out = append(out, sk)
	}
	return out, rows.Err()
}
