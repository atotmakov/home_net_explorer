package store_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/inventory"
	it "github.com/atotmakov/home_net_explorer/internal/inventory/inventorytest"
	"github.com/atotmakov/home_net_explorer/internal/store"
)

// Feature 011, US1: a manual device is listed with the state "manual", matches no state or subnet
// filter, and counts in the total only (FR-002, FR-003, SC-004).
func TestManualDeviceInList(t *testing.T) {
	h := seed(t)
	ctx := context.Background()
	h.Fact(func(tx *sql.Tx) error {
		_, err := inventory.AddManualDevice(ctx, tx, inventory.ManualDevice{Name: "TL-SL1218P", Type: "switch"}, it.T0.Add(time.Minute))
		return err
	})
	now := it.T0.Add(10 * time.Minute)
	find := func(f store.DeviceFilter) (store.DeviceRow, bool) {
		t.Helper()
		f.Now = now
		rows, err := h.Store.ListDevices(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			if r.DisplayName == "TL-SL1218P" {
				return r, true
			}
		}
		return store.DeviceRow{}, false
	}
	d, ok := find(store.DeviceFilter{})
	if !ok || d.State != store.StateManual || !d.Manual || len(d.Addresses) != 0 || d.PrimaryIP() != "" {
		t.Fatalf("unfiltered list: %+v, %v; want state manual, no addresses", d, ok)
	}
	for _, f := range []store.DeviceFilter{{Status: store.StateOnline}, {Status: store.StateStale}, {Status: store.StateOffline}, {Subnet: "192.168.1.0/24"}} {
		if _, ok := find(f); ok {
			t.Errorf("filter %+v lists the manual device", f)
		}
	}
	if _, ok := find(store.DeviceFilter{Q: "sl1218"}); !ok {
		t.Error("text search does not find the manual device")
	}
	if g, err := h.Store.GetDevice(ctx, d.ID, now); err != nil || g.State != store.StateManual || !g.Manual {
		t.Errorf("GetDevice = %+v, %v", g, err)
	}

	c, err := h.Store.HomeCounts(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	all, _ := h.Store.ListDevices(ctx, store.DeviceFilter{Now: now})
	if c.Manual != 1 || c.Total != len(all) || c.Online+c.Stale+c.Offline+c.Manual != c.Total {
		t.Errorf("counts = %+v for %d listed devices; want Manual 1 and the parts adding up to Total", c, len(all))
	}
}
