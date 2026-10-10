package inventory_test

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/inventory"
	it "github.com/atotmakov/home_net_explorer/internal/inventory/inventorytest"
)

type deviceRow struct {
	ID           int64
	IdentityKey  string
	Strength     string
	MAC          string
	Randomized   bool
	Manufacturer string
	Hostname     string
	Status       string
	FirstSeen    string
	LastSeen     string
	DisplayName  string
}

func devices(t *testing.T, h *it.Harness) []deviceRow {
	t.Helper()
	rows, err := h.Store.DB().Query(`SELECT id, identity_key, identity_strength, COALESCE(mac, ''), mac_randomized,
		manufacturer, hostname, status, first_seen, last_seen, display_name FROM devices ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []deviceRow
	for rows.Next() {
		var d deviceRow
		if err := rows.Scan(&d.ID, &d.IdentityKey, &d.Strength, &d.MAC, &d.Randomized, &d.Manufacturer,
			&d.Hostname, &d.Status, &d.FirstSeen, &d.LastSeen, &d.DisplayName); err != nil {
			t.Fatal(err)
		}
		out = append(out, d)
	}
	return out
}

func scalar(t *testing.T, h *it.Harness, q string, args ...any) int {
	t.Helper()
	var n int
	if err := h.Store.DB().QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestFirstRunCreatesDevices(t *testing.T) {
	h := it.New(t)
	h.Ingest(it.Run{Subnets: []contract.SubnetScan{it.Scan("192.168.1.0/24")}, Obs: []contract.Observation{
		it.ARP("192.168.1.20", "00:11:32:aa:bb:cc", "nas.lan"),
		it.ARP("192.168.1.50", "da:a1:19:01:02:03", ""),
	}})
	ds := devices(t, h)
	if len(ds) != 2 {
		t.Fatalf("devices = %d, want 2", len(ds))
	}
	nas, phone := ds[0], ds[1]
	if nas.IdentityKey != "mac:00:11:32:aa:bb:cc" || nas.Strength != "strong" {
		t.Errorf("nas identity = %s/%s", nas.IdentityKey, nas.Strength)
	}
	if !strings.Contains(nas.Manufacturer, "Synology") || nas.Randomized {
		t.Errorf("nas manufacturer = %q randomized=%v", nas.Manufacturer, nas.Randomized)
	}
	if !phone.Randomized || phone.Manufacturer != "" {
		t.Errorf("randomized MAC: randomized=%v manufacturer=%q, want true and empty", phone.Randomized, phone.Manufacturer)
	}
	if nas.DisplayName != "nas.lan" || phone.DisplayName != "192.168.1.50" {
		t.Errorf("display names = %q, %q (hostname, else IP)", nas.DisplayName, phone.DisplayName)
	}
	if nas.Status != "online" {
		t.Errorf("status = %s, want online", nas.Status)
	}
	if n := scalar(t, h, `SELECT count(*) FROM device_addresses WHERE device_id = ? AND ip = '192.168.1.20' AND current = 1`, nas.ID); n != 1 {
		t.Errorf("current address rows = %d", n)
	}
	if n := scalar(t, h, `SELECT seen_count FROM sightings WHERE device_id = ?`, nas.ID); n != 1 {
		t.Errorf("seen_count = %d", n)
	}
}

func TestSightingsAreRunLengthCompressed(t *testing.T) {
	h := it.New(t)
	scan := []contract.SubnetScan{it.Scan("192.168.1.0/24")}
	nas := it.ARP("192.168.1.20", "00:11:32:aa:bb:cc", "nas.lan")
	h.Ingest(it.Run{Start: it.T0, Subnets: scan, Obs: []contract.Observation{nas}})
	h.Ingest(it.Run{Start: it.T0.Add(15 * time.Minute), Subnets: scan, Obs: []contract.Observation{nas}})

	if n := scalar(t, h, `SELECT count(*) FROM sightings`); n != 1 {
		t.Errorf("identical runs created %d sighting rows, want 1", n)
	}
	if n := scalar(t, h, `SELECT seen_count FROM sightings`); n != 2 {
		t.Errorf("seen_count = %d, want 2", n)
	}
	if d := devices(t, h)[0]; d.LastSeen != "2026-10-05T10:15:00.000Z" || d.FirstSeen != "2026-10-05T10:00:00.000Z" {
		t.Errorf("first/last seen = %s/%s", d.FirstSeen, d.LastSeen)
	}

	renamed := it.ARP("192.168.1.20", "00:11:32:aa:bb:cc", "diskstation.lan")
	h.Ingest(it.Run{Start: it.T0.Add(30 * time.Minute), Subnets: scan, Obs: []contract.Observation{renamed}})
	if n := scalar(t, h, `SELECT count(*) FROM sightings`); n != 2 {
		t.Errorf("hostname change: sighting rows = %d, want 2", n)
	}
	if d := devices(t, h)[0]; d.Hostname != "diskstation.lan" || d.DisplayName != "diskstation.lan" {
		t.Errorf("hostname not updated: %+v", d)
	}
}

func TestDeviceIDsFollowIngestOrder(t *testing.T) {
	h := it.New(t)
	scan := []contract.SubnetScan{it.Scan("192.168.1.0/24")}
	h.Ingest(it.Run{Subnets: scan, Obs: []contract.Observation{it.ARP("192.168.1.30", "00:00:00:00:00:30", "")}})
	h.Ingest(it.Run{Start: it.T0.Add(time.Minute), Subnets: scan, Obs: []contract.Observation{
		it.ARP("192.168.1.10", "00:00:00:00:00:10", ""), it.ARP("192.168.1.30", "00:00:00:00:00:30", "")}})
	ds := devices(t, h)
	if ds[0].MAC != "00:00:00:00:00:30" || ds[1].MAC != "00:00:00:00:00:10" {
		t.Errorf("ids not in ingest order: %+v", ds)
	}
}

func TestUserNameWinsDisplayName(t *testing.T) {
	h := it.New(t)
	h.Ingest(it.Run{Subnets: []contract.SubnetScan{it.Scan("192.168.1.0/24")}, Obs: []contract.Observation{
		it.ARP("192.168.1.20", "00:11:32:aa:bb:cc", "nas.lan")}})
	h.Fact(func(tx *sql.Tx) error {
		return inventory.SetDeviceAttr(context.Background(), tx, "mac:00:11:32:aa:bb:cc", "name", "Family NAS", it.T0.Add(2*time.Minute))
	})
	if d := devices(t, h)[0]; d.DisplayName != "Family NAS" {
		t.Errorf("display name = %q, want the user name", d.DisplayName)
	}
	// A later scan must not overwrite the user's name.
	h.Ingest(it.Run{Start: it.T0.Add(15 * time.Minute), Subnets: []contract.SubnetScan{it.Scan("192.168.1.0/24")},
		Obs: []contract.Observation{it.ARP("192.168.1.20", "00:11:32:aa:bb:cc", "nas2.lan")}})
	if d := devices(t, h)[0]; d.DisplayName != "Family NAS" {
		t.Errorf("display name after scan = %q", d.DisplayName)
	}
	if err := inventory.ValidateDeviceAttr("type", "toaster"); err == nil {
		t.Error("unknown device type accepted")
	}
	if err := inventory.ValidateDeviceAttr("type", "nas"); err != nil {
		t.Error(err)
	}
}

func TestSubnetProjection(t *testing.T) {
	h := it.New(t)
	_, res := h.Ingest(it.Run{Collector: "desktop", Subnets: []contract.SubnetScan{
		it.Scan("10.20.30.0/24"), it.TooLarge("10.0.0.0/16")},
		Obs: []contract.Observation{it.ARP("10.20.30.44", "52:54:00:12:34:56", "")}})
	if !reflect.DeepEqual(res.NewSubnets, []string{"10.20.30.0/24"}) {
		t.Errorf("new_subnets = %v, want only the scanned subnet", res.NewSubnets)
	}
	var first string
	var by int64
	if err := h.Store.DB().QueryRow(`SELECT first_seen_at, discovered_by FROM subnets WHERE cidr = '10.20.30.0/24'`).Scan(&first, &by); err != nil {
		t.Fatal(err)
	}
	if first != "2026-10-05T10:00:00.000Z" || by != h.Collector("desktop") {
		t.Errorf("subnet row first_seen_at=%s discovered_by=%d", first, by)
	}
	if n := scalar(t, h, `SELECT count(*) FROM subnets WHERE cidr = '10.0.0.0/16'`); n != 0 {
		t.Error("a skipped (too large) subnet must not become a subnet record")
	}
	_, res2 := h.Ingest(it.Run{Collector: "desktop", Start: it.T0.Add(15 * time.Minute),
		Subnets: []contract.SubnetScan{it.Scan("10.20.30.0/24")}})
	if len(res2.NewSubnets) != 0 {
		t.Errorf("second run reported new subnets %v", res2.NewSubnets)
	}
}

func TestIgnoredSubnetIsNotFolded(t *testing.T) {
	h := it.New(t)
	h.Ingest(it.Run{Subnets: []contract.SubnetScan{it.Scan("10.20.30.0/24")},
		Obs: []contract.Observation{it.ARP("10.20.30.44", "52:54:00:12:34:56", "")}})
	h.Fact(func(tx *sql.Tx) error {
		return inventory.SetSubnetAttr(context.Background(), tx, "10.20.30.0/24", "ignored", "true", it.T0.Add(5*time.Minute))
	})
	h.Fact(func(tx *sql.Tx) error {
		return inventory.SetSubnetAttr(context.Background(), tx, "10.20.30.0/24", "name", "Lab", it.T0.Add(6*time.Minute))
	})
	run, _ := h.Ingest(it.Run{Start: it.T0.Add(15 * time.Minute), Subnets: []contract.SubnetScan{it.Scan("10.20.30.0/24")},
		Obs: []contract.Observation{it.ARP("10.20.30.45", "52:54:00:12:34:57", "")}})

	if n := scalar(t, h, `SELECT count(*) FROM devices WHERE mac = '52:54:00:12:34:57'`); n != 0 {
		t.Error("observation of an ignored subnet was folded into devices")
	}
	if n := scalar(t, h, `SELECT count(*) FROM collection_runs WHERE collection_id = ?`, run.CollectionID); n != 1 {
		t.Error("the raw run must still be stored")
	}
	var name string
	var ignored bool
	h.Store.DB().QueryRow(`SELECT name, ignored FROM subnets WHERE cidr = '10.20.30.0/24'`).Scan(&name, &ignored)
	if name != "Lab" || !ignored {
		t.Errorf("subnet name=%q ignored=%v", name, ignored)
	}
}

func TestRebuildReproducesProjections(t *testing.T) {
	h := it.New(t)
	scan := []contract.SubnetScan{it.Scan("192.168.1.0/24")}
	h.Ingest(it.Run{Subnets: scan, Obs: []contract.Observation{
		it.ARP("192.168.1.20", "00:11:32:aa:bb:cc", "nas.lan"), it.ARP("192.168.1.50", "da:a1:19:01:02:03", "")}})
	h.Fact(func(tx *sql.Tx) error {
		return inventory.SetDeviceAttr(context.Background(), tx, "mac:da:a1:19:01:02:03", "name", "Phone", it.T0.Add(3*time.Minute))
	})
	for i := 1; i <= 4; i++ {
		obs := []contract.Observation{it.ARP("192.168.1.20", "00:11:32:aa:bb:cc", "nas.lan")}
		h.Ingest(it.Run{Start: it.T0.Add(time.Duration(i) * 15 * time.Minute), Subnets: scan, Obs: obs})
	}
	before := h.Snapshot()
	if err := inventory.Rebuild(context.Background(), h.Store, h.Applier); err != nil {
		t.Fatal(err)
	}
	if after := h.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Errorf("rebuild changed projections:\nbefore %v\nafter  %v", before, after)
	}
}

// Feature 004 (research R4): subnet names and ignore choices kept by "remove all devices" apply
// again when the subnet is rediscovered, live and after a rebuild.
func TestSubnetFactsApplyOnRediscovery(t *testing.T) {
	h := it.New(t)
	ctx := context.Background()
	h.Fact(func(tx *sql.Tx) error {
		return inventory.SetSubnetAttr(ctx, tx, "10.20.30.0/24", "name", "lab", it.T0.Add(-time.Hour))
	})
	h.Fact(func(tx *sql.Tx) error {
		return inventory.SetSubnetAttr(ctx, tx, "10.20.30.0/24", "ignored", "true", it.T0.Add(-time.Hour))
	})
	h.Fact(func(tx *sql.Tx) error {
		return inventory.SetSubnetAttr(ctx, tx, "10.20.31.0/24", "name", "office", it.T0.Add(-time.Hour))
	})
	h.Ingest(it.Run{Subnets: []contract.SubnetScan{it.Scan("10.20.30.0/24"), it.Scan("10.20.31.0/24")},
		Obs: []contract.Observation{it.ARP("10.20.30.44", "52:54:00:12:34:56", ""), it.ARP("10.20.31.44", "52:54:00:12:34:57", "")}})

	var name string
	var ignored bool
	h.Store.DB().QueryRow(`SELECT name, ignored FROM subnets WHERE cidr = '10.20.30.0/24'`).Scan(&name, &ignored)
	if name != "lab" || !ignored {
		t.Errorf("rediscovered subnet name=%q ignored=%v, want lab/true", name, ignored)
	}
	if n := scalar(t, h, `SELECT count(*) FROM devices WHERE mac = '52:54:00:12:34:56'`); n != 0 {
		t.Error("an observation of a subnet ignored before it was rediscovered was folded")
	}
	h.Store.DB().QueryRow(`SELECT name, ignored FROM subnets WHERE cidr = '10.20.31.0/24'`).Scan(&name, &ignored)
	if name != "office" || ignored {
		t.Errorf("second subnet name=%q ignored=%v, want office/false", name, ignored)
	}
	if n := scalar(t, h, `SELECT count(*) FROM devices WHERE mac = '52:54:00:12:34:57'`); n != 1 {
		t.Error("the observation of a named (not ignored) subnet was not folded")
	}

	before := h.Snapshot()
	if err := inventory.Rebuild(ctx, h.Store, h.Applier); err != nil {
		t.Fatal(err)
	}
	if after := h.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Errorf("rebuild changed projections:\nbefore %v\nafter  %v", before, after)
	}
}
