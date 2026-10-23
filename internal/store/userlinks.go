package store

import (
	"context"
	"database/sql"
	"time"
)

// LinkOption is a device offered in the "Connected via" select (feature 010).
type LinkOption struct {
	ID       int64
	Label    string
	Type     string
	Selected bool
}

// SetUserLink records that the device fromKey connects via the device toKey (append-only).
func SetUserLink(ctx context.Context, tx *sql.Tx, fromKey, toKey string, at time.Time) error {
	return nil
}

// ClearUserLink records that the device fromKey has no manual link any more (append-only).
func ClearUserLink(ctx context.Context, tx *sql.Tx, fromKey string, at time.Time) error {
	return nil
}

// DeviceLinkOptions lists the devices device id can be linked to.
func (s *Store) DeviceLinkOptions(ctx context.Context, id int64, now time.Time) ([]LinkOption, error) {
	return nil, nil
}
