package inventory

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/store"
)

// DeviceTypes are the allowed values of a device's user-assigned type (data-model.md).
var DeviceTypes = []string{"router", "access_point", "switch", "computer", "phone", "tablet", "tv", "printer", "nas", "iot", "other"}

// ValidateDeviceAttr checks a user edit of a device field.
func ValidateDeviceAttr(field, value string) error {
	switch field {
	case "name":
		if len(value) > 100 {
			return fmt.Errorf("name is longer than 100 characters")
		}
	case "notes":
		if len(value) > 2000 {
			return fmt.Errorf("notes are longer than 2000 characters")
		}
	case "type":
		if value != "" && !slices.Contains(DeviceTypes, value) {
			return fmt.Errorf("unknown device type %q", value)
		}
	default:
		return fmt.Errorf("unknown device field %q", field)
	}
	return nil
}

// SetDeviceAttr records a user edit (an append-only fact keyed by identity_key) and applies it.
func SetDeviceAttr(ctx context.Context, tx *sql.Tx, identityKey, field, value string, at time.Time) error {
	if err := ValidateDeviceAttr(field, value); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO user_device_attrs (identity_key, field, value, at) VALUES (?, ?, ?, ?)`,
		identityKey, field, value, store.FormatTime(at)); err != nil {
		return err
	}
	return applyDeviceAttr(ctx, tx, identityKey, field, value)
}

func applyDeviceAttr(ctx context.Context, tx *sql.Tx, identityKey, field, value string) error {
	col := map[string]string{"name": "user_name", "notes": "notes", "type": "type"}[field]
	if col == "" {
		return fmt.Errorf("unknown device field %q", field)
	}
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM devices WHERE identity_key = ?`, identityKey).Scan(&id)
	if err != nil {
		return fmt.Errorf("inventory: device %s: %w", identityKey, err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE devices SET `+col+` = ? WHERE id = ?`, value, id); err != nil {
		return err
	}
	return refreshDisplayName(ctx, tx, id)
}

// SetSubnetAttr records a subnet rename or ignore/unignore and applies it. For "ignored" the
// value is "true" or "false".
func SetSubnetAttr(ctx context.Context, tx *sql.Tx, cidr, field, value string, at time.Time) error {
	switch {
	case field == "name" && len(value) <= 100:
	case field == "ignored" && (value == "true" || value == "false"):
	default:
		return fmt.Errorf("invalid subnet attribute %s=%q", field, value)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO user_subnet_attrs (cidr, field, value, at) VALUES (?, ?, ?, ?)`,
		cidr, field, value, store.FormatTime(at)); err != nil {
		return err
	}
	return applySubnetAttr(ctx, tx, cidr, field, value)
}

func applySubnetAttr(ctx context.Context, tx *sql.Tx, cidr, field, value string) error {
	var err error
	switch field {
	case "name":
		_, err = tx.ExecContext(ctx, `UPDATE subnets SET name = ? WHERE cidr = ?`, value, cidr)
	case "ignored":
		_, err = tx.ExecContext(ctx, `UPDATE subnets SET ignored = ? WHERE cidr = ?`, value == "true", cidr)
	default:
		err = fmt.Errorf("unknown subnet field %q", field)
	}
	return err
}
