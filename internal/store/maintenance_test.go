package store_test

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/inventory"
	it "github.com/atotmakov/home_net_explorer/internal/inventory/inventorytest"
	"github.com/atotmakov/home_net_explorer/internal/store"
)

// Feature 004: maintenance actions (remove all devices, remove all collectors, drop all data).

var removeAt = it.T0.Add(2 * time.Hour)

// usedStore builds a store with history from two collectors (one of them revoked later), owner
// edits of every kind, a router, subnet facts, an owner password, a session and settings.
func usedStore(t *testing.T) *it.Harness {
	t.Helper()
	ctx := context.Background()
	h := it.New(t)
	h.Ingest(it.Run{Collector: "desktop", Subnets: []contract.SubnetScan{it.Scan("10.20.30.0/24")},
		Obs: []contract.Observation{it.ARP("10.20.30.44", "52:54:00:12:34:56", "pc.lan")}})
	h.Ingest(it.Run{Start: it.T0.Add(5 * time.Minute), Subnets: []contract.SubnetScan{it.Scan("192.168.1.0/24")},
		Obs: []contract.Observation{it.ARP("192.168.1.20", "00:11:32:aa:bb:cc", "nas.lan"), it.ARP("192.168.1.1", "00:00:5e:10:00:01", "")}})
	h.Ingest(it.Run{Collector: "laptop", Start: it.T0.Add(6 * time.Minute), Subnets: []contract.SubnetScan{it.Scan("192.168.1.0/24")},
		Obs: []contract.Observation{it.ARP("192.168.1.20", "00:11:32:aa:bb:cc", "nas.lan")}})
	h.Fact(func(tx *sql.Tx) error {
		return inventory.SetDeviceAttr(ctx, tx, "mac:00:11:32:aa:bb:cc", "name", "NAS", it.T0.Add(10*time.Minute))
	})
	h.Fact(func(tx *sql.Tx) error {
		return inventory.SetDeviceAttr(ctx, tx, "mac:00:00:5e:10:00:01", "type", "router", it.T0.Add(10*time.Minute))
	})
	h.Fact(func(tx *sql.Tx) error {
		return inventory.SetSubnetAttr(ctx, tx, "192.168.1.0/24", "name", "home", it.T0.Add(11*time.Minute))
	})
	h.Fact(func(tx *sql.Tx) error {
		return inventory.SetSubnetAttr(ctx, tx, "10.20.30.0/24", "ignored", "true", it.T0.Add(11*time.Minute))
	})
	h.Fact(func(tx *sql.Tx) error {
		return store.SaveRouter(ctx, tx, "mac:00:00:5e:10:00:01",
			store.RouterSettings{Model: "huawei-hg8145v5", Username: "root", Password: "pw"}, it.T0.Add(12*time.Minute))
	})
	db := h.Store.DB()
	for _, q := range []string{
		`INSERT INTO user_links (from_identity_key, to_identity_key, at) VALUES ('mac:00:11:32:aa:bb:cc', 'mac:00:00:5e:10:00:01', '2026-10-05T10:13:00.000Z')`,
		`INSERT INTO user_acks (identity_key, at) VALUES ('mac:00:11:32:aa:bb:cc', '2026-10-05T10:13:00.000Z')`,
		`INSERT INTO user_identity_alias (identity_key, target_identity_key, kind, at) VALUES ('mac:52:54:00:12:34:56', 'mac:00:11:32:aa:bb:cc', 'split', '2026-10-05T10:13:00.000Z')`,
		`INSERT INTO sessions (id_hash, created_at, expires_at) VALUES (x'01', '2026-10-05T10:00:00.000Z', '2026-11-05T10:00:00.000Z')`,
		`UPDATE collectors SET revoked_at = '2026-10-05T10:20:00.000Z', token_hash = x'02' WHERE name = 'laptop'`,
		`UPDATE collectors SET token_hash = x'03' WHERE name = 'desktop'`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	for k, v := range map[string]string{
		store.SettingOwnerPassword:         "hash",
		store.SettingRouterRejectedBuiltin: `["abc"]`,
		store.SettingBuiltinPaused:         "2026-10-05T10:30:00.000Z",
		"builtin_interval_seconds":         "1800",
		"offline_multiplier":               "5",
	} {
		if err := h.Store.SetSetting(ctx, k, v); err != nil {
			t.Fatal(err)
		}
	}
	return h
}

func rows(t *testing.T, h *it.Harness, table string) int {
	t.Helper()
	return scalarQ(t, h, `SELECT count(*) FROM `+table)
}

func scalarQ(t *testing.T, h *it.Harness, q string, args ...any) int {
	t.Helper()
	var n int
	if err := h.Store.DB().QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func setting(t *testing.T, h *it.Harness, key string) (string, bool) {
	t.Helper()
	v, ok, err := h.Store.Setting(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	return v, ok
}

// maintain runs f inside the ingest lock, as the web handlers do.
func maintain(t *testing.T, h *it.Harness, f func(ctx context.Context, tx *sql.Tx) (store.RemovalCounts, error)) store.RemovalCounts {
	t.Helper()
	var c store.RemovalCounts
	if err := h.Ingester.Do(context.Background(), func(tx *sql.Tx) error {
		var err error
		c, err = f(context.Background(), tx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return c
}

// assertRebuildInvariant: rebuilding from the remaining facts gives the live projections (FR-007).
func assertRebuildInvariant(t *testing.T, h *it.Harness) {
	t.Helper()
	before := h.Snapshot()
	if err := inventory.Rebuild(context.Background(), h.Store, h.Applier); err != nil {
		t.Fatal(err)
	}
	if after := h.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Errorf("rebuild changed projections:\nbefore %v\nafter  %v", before, after)
	}
}

func removeDevices(ctx context.Context, tx *sql.Tx) (store.RemovalCounts, error) {
	return store.RemoveAllDevices(ctx, tx, removeAt)
}

func removeCollectors(ctx context.Context, tx *sql.Tx) (store.RemovalCounts, error) {
	return store.RemoveAllCollectors(ctx, tx, removeAt)
}

var defaults = map[string]string{"builtin_interval_seconds": "900", "offline_multiplier": "3"}

func dropAll(ctx context.Context, tx *sql.Tx) (store.RemovalCounts, error) {
	return store.DropAllData(ctx, tx, removeAt, defaults)
}

var emptiedByRemoveDevices = []string{"collection_runs", "run_subnets", "run_sources", "devices", "device_addresses",
	"sightings", "events", "links", "subnets", "user_device_attrs", "user_identity_alias", "user_links", "user_acks",
	"router_settings"}

func TestRemoveAllDevices(t *testing.T) {
	h := usedStore(t)
	devices := scalarQ(t, h, `SELECT count(*) FROM devices WHERE status != 'merged_away'`)
	collectorsBefore := scalarQ(t, h, `SELECT count(*) FROM collectors`)
	var lastReport string
	h.Store.DB().QueryRow(`SELECT last_report_at FROM collectors WHERE name = 'desktop'`).Scan(&lastReport)

	c := maintain(t, h, removeDevices)
	if c.Devices != devices || c.Runs != 3 || c.Collectors != 0 {
		t.Errorf("counts = %+v, want devices %d, runs 3", c, devices)
	}
	for _, table := range emptiedByRemoveDevices {
		if n := rows(t, h, table); n != 0 {
			t.Errorf("%s: %d rows left", table, n)
		}
	}
	if _, ok := setting(t, h, store.SettingRouterRejectedBuiltin); ok {
		t.Error("the built-in collector's rejected router logins were kept")
	}
	// Kept: collectors (with their last report), sessions, owner, settings, pause, subnet facts.
	if n := rows(t, h, "collectors"); n != collectorsBefore {
		t.Errorf("collectors %d -> %d", collectorsBefore, n)
	}
	var lr string
	h.Store.DB().QueryRow(`SELECT last_report_at FROM collectors WHERE name = 'desktop'`).Scan(&lr)
	if lr != lastReport {
		t.Errorf("last_report_at %s -> %s", lastReport, lr)
	}
	if rows(t, h, "sessions") != 1 || rows(t, h, "user_subnet_attrs") != 2 {
		t.Error("sessions or subnet facts were removed")
	}
	for k, want := range map[string]string{store.SettingOwnerPassword: "hash", store.SettingBuiltinPaused: "2026-10-05T10:30:00.000Z",
		"builtin_interval_seconds": "1800", "offline_multiplier": "5", store.SettingDataResetAt: store.FormatTime(removeAt)} {
		if v, _ := setting(t, h, k); v != want {
			t.Errorf("setting %s = %q, want %q", k, v, want)
		}
	}
	assertRebuildInvariant(t, h)
	if n := rows(t, h, "devices"); n != 0 {
		t.Errorf("rebuild brought back %d devices", n)
	}
	if c := maintain(t, h, removeDevices); c != (store.RemovalCounts{}) {
		t.Errorf("second removal counts = %+v, want zero", c)
	}
}

// FR-004: a failure part-way leaves everything as it was.
func TestRemoveAllDevicesIsAllOrNothing(t *testing.T) {
	h := usedStore(t)
	if _, err := h.Store.DB().Exec(`CREATE TRIGGER boom BEFORE DELETE ON user_acks BEGIN SELECT RAISE(ABORT, 'boom'); END`); err != nil {
		t.Fatal(err)
	}
	before := h.Snapshot()
	runs := rows(t, h, "collection_runs")
	err := h.Ingester.Do(context.Background(), func(tx *sql.Tx) error {
		_, err := store.RemoveAllDevices(context.Background(), tx, removeAt)
		return err
	})
	if err == nil {
		t.Fatal("the removal did not fail")
	}
	if rows(t, h, "collection_runs") != runs || rows(t, h, "router_settings") != 1 || rows(t, h, "user_device_attrs") != 2 {
		t.Error("a failed removal deleted some rows")
	}
	if after := h.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Error("a failed removal changed the projections")
	}
	if _, ok := setting(t, h, store.SettingDataResetAt); ok {
		t.Error("a failed removal set the reset point")
	}
}

func TestRemoveAllCollectors(t *testing.T) {
	ctx := context.Background()
	h := usedStore(t)
	before := h.Snapshot()
	runs := rows(t, h, "collection_runs")

	c := maintain(t, h, removeCollectors)
	if c.Collectors != 2 || c.Devices != 0 || c.Runs != 0 {
		t.Errorf("counts = %+v, want 2 collectors (active and revoked)", c)
	}
	for _, name := range []string{"desktop", "laptop"} {
		var deleted, revoked string
		var token []byte
		if err := h.Store.DB().QueryRow(`SELECT COALESCE(deleted_at, ''), COALESCE(revoked_at, ''), token_hash FROM collectors WHERE name = ?`, name).
			Scan(&deleted, &revoked, &token); err != nil {
			t.Fatal(err)
		}
		if deleted != store.FormatTime(removeAt) || token != nil || revoked == "" {
			t.Errorf("%s: deleted_at=%q revoked_at=%q token=%x", name, deleted, revoked, token)
		}
	}
	var laptopRevoked string
	h.Store.DB().QueryRow(`SELECT revoked_at FROM collectors WHERE name = 'laptop'`).Scan(&laptopRevoked)
	if laptopRevoked != "2026-10-05T10:20:00.000Z" {
		t.Errorf("an earlier revocation time was overwritten: %s", laptopRevoked)
	}
	if n := scalarQ(t, h, `SELECT count(*) FROM collectors WHERE name = 'nas' AND deleted_at IS NULL`); n != 1 {
		t.Error("the built-in collector was removed")
	}
	if rows(t, h, "collection_runs") != runs {
		t.Error("removing collectors deleted their history")
	}
	if after := h.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Error("removing collectors changed the inventory")
	}
	assertRebuildInvariant(t, h)

	cs, err := h.Store.ListCollectors(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 || cs[0].Name != "nas" {
		t.Errorf("ListCollectors = %v, want only nas", cs)
	}
	ov, err := h.Store.CollectorOverviews(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ov) != 1 {
		t.Errorf("CollectorOverviews lists %d collectors, want 1", len(ov))
	}
	subnets, err := h.Store.SubnetStatus(ctx, removeAt)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range subnets {
		if s.CIDR == "10.20.30.0/24" {
			found = true
			if s.DiscoveredBy != "desktop (removed)" || s.LastScannedBy != "desktop (removed)" {
				t.Errorf("subnet discovered by %q, last scanned by %q; want desktop (removed)", s.DiscoveredBy, s.LastScannedBy)
			}
		}
	}
	if !found {
		t.Error("the removed collector's subnet disappeared")
	}
	if c := maintain(t, h, removeCollectors); c.Collectors != 0 {
		t.Errorf("second removal counts = %+v, want zero", c)
	}

	// Once their history is gone, removed collectors are deleted for good.
	maintain(t, h, removeDevices)
	if n := rows(t, h, "collectors"); n != 1 {
		t.Errorf("%d collectors left after removing all devices, want only nas", n)
	}
}

func TestDropAllData(t *testing.T) {
	h := usedStore(t)
	devices := scalarQ(t, h, `SELECT count(*) FROM devices WHERE status != 'merged_away'`)
	maintain(t, h, removeCollectors) // one removed collector set, to be deleted too
	if _, err := h.Store.DB().Exec(`INSERT INTO collectors (name, kind, token_hash, created_at) VALUES ('tablet', 'remote', x'04', '2026-10-05T11:00:00.000Z')`); err != nil {
		t.Fatal(err)
	}

	c := maintain(t, h, dropAll)
	if c.Devices != devices || c.Runs != 3 || c.Collectors != 3 {
		t.Errorf("counts = %+v, want devices %d, runs 3, collectors 3", c, devices)
	}
	for _, table := range append(emptiedByRemoveDevices, "user_subnet_attrs") {
		if n := rows(t, h, table); n != 0 {
			t.Errorf("%s: %d rows left", table, n)
		}
	}
	if n := scalarQ(t, h, `SELECT count(*) FROM collectors WHERE name = 'nas'`); n != 1 || rows(t, h, "collectors") != 1 {
		t.Error("collectors left other than nas, or nas deleted")
	}
	if rows(t, h, "sessions") != 1 {
		t.Error("the owner's session was deleted")
	}
	want := map[string]string{store.SettingOwnerPassword: "hash", store.SettingDataResetAt: store.FormatTime(removeAt),
		"builtin_interval_seconds": "900", "offline_multiplier": "3"}
	got := map[string]string{}
	r, err := h.Store.DB().Query(`SELECT key, value FROM settings`)
	if err != nil {
		t.Fatal(err)
	}
	for r.Next() {
		var k, v string
		r.Scan(&k, &v)
		got[k] = v
	}
	r.Close()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("settings = %v, want %v", got, want)
	}
	assertRebuildInvariant(t, h)
}
