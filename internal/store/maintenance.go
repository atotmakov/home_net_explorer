package store

import (
	"context"
	"database/sql"
	"time"
)

// Settings keys used by the maintenance actions (feature 004).
const (
	SettingOwnerPassword = "owner_password_hash"
	// SettingDataResetAt is the time of the latest "remove all devices" or "drop all data". Runs
	// that started before it are discarded on arrival (research R2). It is never deleted.
	SettingDataResetAt = "data_reset_at"
	// SettingBuiltinPaused holds the time the built-in scanner was paused; absent when running.
	SettingBuiltinPaused = "builtin_paused"
)

// RemovalCounts is what a maintenance action removed.
type RemovalCounts struct {
	Devices    int // devices not merged away
	Runs       int // stored collection runs
	Collectors int // remote collectors
}

// RemoveAllDevices deletes every device with all scan history and all owner edits about devices
// (names, types, notes, links, acknowledgements, merges, router settings), facts and projections
// together, so a rebuild stays empty (Principle V). Subnet facts, collectors, sessions and
// settings are kept. Runs that started before at are discarded when they arrive later. Callers
// run it inside the ingest lock.
func RemoveAllDevices(ctx context.Context, tx *sql.Tx, at time.Time) (RemovalCounts, error) {
	var c RemovalCounts
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM devices WHERE status != 'merged_away'`).Scan(&c.Devices); err != nil {
		return c, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM collection_runs`).Scan(&c.Runs); err != nil {
		return c, err
	}
	if err := ResetProjections(ctx, tx); err != nil { // projections reference collectors and runs' subnets
		return c, err
	}
	for _, q := range []string{
		`DELETE FROM run_sources`,
		`DELETE FROM run_subnets`,
		`DELETE FROM collection_runs`,
		`DELETE FROM user_device_attrs`,
		`DELETE FROM user_identity_alias`,
		`DELETE FROM user_links`,
		`DELETE FROM user_acks`,
		`DELETE FROM router_settings`,
		`DELETE FROM collectors WHERE deleted_at IS NOT NULL`, // removed collectors had only history left
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return c, err
		}
	}
	if err := setTx(ctx, tx, SettingDataResetAt, FormatTime(at)); err != nil {
		return c, err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, SettingRouterRejectedBuiltin)
	return c, err
}

// RemoveAllCollectors removes every remote collector, active or revoked: it stops working at
// once (token cleared) and is no longer listed, and its name can be reused. Its history is kept
// and still names it (soft delete, research R3). The built-in collector is kept.
func RemoveAllCollectors(ctx context.Context, tx *sql.Tx, at time.Time) (RemovalCounts, error) {
	var c RemovalCounts
	res, err := tx.ExecContext(ctx, `UPDATE collectors SET deleted_at = ?, token_hash = NULL,
		revoked_at = COALESCE(revoked_at, ?) WHERE kind = ? AND deleted_at IS NULL`, FormatTime(at), FormatTime(at), KindRemote)
	if err != nil {
		return c, err
	}
	n, err := res.RowsAffected()
	c.Collectors = int(n)
	return c, err
}

// DropAllData deletes everything except the owner (password and sessions) and the built-in
// collector: what RemoveAllDevices deletes, plus subnet facts, all remote collectors and every
// setting, after which defaults (the server's start-up values) are written again. The built-in
// scanner is no longer paused.
func DropAllData(ctx context.Context, tx *sql.Tx, at time.Time, defaults map[string]string) (RemovalCounts, error) {
	var remote int // removed ones included
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM collectors WHERE kind = ?`, KindRemote).Scan(&remote); err != nil {
		return RemovalCounts{}, err
	}
	c, err := RemoveAllDevices(ctx, tx, at)
	if err != nil {
		return c, err
	}
	c.Collectors = remote
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_subnet_attrs`); err != nil {
		return c, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM collectors WHERE kind = ?`, KindRemote); err != nil {
		return c, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM settings WHERE key NOT IN (?, ?)`, SettingOwnerPassword, SettingDataResetAt); err != nil {
		return c, err
	}
	for k, v := range defaults {
		if err := setTx(ctx, tx, k, v); err != nil {
			return c, err
		}
	}
	return c, nil
}

func setTx(ctx context.Context, tx *sql.Tx, key, value string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}
