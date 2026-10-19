package inventory_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/inventory"
	it "github.com/atotmakov/home_net_explorer/internal/inventory/inventorytest"
)

// Rule (feature 006, research R1): after a complete, not ignored scan R of subnet S, a device
// with a current address in S that R did not see goes offline when its last_seen is before
// R.started_at and it is not current in another subnet whose latest complete scan saw it.

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

func wentOfflineAt(t *testing.T, h *it.Harness, mac string) []string {
	t.Helper()
	rows, err := h.Store.DB().Query(`SELECT e.at FROM events e JOIN devices d ON d.id = e.device_id
		WHERE d.mac = ? AND e.type = 'went_offline' ORDER BY e.at`, mac)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var at string
		rows.Scan(&at)
		out = append(out, at)
	}
	return out
}

func minutes(m int) time.Time { return it.T0.Add(time.Duration(m) * time.Minute) }

func scanAt(h *it.Harness, collector string, m int, scan contract.SubnetScan, obs ...contract.Observation) *contract.CollectionRun {
	run, _ := h.Ingest(it.Run{Collector: collector, Start: minutes(m), Subnets: []contract.SubnetScan{scan}, Obs: obs})
	return run
}

func fmtTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

func TestOfflineAfterOneMissedScan(t *testing.T) {
	h := it.New(t)
	scanAt(h, "nas", 0, it.Scan(home), phone, nasDev)
	missed := scanAt(h, "nas", 15, it.Scan(home), nasDev)
	if s := status(t, h, phone.MAC); s != "offline" {
		t.Fatalf("after one missed complete scan status = %s, want offline", s)
	}
	if at := wentOfflineAt(t, h, phone.MAC); len(at) != 1 || at[0] != fmtTime(missed.FinishedAt) {
		t.Errorf("went_offline at %v, want the missing scan's finished_at %s", at, fmtTime(missed.FinishedAt))
	}
	if s := status(t, h, nasDev.MAC); s != "online" {
		t.Errorf("seen device status = %s", s)
	}
}

func TestComesBackOnline(t *testing.T) {
	h := it.New(t)
	scanAt(h, "nas", 0, it.Scan(home), phone)
	scanAt(h, "nas", 15, it.Scan(home))
	scanAt(h, "nas", 30, it.Scan(home), phone)
	if s := status(t, h, phone.MAC); s != "online" {
		t.Errorf("device seen again: status = %s, want online", s)
	}
}

// A spooled scan that started before the device's latest sighting is uploaded late: it must not
// mark the device offline.
func TestLateUploadDoesNotMarkOffline(t *testing.T) {
	h := it.New(t)
	late := h.Build(it.Run{Collector: "desktop", Start: minutes(10), Subnets: []contract.SubnetScan{it.Scan(home)}})
	scanAt(h, "nas", 20, it.Scan(home), phone)
	h.IngestRun(late, minutes(30))
	if s := status(t, h, phone.MAC); s != "online" {
		t.Errorf("a late upload of an older scan made the device %s", s)
	}
}

func TestIncompleteScansDontCount(t *testing.T) {
	h := it.New(t)
	scanAt(h, "nas", 0, it.Scan(home), phone)
	scanAt(h, "nas", 15, it.Partial(home))
	scanAt(h, "nas", 30, it.Partial(home))
	if s := status(t, h, phone.MAC); s != "online" {
		t.Errorf("incomplete scans marked the device %s", s)
	}
}

// Several collectors scan the same subnet: the latest complete scan of any of them decides.
func TestTwoCollectorsLatestScanDecides(t *testing.T) {
	h := it.New(t)
	scanAt(h, "desktop", 0, it.Scan(home), phone)
	scanAt(h, "nas", 5, it.Scan(home))
	if s := status(t, h, phone.MAC); s != "offline" {
		t.Errorf("the NAS's later complete scan missed the device: status = %s, want offline", s)
	}
}

// A device current in two subnets goes offline only when both subnets' latest complete scans
// missed it.
func TestTwoSubnetsOfflineOnlyWhenBothMiss(t *testing.T) {
	h := it.New(t)
	const lab = "10.9.9.0/24"
	nic1 := it.ARP("192.168.1.30", "00:00:5e:00:00:30", "")
	nic2 := it.ARP("10.9.9.30", "00:00:5e:00:00:30", "") // the same device (same MAC) in a second subnet
	scanAt(h, "nas", 0, it.Scan(home), nic1)
	scanAt(h, "lab", 5, it.Scan(lab), nic2)
	scanAt(h, "nas", 15, it.Scan(home))
	if s := status(t, h, nic1.MAC); s != "online" {
		t.Fatalf("missed in %s but seen by the latest scan of %s: status = %s, want online", home, lab, s)
	}
	missed := scanAt(h, "lab", 20, it.Scan(lab))
	if s := status(t, h, nic1.MAC); s != "offline" {
		t.Fatalf("missed by the latest scans of both subnets: status = %s, want offline", s)
	}
	if at := wentOfflineAt(t, h, nic1.MAC); len(at) != 1 || at[0] != fmtTime(missed.FinishedAt) {
		t.Errorf("went_offline at %v, want %s", at, fmtTime(missed.FinishedAt))
	}
}

// Rebuild reaches the same statuses and events (Principle V).
func TestRebuildSameStatuses(t *testing.T) {
	h := it.New(t)
	scanAt(h, "nas", 0, it.Scan(home), phone, nasDev)
	scanAt(h, "nas", 15, it.Scan(home), nasDev)
	late := h.Build(it.Run{Collector: "desktop", Start: minutes(20), Subnets: []contract.SubnetScan{it.Scan(home)}})
	scanAt(h, "nas", 30, it.Scan(home), nasDev, phone)
	h.IngestRun(late, minutes(40))
	scanAt(h, "nas", 45, it.Scan(home), nasDev)
	before := h.Snapshot()
	if err := inventory.Rebuild(context.Background(), h.Store, h.Applier); err != nil {
		t.Fatal(err)
	}
	if after := h.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Errorf("rebuild changed the projections:\nbefore %v\nafter  %v", before, after)
	}
}

// Reading subnet status with a later clock never changes the database.
func TestSubnetStatusReadOnly(t *testing.T) {
	h := it.New(t)
	scanAt(h, "nas", 0, it.Scan(home), phone)
	before := h.Snapshot()
	if _, err := h.Store.SubnetStatus(context.Background(), minutes(24*60)); err != nil {
		t.Fatal(err)
	}
	if after := h.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Error("reading status with a later clock changed the database")
	}
}
