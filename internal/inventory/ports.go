package inventory

import (
	"context"
	"database/sql"
	"errors"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/store"
)

// applyPortChecks stores the port checks of a run and derives the devices' ports (feature 015,
// data-model.md). A check is matched to a device by MAC, else by the device that currently holds
// its address; a check matching no device is dropped (research R10).
func applyPortChecks(ctx context.Context, tx *sql.Tx, collectorID int64, run *contract.CollectionRun) error {
	for _, c := range run.PortChecks {
		devID, subnetID, err := portCheckDevice(ctx, tx, c)
		if err != nil {
			return err
		}
		if devID == 0 {
			continue
		}
		finished := store.FormatTime(c.FinishedAt)
		res, err := tx.ExecContext(ctx, `INSERT INTO port_checks (collection_id, collector_id, device_id, subnet_id, ip,
			started_at, finished_at, complete, all_open) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			run.CollectionID, collectorID, devID, subnetID, c.IP, store.FormatTime(c.StartedAt), finished, c.Complete, c.AllOpen)
		if err != nil {
			return err
		}
		checkID, err := res.LastInsertId()
		if err != nil {
			return err
		}
		for _, o := range c.Open {
			if _, err := tx.ExecContext(ctx, `INSERT INTO port_findings (check_id, port, proto, service, service_source)
				VALUES (?, ?, ?, ?, ?)`, checkID, o.Port, o.Proto, o.Service, o.ServiceSource); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO device_ports (device_id, port, proto, service, state, ip,
				collector_id, first_open, last_open) VALUES (?, ?, ?, ?, 'open', ?, ?, ?, ?)
				ON CONFLICT (device_id, port, proto) DO UPDATE SET service = excluded.service, state = 'open',
				ip = excluded.ip, collector_id = excluded.collector_id, last_open = excluded.last_open, closed_at = ''`,
				devID, o.Port, o.Proto, o.Service, c.IP, collectorID, finished, finished); err != nil {
				return err
			}
		}
	}
	return nil
}

// portCheckDevice finds the device of a check and the subnet of its address (0 if none).
func portCheckDevice(ctx context.Context, tx *sql.Tx, c contract.PortCheck) (int64, any, error) {
	var devID int64
	if c.MAC != "" {
		err := tx.QueryRowContext(ctx, `SELECT id FROM devices WHERE mac = ? AND merged_into IS NULL
			ORDER BY last_seen DESC LIMIT 1`, c.MAC).Scan(&devID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return 0, nil, err
		}
	}
	if devID == 0 {
		err := tx.QueryRowContext(ctx, `SELECT a.device_id FROM device_addresses a JOIN devices d ON d.id = a.device_id
			WHERE a.ip = ? AND a.current = 1 AND d.merged_into IS NULL LIMIT 1`, c.IP).Scan(&devID)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil, nil
		}
		if err != nil {
			return 0, nil, err
		}
	}
	var subnetID any
	var sid int64
	err := tx.QueryRowContext(ctx, `SELECT subnet_id FROM device_addresses WHERE device_id = ? AND ip = ? LIMIT 1`,
		devID, c.IP).Scan(&sid)
	switch {
	case err == nil:
		subnetID = sid
	case !errors.Is(err, sql.ErrNoRows):
		return 0, nil, err
	}
	return devID, subnetID, nil
}
