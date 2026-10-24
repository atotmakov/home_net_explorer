package inventory

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/oui"
	"github.com/atotmakov/home_net_explorer/internal/store"
)

// Manual devices (feature 011): equipment the owner adds by hand because scans cannot see it (an
// unmanaged switch). Each change is an append-only fact in user_manual_devices, replayed by a
// rebuild; name, type and notes are ordinary device attributes (research R1).

// ManualKeyPrefix starts the identity key of a manual device: manual:<uuid>, fixed at add (R2).
const ManualKeyPrefix = "manual:"

// ManualDevice is what the owner enters when adding a device.
type ManualDevice struct {
	Name, Type, MAC, Notes string
}

// ValidationError is an owner input the add or edit form refuses; its text is shown as is.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

// ErrMACInUse refuses a MAC that another device already has (FR-008).
type ErrMACInUse struct {
	Name, IP string
}

func (e *ErrMACInUse) Error() string {
	if e.IP == "" {
		return "this MAC belongs to " + e.Name
	}
	return "this MAC belongs to " + e.Name + " (" + e.IP + ")"
}

// ErrNotManual is returned for a MAC change or removal of a device found by scans (FR-012).
var ErrNotManual = errors.New("inventory: not a device added by hand")

// NormalizeManualMAC accepts a MAC with colons, dashes, dots or no separators, in any case, and
// returns it as scans store it (00:00:5e:00:53:01). A broadcast or multicast MAC is refused.
func NormalizeManualMAC(s string) (string, error) {
	hex := strings.ToLower(strings.NewReplacer(":", "", "-", "", ".", "", " ", "").Replace(strings.TrimSpace(s)))
	bad := ValidationError(fmt.Sprintf("%q is not a MAC address", s))
	if len(hex) != 12 || strings.Trim(hex, "0123456789abcdef") != "" || hex == "000000000000" {
		return "", bad
	}
	if first := strings.IndexByte("0123456789abcdef", hex[1]); first&1 == 1 {
		return "", bad // group address (broadcast, multicast): never a device
	}
	parts := make([]string, 6)
	for i := range parts {
		parts[i] = hex[2*i : 2*i+2]
	}
	return strings.Join(parts, ":"), nil
}

// AddManualDevice validates m, records the add and its attributes as facts and applies them. It
// returns the new device's identity key.
func AddManualDevice(ctx context.Context, tx *sql.Tx, m ManualDevice, at time.Time) (string, error) {
	m.Name, m.Type, m.Notes = strings.TrimSpace(m.Name), strings.TrimSpace(m.Type), strings.TrimSpace(m.Notes)
	if m.Name == "" {
		return "", ValidationError("a name is required")
	}
	for field, v := range map[string]string{"name": m.Name, "type": m.Type, "notes": m.Notes} {
		if err := ValidateDeviceAttr(field, v); err != nil {
			return "", ValidationError(err.Error())
		}
	}
	mac := ""
	if strings.TrimSpace(m.MAC) != "" {
		var err error
		if mac, err = NormalizeManualMAC(m.MAC); err != nil {
			return "", err
		}
		if err := macFree(ctx, tx, mac, ""); err != nil {
			return "", err
		}
	}
	key := ManualKeyPrefix + newUUID()
	if err := addManualFact(ctx, tx, key, "add", "", at); err != nil {
		return "", err
	}
	if mac != "" {
		if err := addManualFact(ctx, tx, key, "mac", mac, at); err != nil {
			return "", err
		}
	}
	for _, f := range [][2]string{{"name", m.Name}, {"type", m.Type}, {"notes", m.Notes}} {
		if f[1] == "" {
			continue
		}
		if err := SetDeviceAttr(ctx, tx, key, f[0], f[1], at); err != nil {
			return "", err
		}
	}
	return key, nil
}

// SetManualMAC sets or (with "") clears the MAC of a manual device.
func SetManualMAC(ctx context.Context, tx *sql.Tx, key, mac string, at time.Time) error {
	if err := mustBeManual(ctx, tx, key); err != nil {
		return err
	}
	if strings.TrimSpace(mac) != "" {
		var err error
		if mac, err = NormalizeManualMAC(mac); err != nil {
			return err
		}
		if err := macFree(ctx, tx, mac, key); err != nil {
			return err
		}
	} else {
		mac = ""
	}
	return addManualFact(ctx, tx, key, "mac", mac, at)
}

// RemoveManualDevice removes a manual device and the owner's links that point to it (R5).
func RemoveManualDevice(ctx context.Context, tx *sql.Tx, key string, at time.Time) error {
	if err := mustBeManual(ctx, tx, key); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT l.from_identity_key FROM user_links l
		WHERE l.id = (SELECT max(m.id) FROM user_links m WHERE m.from_identity_key = l.from_identity_key)
		AND l.removed = 0 AND l.to_identity_key = ?`, key)
	if err != nil {
		return err
	}
	var from []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return err
		}
		from = append(from, k)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, k := range from {
		if err := store.ClearUserLink(ctx, tx, k, at); err != nil {
			return err
		}
	}
	return addManualFact(ctx, tx, key, "remove", "", at)
}

func mustBeManual(ctx context.Context, tx *sql.Tx, key string) error {
	var manual bool
	err := tx.QueryRowContext(ctx, `SELECT manual FROM devices WHERE identity_key = ? AND status != 'merged_away'`, key).Scan(&manual)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !manual) {
		return ErrNotManual
	}
	return err
}

// macFree refuses mac when a device other than self (not merged away) has it.
func macFree(ctx context.Context, tx *sql.Tx, mac, self string) error {
	var name, ip string
	err := tx.QueryRowContext(ctx, `SELECT d.display_name, COALESCE((SELECT a.ip FROM device_addresses a
		WHERE a.device_id = d.id AND a.current = 1 ORDER BY a.subnet_id LIMIT 1), '')
		FROM devices d WHERE d.mac = ? AND d.status != 'merged_away' AND d.identity_key != ? LIMIT 1`, mac, self).Scan(&name, &ip)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return &ErrMACInUse{Name: name, IP: ip}
}

func addManualFact(ctx context.Context, tx *sql.Tx, key, field, value string, at time.Time) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO user_manual_devices (identity_key, field, value, at) VALUES (?, ?, ?, ?)`,
		key, field, value, store.FormatTime(at)); err != nil {
		return err
	}
	return applyManualFact(ctx, tx, key, field, value, store.FormatTime(at))
}

// applyManualFact applies one manual device fact to the projections (live and in a rebuild).
func applyManualFact(ctx context.Context, tx *sql.Tx, key, field, value, at string) error {
	var err error
	switch field {
	case "add":
		_, err = tx.ExecContext(ctx, `INSERT INTO devices (identity_key, identity_strength, status, first_seen, last_seen,
			display_name, manual) VALUES (?, ?, ?, ?, ?, '', 1)`, key, StrengthStrong, StatusOnline, at, at)
	case "mac":
		var mac any
		vendor, randomized := "", false
		if value != "" {
			mac, vendor, randomized = value, oui.Manufacturer(value), oui.IsRandomized(value)
		}
		_, err = tx.ExecContext(ctx, `UPDATE devices SET mac = ?, manufacturer = ?, mac_randomized = ?
			WHERE identity_key = ? AND manual = 1`, mac, vendor, randomized, key)
	case "remove":
		_, err = tx.ExecContext(ctx, `DELETE FROM devices WHERE identity_key = ? AND manual = 1 AND status != 'merged_away'`, key)
	default:
		err = fmt.Errorf("inventory: unknown manual device field %q", field)
	}
	return err
}

func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
