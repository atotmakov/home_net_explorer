package store

import (
	"context"
	"database/sql"
)

// projectionTables lists the derived tables in dependency order (children first).
var projectionTables = []string{"links", "events", "sightings", "device_addresses", "devices", "subnets"}

// ResetProjections deletes every projection row, leaving facts untouched. Row ids restart at 1,
// so a rebuild reproduces ingest-order ids exactly.
func ResetProjections(ctx context.Context, tx *sql.Tx) error {
	for _, t := range projectionTables {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+t); err != nil {
			return err
		}
	}
	return nil
}
