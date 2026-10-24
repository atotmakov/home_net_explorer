package store

import (
	"context"
	"database/sql"
)

// DeviceIDTx returns the id of the device with identityKey inside tx (feature 011: the page of a
// device just added by hand).
func DeviceIDTx(ctx context.Context, tx *sql.Tx, identityKey string) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM devices WHERE identity_key = ?`, identityKey).Scan(&id)
	return id, err
}
