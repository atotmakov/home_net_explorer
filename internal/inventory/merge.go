package inventory

import (
	"context"
	"database/sql"
)

// foldWeakDevices merges weak (MAC-less) devices that the MAC-identified device devID has just
// been seen as: same subnet, and the same current IP or the same hostname (research R6).
func foldWeakDevices(ctx context.Context, tx *sql.Tx, devID int64, sn *scannedSubnet, ip, hostname, at string) error {
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT d.id FROM devices d
		LEFT JOIN device_addresses a ON a.device_id = d.id AND a.subnet_id = ? AND a.current = 1
		WHERE d.identity_strength = 'weak' AND d.status != 'merged_away' AND d.id != ?
		AND (a.ip = ? OR d.identity_key = ?)`,
		sn.id, devID, ip, WeakKey(sn.scan.CIDR, ip, hostnameOrSentinel(hostname)))
	if err != nil {
		return err
	}
	var weak []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		weak = append(weak, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range weak {
		if err := mergeInto(ctx, tx, id, devID, at); err != nil {
			return err
		}
	}
	return nil
}

// hostnameOrSentinel avoids matching every IP-keyed weak device when there is no hostname.
func hostnameOrSentinel(h string) string {
	if h == "" {
		return "\x00"
	}
	return h
}

// mergeInto folds device from into device to: history, addresses, events, links and the
// owner's notes move over; from becomes merged_away (kept for history) and a merged event is
// recorded on to.
func mergeInto(ctx context.Context, tx *sql.Tx, from, to int64, at string) error {
	stmts := []struct {
		q    string
		args []any
	}{
		{`UPDATE sightings SET device_id = ? WHERE device_id = ?`, []any{to, from}},
		{`INSERT INTO device_addresses (device_id, subnet_id, ip, current, as_of)
			SELECT ?, subnet_id, ip, current, as_of FROM device_addresses WHERE device_id = ?
			ON CONFLICT (device_id, subnet_id, ip) DO UPDATE SET as_of = max(as_of, excluded.as_of)`, []any{to, from}},
		{`DELETE FROM device_addresses WHERE device_id = ?`, []any{from}},
		// One current address per subnet: the most recent.
		{`UPDATE device_addresses SET current = (as_of = (SELECT max(b.as_of) FROM device_addresses b
			WHERE b.device_id = device_addresses.device_id AND b.subnet_id = device_addresses.subnet_id))
			WHERE device_id = ?`, []any{to}},
		{`UPDATE OR IGNORE events SET device_id = ? WHERE device_id = ?`, []any{to, from}},
		{`DELETE FROM events WHERE device_id = ?`, []any{from}},
		{`UPDATE links SET from_device_id = ? WHERE from_device_id = ?`, []any{to, from}},
		{`UPDATE links SET to_device_id = ? WHERE to_device_id = ?`, []any{to, from}},
		{`UPDATE devices SET
			first_seen = min(first_seen, (SELECT first_seen FROM devices WHERE id = ?)),
			last_seen = max(last_seen, (SELECT last_seen FROM devices WHERE id = ?)),
			user_name = CASE WHEN user_name = '' THEN (SELECT user_name FROM devices WHERE id = ?) ELSE user_name END,
			notes = CASE WHEN notes = '' THEN (SELECT notes FROM devices WHERE id = ?) ELSE notes END,
			type = CASE WHEN type = '' THEN (SELECT type FROM devices WHERE id = ?) ELSE type END
			WHERE id = ?`, []any{from, from, from, from, from, to}},
		{`UPDATE devices SET status = 'merged_away', merged_into = ? WHERE id = ?`, []any{to, from}},
	}
	for _, s := range stmts {
		if _, err := tx.ExecContext(ctx, s.q, s.args...); err != nil {
			return err
		}
	}
	var fromKey, toKey string
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT identity_key FROM devices WHERE id = ?), (SELECT identity_key FROM devices WHERE id = ?)`,
		from, to).Scan(&fromKey, &toKey); err != nil {
		return err
	}
	if err := addEvent(ctx, tx, at, to, EventMerged, 0, fromKey, toKey, 0); err != nil {
		return err
	}
	return refreshDisplayName(ctx, tx, to)
}
