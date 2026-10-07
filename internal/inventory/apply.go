package inventory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/oui"
	"github.com/atotmakov/home_net_explorer/internal/store"
)

// Applier folds runs into the projections (implements ingest.Applier). It is deterministic:
// the same runs and user facts in the same order always give the same projections, which
// is what makes `hne-server rebuild` possible (Principle V).
type Applier struct {
	// Manufacturer maps a MAC to its vendor ("" for unknown/randomized).
	Manufacturer func(mac string) string
}

// NewApplier returns an Applier using the embedded IEEE registry.
func NewApplier() *Applier {
	return &Applier{Manufacturer: oui.Manufacturer}
}

type scannedSubnet struct {
	id      int64
	prefix  netip.Prefix
	ignored bool
	scan    contract.SubnetScan
}

// Apply implements ingest.Applier.
func (a *Applier) Apply(ctx context.Context, tx *sql.Tx, collectorID int64, run *contract.CollectionRun, receivedAt time.Time) ([]string, error) {
	var runRow int64
	if err := tx.QueryRowContext(ctx, `SELECT rowid FROM collection_runs WHERE collection_id = ?`, run.CollectionID).Scan(&runRow); err != nil {
		return nil, fmt.Errorf("inventory: run %s not stored: %w", run.CollectionID, err)
	}
	subnets, newSubnets, err := applySubnets(ctx, tx, collectorID, run)
	if err != nil {
		return nil, err
	}

	seen := map[int64]map[int64]bool{} // subnet id → device ids seen in this run
	for _, o := range run.Observations {
		sn := containing(subnets, o.IP)
		if sn == nil || sn.ignored {
			continue // ignored subnets stay in the raw run but are never folded
		}
		devID, err := a.observe(ctx, tx, collectorID, sn, o)
		if err != nil {
			return nil, err
		}
		if devID == 0 {
			continue
		}
		if seen[sn.id] == nil {
			seen[sn.id] = map[int64]bool{}
		}
		seen[sn.id][devID] = true
	}

	for _, sn := range subnets {
		if !sn.scan.Complete || sn.ignored {
			continue
		}
		if err := evaluateOffline(ctx, tx, run, runRow, collectorID, sn, seen[sn.id]); err != nil {
			return nil, err
		}
	}
	return newSubnets, nil
}

// applySubnets creates subnet records for the non-skipped scans of a run (research R14).
func applySubnets(ctx context.Context, tx *sql.Tx, collectorID int64, run *contract.CollectionRun) ([]*scannedSubnet, []string, error) {
	var out []*scannedSubnet
	var created []string
	for _, s := range run.Subnets {
		if s.Method == contract.MethodSkipped {
			continue // skipped entries live only in run_subnets
		}
		p, err := netip.ParsePrefix(s.CIDR)
		if err != nil {
			return nil, nil, err
		}
		sn := &scannedSubnet{prefix: p, scan: s}
		err = tx.QueryRowContext(ctx, `SELECT id, ignored FROM subnets WHERE cidr = ?`, s.CIDR).Scan(&sn.id, &sn.ignored)
		if errors.Is(err, sql.ErrNoRows) {
			res, err := tx.ExecContext(ctx, `INSERT INTO subnets (cidr, first_seen_at, discovered_by) VALUES (?, ?, ?)`,
				s.CIDR, store.FormatTime(run.StartedAt), collectorID)
			if err != nil {
				return nil, nil, err
			}
			sn.id, _ = res.LastInsertId()
			created = append(created, s.CIDR)
		} else if err != nil {
			return nil, nil, err
		}
		out = append(out, sn)
	}
	return out, created, nil
}

func containing(subnets []*scannedSubnet, ip string) *scannedSubnet {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return nil
	}
	var best *scannedSubnet
	for _, sn := range subnets {
		if sn.prefix.Contains(a) && (best == nil || sn.prefix.Bits() > best.prefix.Bits()) {
			best = sn
		}
	}
	return best
}

// device is the slice of a device row that Apply needs.
type device struct {
	id         int64
	status     string
	lastSeen   string
	hostname   string
	hostnameAt string
}

// observe folds one observation and returns the device id. Identity (research R6): the MAC
// when known; otherwise the device that currently holds the address, else a weak identity.
func (a *Applier) observe(ctx context.Context, tx *sql.Tx, collectorID int64, sn *scannedSubnet, o contract.Observation) (int64, error) {
	subnetID := sn.id
	at := store.FormatTime(o.ObservedAt)

	var d device
	var err error
	if o.MAC != "" {
		d, err = loadDevice(ctx, tx, MACKey(o.MAC))
		if errors.Is(err, sql.ErrNoRows) {
			d, err = a.createDevice(ctx, tx, MACKey(o.MAC), StrengthStrong, o, at)
		}
		if err == nil {
			err = foldWeakDevices(ctx, tx, d.id, sn, o.IP, o.Hostname, at)
		}
	} else {
		var holder int64
		holder, err = currentHolder(ctx, tx, subnetID, o.IP)
		switch {
		case err != nil:
		case holder != 0:
			d, err = loadDeviceByID(ctx, tx, holder)
		default:
			key := WeakKey(sn.scan.CIDR, o.IP, o.Hostname)
			d, err = loadDevice(ctx, tx, key)
			if errors.Is(err, sql.ErrNoRows) {
				d, err = a.createDevice(ctx, tx, key, StrengthWeak, o, at)
			}
		}
	}
	if err != nil {
		return 0, err
	}

	if err := upsertSighting(ctx, tx, d.id, collectorID, subnetID, o, at); err != nil {
		return 0, err
	}
	if err := updateAddress(ctx, tx, d.id, subnetID, o.IP, at); err != nil {
		return 0, err
	}

	// Monotonic device state: only newer observations move the hostname forward.
	if o.Hostname != "" && at >= d.hostnameAt && o.Hostname != d.hostname {
		if _, err := tx.ExecContext(ctx, `UPDATE devices SET hostname = ?, hostname_at = ? WHERE id = ?`, o.Hostname, at, d.id); err != nil {
			return 0, err
		}
	} else if o.Hostname != "" && o.Hostname == d.hostname && at > d.hostnameAt {
		if _, err := tx.ExecContext(ctx, `UPDATE devices SET hostname_at = ? WHERE id = ?`, at, d.id); err != nil {
			return 0, err
		}
	}

	if at > d.lastSeen {
		if _, err := tx.ExecContext(ctx, `UPDATE devices SET last_seen = ? WHERE id = ?`, at, d.id); err != nil {
			return 0, err
		}
		if d.status == StatusOffline || d.status == StatusNewOffline {
			back := StatusOnline
			if d.status == StatusNewOffline {
				back = StatusNew
			}
			if _, err := tx.ExecContext(ctx, `UPDATE devices SET status = ? WHERE id = ?`, back, d.id); err != nil {
				return 0, err
			}
			if err := addEvent(ctx, tx, at, d.id, EventCameOnline, subnetID, "", o.IP, collectorID); err != nil {
				return 0, err
			}
		}
	}
	if err := refreshDisplayName(ctx, tx, d.id); err != nil {
		return 0, err
	}
	return d.id, nil
}

const deviceSelect = `SELECT id, status, last_seen, hostname, hostname_at FROM devices `

func loadDevice(ctx context.Context, tx *sql.Tx, key string) (device, error) {
	var d device
	err := tx.QueryRowContext(ctx, deviceSelect+`WHERE identity_key = ?`, key).
		Scan(&d.id, &d.status, &d.lastSeen, &d.hostname, &d.hostnameAt)
	return d, err
}

func loadDeviceByID(ctx context.Context, tx *sql.Tx, id int64) (device, error) {
	var d device
	err := tx.QueryRowContext(ctx, deviceSelect+`WHERE id = ?`, id).
		Scan(&d.id, &d.status, &d.lastSeen, &d.hostname, &d.hostnameAt)
	return d, err
}

// currentHolder returns the device that currently has ip on the subnet (0 if none),
// preferring a MAC-identified one.
func currentHolder(ctx context.Context, tx *sql.Tx, subnetID int64, ip string) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT d.id FROM device_addresses a JOIN devices d ON d.id = a.device_id
		WHERE a.subnet_id = ? AND a.ip = ? AND a.current = 1 AND d.status != 'merged_away'
		ORDER BY d.identity_strength = 'strong' DESC, a.as_of DESC LIMIT 1`, subnetID, ip).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

func (a *Applier) createDevice(ctx context.Context, tx *sql.Tx, key, strength string, o contract.Observation, at string) (device, error) {
	hostAt := ""
	if o.Hostname != "" {
		hostAt = at
	}
	var mac any
	manufacturer, randomized := "", false
	if o.MAC != "" {
		mac, randomized = o.MAC, oui.IsRandomized(o.MAC)
		if a.Manufacturer != nil {
			manufacturer = a.Manufacturer(o.MAC)
		}
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO devices (identity_key, identity_strength, mac, mac_randomized, manufacturer,
		hostname, hostname_at, status, first_seen, last_seen, display_name) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		key, strength, mac, randomized, manufacturer, o.Hostname, hostAt, StatusOnline, at, at, o.IP)
	if err != nil {
		return device{}, err
	}
	id, _ := res.LastInsertId()
	// Events for new devices arrive with US3 (T076); the device row is the record for now.
	return device{id: id, status: StatusOnline, lastSeen: at, hostname: o.Hostname, hostnameAt: hostAt}, nil
}

// upsertSighting extends the latest sighting of (device, collector, subnet) when the tuple is
// unchanged, otherwise inserts a new one (data-model.md "Sighting").
func upsertSighting(ctx context.Context, tx *sql.Tx, devID, collectorID, subnetID int64, o contract.Observation, at string) error {
	var id int64
	var ip, mac, host, first, last string
	err := tx.QueryRowContext(ctx, `SELECT id, ip, mac, hostname, first_seen, last_seen FROM sightings
		WHERE device_id = ? AND collector_id = ? AND subnet_id = ? ORDER BY last_seen DESC, id DESC LIMIT 1`,
		devID, collectorID, subnetID).Scan(&id, &ip, &mac, &host, &first, &last)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && ip == o.IP && mac == o.MAC && host == o.Hostname {
		_, err := tx.ExecContext(ctx, `UPDATE sightings SET seen_count = seen_count + 1,
			first_seen = min(first_seen, ?), last_seen = max(last_seen, ?) WHERE id = ?`, at, at, id)
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sightings (device_id, collector_id, subnet_id, ip, mac, hostname,
		first_seen, last_seen, seen_count) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1)`,
		devID, collectorID, subnetID, o.IP, o.MAC, o.Hostname, at, at)
	return err
}

// updateAddress keeps one current address per (device, subnet), moving it only forward in time.
func updateAddress(ctx context.Context, tx *sql.Tx, devID, subnetID int64, ip, at string) error {
	var curIP, asOf string
	err := tx.QueryRowContext(ctx, `SELECT ip, as_of FROM device_addresses WHERE device_id = ? AND subnet_id = ? AND current = 1`,
		devID, subnetID).Scan(&curIP, &asOf)
	switch {
	case err == nil && curIP == ip:
		if at > asOf {
			_, err = tx.ExecContext(ctx, `UPDATE device_addresses SET as_of = ? WHERE device_id = ? AND subnet_id = ? AND ip = ?`, at, devID, subnetID, ip)
		}
		return err
	case err == nil && at < asOf:
		// A late observation of an older address: history only (sighting), current unchanged.
		_, err = tx.ExecContext(ctx, `INSERT INTO device_addresses (device_id, subnet_id, ip, current, as_of)
			VALUES (?, ?, ?, 0, ?) ON CONFLICT (device_id, subnet_id, ip) DO NOTHING`, devID, subnetID, ip, at)
		return err
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE device_addresses SET current = 0 WHERE device_id = ? AND subnet_id = ?`, devID, subnetID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO device_addresses (device_id, subnet_id, ip, current, as_of) VALUES (?, ?, ?, 1, ?)
		ON CONFLICT (device_id, subnet_id, ip) DO UPDATE SET current = 1, as_of = excluded.as_of`, devID, subnetID, ip, at)
	return err
}

// refreshDisplayName applies "user name, else hostname, else IP".
func refreshDisplayName(ctx context.Context, tx *sql.Tx, devID int64) error {
	_, err := tx.ExecContext(ctx, `UPDATE devices SET display_name = COALESCE(
		NULLIF(user_name, ''),
		NULLIF(hostname, ''),
		(SELECT ip FROM device_addresses WHERE device_id = devices.id AND current = 1 ORDER BY subnet_id LIMIT 1),
		display_name) WHERE id = ?`, devID)
	return err
}

func addEvent(ctx context.Context, tx *sql.Tx, at string, devID int64, typ string, subnetID int64, oldV, newV string, collectorID int64) error {
	var sid, cid any
	if subnetID != 0 {
		sid = subnetID
	}
	if collectorID != 0 {
		cid = collectorID
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO events (at, device_id, type, subnet_id, old_value, new_value, collector_id)
		VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT (device_id, type, at, new_value) DO NOTHING`,
		at, devID, typ, sid, oldV, newV, cid)
	return err
}
