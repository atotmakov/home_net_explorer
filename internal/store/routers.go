package store

import (
	"context"
	"database/sql"
	"errors"
	"net/netip"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// Router settings configured in the web UI (feature 003). The password is stored in plain text
// by owner decision (spec TODO-SEC-1) and only leaves the store through RouterLogin, for the
// collector API.

// SettingRouterRejectedBuiltin holds the built-in collector's rejected router login hashes
// (feature 003, research R8). Saving or removing router settings clears it, so re-saving the
// Router card makes the NAS try again (e.g. after a lockout with the right password).
const SettingRouterRejectedBuiltin = "router_rejected_builtin"

func clearBuiltinRejections(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, SettingRouterRejectedBuiltin)
	return err
}

// ErrNoPassword means a router was saved without a password and none was stored before.
var ErrNoPassword = errors.New("store: a router password is required")

// RouterSettings is what the owner saves. An empty Password keeps the stored one.
type RouterSettings struct {
	Model    string
	Subnet   string // preferred subnet for the address; "" = where the device was last seen
	Username string
	Password string
}

// RouterView is a router's settings as shown in the UI. It deliberately has no password field.
type RouterView struct {
	ID          int64
	IdentityKey string
	Model       string
	Subnet      string
	Username    string
	PasswordSet bool
	UpdatedAt   time.Time
}

// RouterRead is the latest read of a router by one collector (from run_sources).
type RouterRead struct {
	Collector string
	Outcome   string
	Online    int
	Offline   int
	At        time.Time
}

// SaveRouter creates or replaces the router settings of a device, keeping its id.
func SaveRouter(ctx context.Context, tx *sql.Tx, identityKey string, r RouterSettings, at time.Time) error {
	if err := clearBuiltinRejections(ctx, tx); err != nil {
		return err
	}
	var stored string
	err := tx.QueryRowContext(ctx, `SELECT password FROM router_settings WHERE identity_key = ?`, identityKey).Scan(&stored)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if r.Password == "" {
			return ErrNoPassword
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO router_settings (identity_key, model, subnet, username, password, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)`, identityKey, r.Model, r.Subnet, r.Username, r.Password, FormatTime(at))
		return err
	case err != nil:
		return err
	}
	if r.Password == "" {
		r.Password = stored
	}
	_, err = tx.ExecContext(ctx, `UPDATE router_settings SET model = ?, subnet = ?, username = ?, password = ?, updated_at = ?
		WHERE identity_key = ?`, r.Model, r.Subnet, r.Username, r.Password, FormatTime(at), identityKey)
	return err
}

// DeleteRouter removes a device's router settings (no-op when there are none).
func DeleteRouter(ctx context.Context, tx *sql.Tx, identityKey string) error {
	if err := clearBuiltinRejections(ctx, tx); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM router_settings WHERE identity_key = ?`, identityKey)
	return err
}

// GetRouterSettings returns a device's router settings, without the password.
func (s *Store) GetRouterSettings(ctx context.Context, identityKey string) (RouterView, bool, error) {
	var v RouterView
	var updated string
	err := s.db.QueryRowContext(ctx, `SELECT id, identity_key, model, subnet, username, password != '', updated_at
		FROM router_settings WHERE identity_key = ?`, identityKey).
		Scan(&v.ID, &v.IdentityKey, &v.Model, &v.Subnet, &v.Username, &v.PasswordSet, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return v, false, nil
	}
	if err != nil {
		return v, false, err
	}
	v.UpdatedAt = MustParseTime(updated)
	return v, true, nil
}

// RouterLogin returns the login of router id, for the collector API only.
func (s *Store) RouterLogin(ctx context.Context, id int64) (contract.RouterLogin, error) {
	var l contract.RouterLogin
	err := s.db.QueryRowContext(ctx, `SELECT username, password FROM router_settings WHERE id = ?`, id).
		Scan(&l.Username, &l.Password)
	if errors.Is(err, sql.ErrNoRows) {
		return l, ErrNotFound
	}
	return l, err
}

// RouterAddress is a candidate address of a router device.
type RouterAddress struct {
	IP       string
	CIDR     string
	LastSeen string // latest sighting on that subnet
}

// RouterAddresses returns the current private addresses of the device holding identityKey
// (following merges), on subnets the owner hasn't ignored.
func (s *Store) RouterAddresses(ctx context.Context, identityKey string) ([]RouterAddress, error) {
	devID, err := s.resolveDevice(ctx, identityKey)
	if err != nil || devID == 0 {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT a.ip, sn.cidr,
		COALESCE((SELECT max(g.last_seen) FROM sightings g WHERE g.device_id = a.device_id AND g.subnet_id = a.subnet_id), '')
		FROM device_addresses a JOIN subnets sn ON sn.id = a.subnet_id
		WHERE a.device_id = ? AND a.current = 1 AND sn.ignored = 0`, devID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RouterAddress
	for rows.Next() {
		var a RouterAddress
		if err := rows.Scan(&a.IP, &a.CIDR, &a.LastSeen); err != nil {
			return nil, err
		}
		if ip, err := netip.ParseAddr(a.IP); err == nil && contract.IsPrivateAddr(ip) {
			out = append(out, a)
		}
	}
	return out, rows.Err()
}

// resolveDevice returns the id of the device holding identityKey, following merged_into, or 0.
func (s *Store) resolveDevice(ctx context.Context, identityKey string) (int64, error) {
	var id int64
	var into sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT id, merged_into FROM devices WHERE identity_key = ?`, identityKey).Scan(&id, &into)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	for i := 0; err == nil && into.Valid && i < 16; i++ {
		id = into.Int64
		err = s.db.QueryRowContext(ctx, `SELECT merged_into FROM devices WHERE id = ?`, id).Scan(&into)
	}
	return id, err
}

// PickRouterAddress chooses the router's address: the one on the preferred subnet, else the one
// on the subnet where the device was last seen (ties: lower subnet address). ok is false when
// there is no candidate.
func PickRouterAddress(addrs []RouterAddress, preferred string) (RouterAddress, bool) {
	var best RouterAddress
	found := false
	for _, a := range addrs {
		if preferred != "" {
			if a.CIDR == preferred {
				return a, true
			}
			continue
		}
		if !found || a.LastSeen > best.LastSeen || (a.LastSeen == best.LastSeen && cidrLess(a.CIDR, best.CIDR)) {
			best, found = a, true
		}
	}
	return best, found
}

func cidrLess(a, b string) bool {
	pa, errA := netip.ParsePrefix(a)
	pb, errB := netip.ParsePrefix(b)
	if errA != nil || errB != nil {
		return a < b
	}
	return pa.Addr().Less(pb.Addr())
}

// ListRouters returns every configured router with a resolved address (research R3), by id.
// Routers without a current private address are left out. Never includes credentials.
func (s *Store) ListRouters(ctx context.Context) ([]contract.RouterRef, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, identity_key, model, subnet FROM router_settings ORDER BY id`)
	if err != nil {
		return nil, err
	}
	type row struct {
		id                 int64
		key, model, subnet string
	}
	var rs []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.key, &r.model, &r.subnet); err != nil {
			rows.Close()
			return nil, err
		}
		rs = append(rs, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []contract.RouterRef{}
	for _, r := range rs {
		addrs, err := s.RouterAddresses(ctx, r.key)
		if err != nil {
			return nil, err
		}
		if a, ok := PickRouterAddress(addrs, r.subnet); ok {
			out = append(out, contract.RouterRef{ID: r.id, Model: r.model, Address: a.IP, Subnet: a.CIDR})
		}
	}
	return out, nil
}

// RouterStatus returns, per collector (by name), the latest read of the router at address.
func (s *Store) RouterStatus(ctx context.Context, address string) ([]RouterRead, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.name, rs.outcome, rs.online, rs.offline, r.finished_at
		FROM run_sources rs
		JOIN collection_runs r ON r.collection_id = rs.collection_id
		JOIN collectors c ON c.id = r.collector_id
		WHERE rs.address = ? AND r.rowid = (SELECT max(r2.rowid) FROM collection_runs r2
			JOIN run_sources rs2 ON rs2.collection_id = r2.collection_id
			WHERE r2.collector_id = r.collector_id AND rs2.address = ?)
		ORDER BY c.name`, address, address)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RouterRead
	for rows.Next() {
		var rd RouterRead
		var at string
		if err := rows.Scan(&rd.Collector, &rd.Outcome, &rd.Online, &rd.Offline, &at); err != nil {
			return nil, err
		}
		rd.At = MustParseTime(at)
		out = append(out, rd)
	}
	return out, rows.Err()
}
