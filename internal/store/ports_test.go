package store_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	it "github.com/atotmakov/home_net_explorer/internal/inventory/inventorytest"
	"github.com/atotmakov/home_net_explorer/internal/store"
)

// Feature 015 (research R5): the server decides which addresses are due for a port check.

const (
	macA = "00:00:5e:00:53:20" // never checked
	macB = "00:00:5e:00:53:21" // checked 21 hours ago
	macC = "00:00:5e:00:53:22" // checked 19 hours ago
)

// portScenario: three devices on the NAS's subnet; B and C checked before now.
func portScenario(t *testing.T, now time.Time) (*it.Harness, int64) {
	h := it.New(t)
	h.Ingest(it.Run{Subnets: []contract.SubnetScan{it.Scan("192.168.1.0/24")}, Obs: []contract.Observation{
		it.ARP("192.168.1.20", macA, ""), it.ARP("192.168.1.21", macB, ""), it.ARP("192.168.1.22", macC, "")}})
	for _, c := range []struct {
		ip, mac string
		ago     time.Duration
	}{{"192.168.1.21", macB, 21 * time.Hour}, {"192.168.1.22", macC, 19 * time.Hour}} {
		start := now.Add(-c.ago).Add(-30 * time.Second)
		h.Ingest(it.Run{Start: start, PortChecks: []contract.PortCheck{{IP: c.ip, MAC: c.mac, Complete: true,
			Open: []contract.OpenPort{{Port: 22, Proto: "tcp", Service: "ssh", ServiceSource: "table"}}}}})
	}
	// A later presence run keeps the NAS's latest presence run on the subnet.
	h.Ingest(it.Run{Start: now.Add(-time.Minute), Subnets: []contract.SubnetScan{it.Scan("192.168.1.0/24")}, Obs: []contract.Observation{
		it.ARP("192.168.1.20", macA, ""), it.ARP("192.168.1.21", macB, ""), it.ARP("192.168.1.22", macC, "")}})
	return h, h.Collector("nas")
}

func identityOf(t *testing.T, h *it.Harness, mac string) string {
	t.Helper()
	var k string
	if err := h.Store.DB().QueryRow(`SELECT identity_key FROM devices WHERE mac = ?`, mac).Scan(&k); err != nil {
		t.Fatal(err)
	}
	return k
}

func TestPortPlan(t *testing.T) {
	ctx := context.Background()
	day := it.T0.Add(72 * time.Hour).Truncate(24 * time.Hour)

	// After B's slot today: B (21 h) is due; A (never) is due; C (19 h) is not.
	probe, _ := portScenario(t, day.Add(12*time.Hour))
	slotB := time.Duration(store.PortSlot(identityOf(t, probe, macB))) * time.Minute
	now := day.Add(slotB + time.Hour)
	h, nas := portScenario(t, now)
	plan, err := h.Store.PortPlan(ctx, nas, now)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Enabled || !slices.Contains(plan.Due, "192.168.1.20") || !slices.Contains(plan.Due, "192.168.1.21") ||
		slices.Contains(plan.Due, "192.168.1.22") {
		t.Errorf("plan after B's slot = %+v, want .20 and .21 due, .22 not", plan)
	}
	if plan.Excluded == nil {
		t.Error("excluded must be an empty list, not null")
	}

	// Before B's slot today: B's check (21 h ago) is after its slot yesterday, so it is not due.
	now = day.Add(24*time.Hour + slotB - time.Hour)
	h, nas = portScenario(t, now)
	if plan, _ := h.Store.PortPlan(ctx, nas, now); slices.Contains(plan.Due, "192.168.1.21") {
		t.Errorf("plan before B's slot = %v, want .21 not due", plan.Due)
	}

	// A collector whose latest presence run scanned another subnet gets none of these.
	h.Ingest(it.Run{Collector: "desktop", Start: now.Add(-time.Minute), Subnets: []contract.SubnetScan{it.Scan("192.168.8.0/24")},
		Obs: []contract.Observation{it.ARP("192.168.8.20", "00:00:5e:00:53:30", "")}})
	plan, _ = h.Store.PortPlan(ctx, h.Collector("desktop"), now)
	if !slices.Equal(plan.Due, []string{"192.168.8.20"}) {
		t.Errorf("desktop plan = %v, want only its own subnet's device", plan.Due)
	}
}

func TestDevicePortsView(t *testing.T) {
	ctx := context.Background()
	now := it.T0.Add(72 * time.Hour)
	h, _ := portScenario(t, now)
	var b, a int64
	h.Store.DB().QueryRow(`SELECT id FROM devices WHERE mac = ?`, macB).Scan(&b)
	h.Store.DB().QueryRow(`SELECT id FROM devices WHERE mac = ?`, macA).Scan(&a)
	v, err := h.Store.DevicePorts(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Checked || v.LastCollector != "nas" || len(v.Open) != 1 || v.Open[0].Label() != "22/tcp" ||
		v.Open[0].Service != "ssh" || v.Open[0].IP != "192.168.1.21" || !v.Reachable {
		t.Errorf("B's ports = %+v", v)
	}
	if v.LastCheck.IsZero() || v.LastCheck.After(now) {
		t.Errorf("last check = %v", v.LastCheck)
	}
	if v, _ := h.Store.DevicePorts(ctx, a); v.Checked || len(v.Open) != 0 || !v.Reachable {
		t.Errorf("A's ports = %+v, want not checked yet but reachable", v)
	}
}
