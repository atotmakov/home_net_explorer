package store

import (
	"cmp"
	"context"
	"database/sql"
	"slices"
	"strings"
	"time"
)

// The owner's "connected via" links (feature 010, research R7) are user facts in user_links:
// append-only rows keyed by identity keys, the latest row per device wins, removed = 1 clears.
// They are inputs, not projections, so they survive scans and rebuilds.

// SetUserLink records that the device fromKey connects via the device toKey.
func SetUserLink(ctx context.Context, tx *sql.Tx, fromKey, toKey string, at time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO user_links (from_identity_key, to_identity_key, removed, at)
		VALUES (?, ?, 0, ?)`, fromKey, toKey, FormatTime(at))
	return err
}

// ClearUserLink records that the device fromKey has no manual link any more.
func ClearUserLink(ctx context.Context, tx *sql.Tx, fromKey string, at time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO user_links (from_identity_key, to_identity_key, removed, at)
		VALUES (?, '', 1, ?)`, fromKey, FormatTime(at))
	return err
}

// userLinkTargets returns each device's current manual link target, both ends resolved through
// merges. A link whose target is unknown maps to 0.
func (s *Store) userLinkTargets(ctx context.Context) (map[int64]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT from_identity_key, to_identity_key, removed FROM user_links ORDER BY at, id`)
	if err != nil {
		return nil, err
	}
	type row struct {
		from, to string
		removed  bool
	}
	var rs []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.from, &r.to, &r.removed); err != nil {
			rows.Close()
			return nil, err
		}
		rs = append(rs, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	resolved := map[string]int64{}
	resolve := func(key string) (int64, error) {
		if id, ok := resolved[key]; ok {
			return id, nil
		}
		id, err := s.resolveDevice(ctx, key)
		resolved[key] = id
		return id, err
	}
	out := map[int64]int64{}
	for _, r := range rs {
		from, err := resolve(r.from)
		if err != nil {
			return nil, err
		}
		if from == 0 {
			continue
		}
		if r.removed {
			delete(out, from)
			continue
		}
		to, err := resolve(r.to)
		if err != nil {
			return nil, err
		}
		out[from] = to
	}
	return out, nil
}

// LinkOption is a device offered in the "Connected via" select (feature 010).
type LinkOption struct {
	ID       int64
	Label    string
	Type     string
	Selected bool
}

// DeviceLinkOptions lists the devices that device id can be linked to: every other device of the
// Devices list, routers, switches and access points first (feature 011, FR-004), then by name;
// the current target is selected.
func (s *Store) DeviceLinkOptions(ctx context.Context, id int64, now time.Time) ([]LinkOption, error) {
	devs, err := s.ListDevices(ctx, DeviceFilter{Now: now})
	if err != nil {
		return nil, err
	}
	links, err := s.userLinkTargets(ctx)
	if err != nil {
		return nil, err
	}
	current := links[id]
	out := []LinkOption{}
	for _, d := range devs {
		if d.ID == id {
			continue
		}
		label := d.DisplayName
		if ip := d.PrimaryIP(); ip != "" && ip != label {
			label += " (" + ip + ")"
		}
		out = append(out, LinkOption{ID: d.ID, Label: label, Type: d.Type, Selected: d.ID == current})
	}
	rank := func(t string) int {
		switch t {
		case "router":
			return 0
		case "switch":
			return 1
		case "access_point":
			return 2
		}
		return 3
	}
	slices.SortStableFunc(out, func(a, b LinkOption) int {
		if c := cmp.Compare(rank(a.Type), rank(b.Type)); c != 0 {
			return c
		}
		return cmp.Compare(strings.ToLower(a.Label), strings.ToLower(b.Label))
	})
	return out, nil
}

// LinkLoop reports whether linking from to to would close a loop: to's chain of current links
// reaches from (feature 011, FR-009).
func (s *Store) LinkLoop(ctx context.Context, from, to int64) (bool, error) {
	links, err := s.userLinkTargets(ctx)
	if err != nil {
		return false, err
	}
	seen := map[int64]bool{}
	for cur := to; cur != 0 && !seen[cur]; cur = links[cur] {
		if cur == from {
			return true, nil
		}
		seen[cur] = true
	}
	return false, nil
}
