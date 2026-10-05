package inventory_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	it "github.com/atotmakov/home_net_explorer/internal/inventory/inventorytest"
)

// Rule (research R7):
//   - "A collector is active for subnet S at time t if it completed a scan of S within
//     3 × its interval before t."
//   - "After ingesting a completed scan R of subnet S, each device in S that R didn't see goes
//     offline when both of these hold: (1) every collector that is active for S at
//     R.finished_at has completed ≥ 3 scans of S since the device's last_seen, and none of those
//     scans saw it, and (2) R.finished_at − last_seen ≥ 3 × the interval of R's collector."

const home = "192.168.1.0/24"

var phone = it.ARP("192.168.1.50", "00:00:00:00:00:50", "")
var nasDev = it.ARP("192.168.1.20", "00:11:32:aa:bb:cc", "")

func status(t *testing.T, h *it.Harness, mac string) string {
	t.Helper()
	var s string
	if err := h.Store.DB().QueryRow(`SELECT status FROM devices WHERE mac = ?`, mac).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func minutes(m int) time.Time { return it.T0.Add(time.Duration(m) * time.Minute) }

func scanAt(h *it.Harness, collector string, m int, scan contract.SubnetScan, obs ...contract.Observation) *contract.CollectionRun {
	run, _ := h.Ingest(it.Run{Collector: collector, Start: minutes(m), Subnets: []contract.SubnetScan{scan}, Obs: obs})
	return run
}

func TestOfflineWithSilentCollector(t *testing.T) {
	h := it.New(t)
	scanAt(h, "nas", 0, it.Scan(home), phone, nasDev)
	scanAt(h, "nas", 15, it.Scan(home), nasDev)
	scanAt(h, "nas", 30, it.Scan(home), nasDev)
	if s := status(t, h, phone.MAC); s != "online" {
		t.Fatalf("after 2 missed scans status = %s, want online", s)
	}
	third := scanAt(h, "nas", 45, it.Scan(home), nasDev)
	if s := status(t, h, phone.MAC); s != "offline" {
		t.Fatalf("NAS active, desktop silent: after 3 missed scans status = %s, want offline", s)
	}
	var at string
	if err := h.Store.DB().QueryRow(`SELECT e.at FROM events e JOIN devices d ON d.id = e.device_id
		WHERE d.mac = ? AND e.type = 'went_offline'`, phone.MAC).Scan(&at); err != nil {
		t.Fatal(err)
	}
	if want := third.FinishedAt.UTC().Format("2006-01-02T15:04:05.000Z"); at != want {
		t.Errorf("went_offline at %s, want the triggering scan's finished_at %s", at, want)
	}
	if s := status(t, h, nasDev.MAC); s != "online" {
		t.Errorf("seen device status = %s", s)
	}

	scanAt(h, "nas", 60, it.Scan(home), nasDev, phone)
	if s := status(t, h, phone.MAC); s != "online" {
		t.Errorf("device seen again: status = %s, want online", s)
	}
}

func TestOfflineNeedsEveryActiveCollector(t *testing.T) {
	h := it.New(t)
	scanAt(h, "nas", 0, it.Scan(home), phone)
	scanAt(h, "desktop", 0, it.Scan(home), phone)
	scanAt(h, "nas", 15, it.Scan(home))
	scanAt(h, "desktop", 20, it.Scan(home))
	scanAt(h, "nas", 30, it.Scan(home))
	scanAt(h, "nas", 45, it.Scan(home))
	if s := status(t, h, phone.MAC); s != "online" {
		t.Fatalf("desktop (active) has only 1 scan since last_seen; status = %s, want online", s)
	}
	scanAt(h, "desktop", 35, it.Scan(home))
	if s := status(t, h, phone.MAC); s != "online" {
		t.Fatalf("desktop has 2 scans; status = %s, want online", s)
	}
	scanAt(h, "desktop", 50, it.Scan(home))
	if s := status(t, h, phone.MAC); s != "offline" {
		t.Fatalf("both active collectors have 3 scans; status = %s, want offline", s)
	}
}

func TestIncompleteScansDontCount(t *testing.T) {
	h := it.New(t)
	scanAt(h, "nas", 0, it.Scan(home), phone)
	scanAt(h, "nas", 15, it.Partial(home))
	scanAt(h, "nas", 30, it.Partial(home))
	scanAt(h, "nas", 45, it.Partial(home))
	scanAt(h, "nas", 60, it.Partial(home))
	if s := status(t, h, phone.MAC); s != "online" {
		t.Errorf("incomplete scans marked the device %s", s)
	}
}

func TestStaleSubnetKeepsStatus(t *testing.T) {
	h := it.New(t)
	scanAt(h, "nas", 0, it.Scan(home), phone)
	before := h.Snapshot()

	ctx := context.Background()
	subnets, err := h.Store.SubnetStatus(ctx, minutes(10))
	if err != nil {
		t.Fatal(err)
	}
	if len(subnets) != 1 || subnets[0].Stale {
		t.Errorf("subnet scanned 10 minutes ago reported stale: %+v", subnets)
	}
	// No scans for a day: the subnet is stale, but nothing in the database changes.
	subnets, err = h.Store.SubnetStatus(ctx, minutes(24*60))
	if err != nil {
		t.Fatal(err)
	}
	if !subnets[0].Stale {
		t.Error("subnet with no active collector not reported stale")
	}
	if s := status(t, h, phone.MAC); s != "online" {
		t.Errorf("status changed without scans: %s", s)
	}
	if after := h.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Error("reading status with a later clock changed the database")
	}
}

func TestMultiplierFromSettings(t *testing.T) {
	h := it.New(t)
	if err := h.Store.SetSetting(context.Background(), "offline_multiplier", "2"); err != nil {
		t.Fatal(err)
	}
	scanAt(h, "nas", 0, it.Scan(home), phone)
	scanAt(h, "nas", 15, it.Scan(home))
	scanAt(h, "nas", 30, it.Scan(home))
	if s := status(t, h, phone.MAC); s != "offline" {
		t.Errorf("with multiplier 2, 2 missed scans should mark offline; status = %s", s)
	}
}
