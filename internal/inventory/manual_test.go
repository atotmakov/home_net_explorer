package inventory_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/inventory"
	it "github.com/atotmakov/home_net_explorer/internal/inventory/inventorytest"
	"github.com/atotmakov/home_net_explorer/internal/store"
)

// Feature 011: devices the owner adds by hand (data-model.md, research R1–R5).

func addManual(t *testing.T, h *it.Harness, m inventory.ManualDevice, at time.Time) string {
	t.Helper()
	var key string
	h.Fact(func(tx *sql.Tx) error {
		var err error
		key, err = inventory.AddManualDevice(context.Background(), tx, m, at)
		return err
	})
	return key
}

type manualRow struct {
	Manual, Strength, Status, MAC, Vendor, Name, Type, Notes, Display, First, Last string
	Addresses                                                                     int
}

func manualDevice(t *testing.T, h *it.Harness, key string) (manualRow, bool) {
	t.Helper()
	var r manualRow
	err := h.Store.DB().QueryRow(`SELECT manual, identity_strength, status, COALESCE(mac, ''), manufacturer, user_name, type,
		notes, display_name, first_seen, last_seen, (SELECT count(*) FROM device_addresses WHERE device_id = devices.id)
		FROM devices WHERE identity_key = ?`, key).Scan(&r.Manual, &r.Strength, &r.Status, &r.MAC, &r.Vendor, &r.Name,
		&r.Type, &r.Notes, &r.Display, &r.First, &r.Last, &r.Addresses)
	if errors.Is(err, sql.ErrNoRows) {
		return r, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return r, true
}

func TestAddManualDevice(t *testing.T) {
	h := it.New(t)
	at := it.T0.Add(time.Minute)
	key := addManual(t, h, inventory.ManualDevice{Name: "  TL-SL1218P ", Type: "switch", MAC: "00-11-32-AA-BB-01", Notes: "under the desk"}, at)
	if !strings.HasPrefix(key, "manual:") || len(key) < len("manual:")+32 {
		t.Fatalf("identity key = %q, want manual:<uuid>", key)
	}
	r, ok := manualDevice(t, h, key)
	want := manualRow{Manual: "1", Strength: "strong", Status: "online", MAC: "00:11:32:aa:bb:01", Name: "TL-SL1218P",
		Type: "switch", Notes: "under the desk", Display: "TL-SL1218P", First: store.FormatTime(at), Last: store.FormatTime(at)}
	r.Vendor = "" // checked below
	if !ok || r != want {
		t.Errorf("manual device = %+v, %v\nwant %+v", r, ok, want)
	}
	if r, _ := manualDevice(t, h, key); r.Vendor == "" {
		t.Error("no vendor from the MAC (00:11:32 is Synology)")
	}
	other := addManual(t, h, inventory.ManualDevice{Name: "hub"}, at)
	if other == key {
		t.Error("two manual devices share an identity key")
	}
	if r, _ := manualDevice(t, h, other); r.MAC != "" || r.Type != "" {
		t.Errorf("device without MAC and type = %+v", r)
	}
}

func TestManualDeviceValidation(t *testing.T) {
	h := it.New(t)
	h.Ingest(it.Run{Subnets: []contract.SubnetScan{it.Scan("192.168.0.0/24")}, Obs: []contract.Observation{
		it.ARP("192.168.0.20", "00:00:5e:00:53:20", "nas.lan")}})
	ctx := context.Background()
	try := func(m inventory.ManualDevice) error {
		return h.Ingester.Do(ctx, func(tx *sql.Tx) error {
			_, err := inventory.AddManualDevice(ctx, tx, m, it.T0.Add(time.Minute))
			return err
		})
	}
	for label, c := range map[string]struct {
		m    inventory.ManualDevice
		want string
	}{
		"empty name":   {inventory.ManualDevice{Name: "   "}, "a name is required"},
		"long name":    {inventory.ManualDevice{Name: strings.Repeat("x", 101)}, "longer than 100"},
		"bad type":     {inventory.ManualDevice{Name: "x", Type: "toaster"}, "unknown device type"},
		"long notes":   {inventory.ManualDevice{Name: "x", Notes: strings.Repeat("n", 2001)}, "longer than 2000"},
		"not a MAC":    {inventory.ManualDevice{Name: "x", MAC: "zz"}, "is not a MAC address"},
		"broadcast":    {inventory.ManualDevice{Name: "x", MAC: "ff:ff:ff:ff:ff:ff"}, "is not a MAC address"},
		"multicast":    {inventory.ManualDevice{Name: "x", MAC: "01:00:5e:00:00:fb"}, "is not a MAC address"},
		"MAC in use":   {inventory.ManualDevice{Name: "x", MAC: "00005E005320"}, "nas.lan (192.168.0.20)"},
		"MAC in use 2": {inventory.ManualDevice{Name: "x", MAC: "00:00:5E:00:53:20"}, "belongs to"},
	} {
		err := try(c.m)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", label, err, c.want)
		}
	}
	var inUse *inventory.ErrMACInUse
	if err := try(inventory.ManualDevice{Name: "x", MAC: "00:00:5e:00:53:20"}); !errors.As(err, &inUse) || inUse.Name != "nas.lan" {
		t.Errorf("MAC in use: %v, want ErrMACInUse naming nas.lan", err)
	}
	var n int
	h.Store.DB().QueryRow(`SELECT count(*) FROM devices WHERE manual = 1`).Scan(&n)
	if n != 0 {
		t.Errorf("%d manual devices saved by refused adds", n)
	}
}

// Owner facts are timed after the scans they follow (received at T0+1m+1s), as in real use: a
// rebuild replays runs and facts in time order.
func TestManualMACAndRemove(t *testing.T) {
	h := it.New(t)
	ctx := context.Background()
	h.Ingest(it.Run{Subnets: []contract.SubnetScan{it.Scan("192.168.0.0/24")}, Obs: []contract.Observation{
		it.ARP("192.168.0.4", "00:00:5e:00:53:04", "pc.lan"), it.ARP("192.168.0.20", "00:00:5e:00:53:20", "nas.lan")}})
	sw := addManual(t, h, inventory.ManualDevice{Name: "switch", Type: "switch"}, it.T0.Add(2*time.Minute))
	do := func(f func(tx *sql.Tx) error) error { return h.Ingester.Do(ctx, f) }

	for _, c := range []struct{ mac, want string }{{"00-00-5E-00-53-99", "00:00:5e:00:53:99"}, {"00:00:5e:00:53:99", "00:00:5e:00:53:99"}, {"", ""}} {
		if err := do(func(tx *sql.Tx) error { return inventory.SetManualMAC(ctx, tx, sw, c.mac, it.T0.Add(3*time.Minute)) }); err != nil {
			t.Fatalf("set MAC %q: %v", c.mac, err)
		}
		if r, _ := manualDevice(t, h, sw); r.MAC != c.want {
			t.Errorf("after setting %q: MAC = %q, want %q", c.mac, r.MAC, c.want)
		}
	}
	if err := do(func(tx *sql.Tx) error { return inventory.SetManualMAC(ctx, tx, sw, "00:00:5e:00:53:04", it.T0) }); err == nil {
		t.Error("a scanned device's MAC was accepted")
	}
	if err := do(func(tx *sql.Tx) error {
		return inventory.SetManualMAC(ctx, tx, inventory.MACKey("00:00:5e:00:53:04"), "00:00:5e:00:53:98", it.T0)
	}); !errors.Is(err, inventory.ErrNotManual) {
		t.Errorf("set MAC on a scanned device: %v, want ErrNotManual", err)
	}

	pc, nas := inventory.MACKey("00:00:5e:00:53:04"), inventory.MACKey("00:00:5e:00:53:20")
	h.Fact(func(tx *sql.Tx) error { return store.SetUserLink(ctx, tx, pc, sw, it.T0.Add(4*time.Minute)) })
	h.Fact(func(tx *sql.Tx) error { return store.SetUserLink(ctx, tx, sw, nas, it.T0.Add(4*time.Minute)) })
	if err := do(func(tx *sql.Tx) error { return inventory.RemoveManualDevice(ctx, tx, pc, it.T0.Add(5*time.Minute)) }); !errors.Is(err, inventory.ErrNotManual) {
		t.Errorf("remove a scanned device: %v, want ErrNotManual", err)
	}
	h.Fact(func(tx *sql.Tx) error { return inventory.RemoveManualDevice(ctx, tx, sw, it.T0.Add(5*time.Minute)) })
	if _, ok := manualDevice(t, h, sw); ok {
		t.Error("removed manual device still in devices")
	}
	var removed int
	var to string
	if err := h.Store.DB().QueryRow(`SELECT removed, to_identity_key FROM user_links WHERE from_identity_key = ? ORDER BY id DESC LIMIT 1`, pc).
		Scan(&removed, &to); err != nil || removed != 1 {
		t.Errorf("link to the removed device: removed=%d to=%q err=%v, want cleared", removed, to, err)
	}

	before := h.Snapshot()
	if err := inventory.Rebuild(ctx, h.Store, h.Applier); err != nil {
		t.Fatal(err)
	}
	if after := h.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Errorf("rebuild changed projections:\nbefore %v\nafter  %v", before, after)
	}
}

// SC-003 / FR-011: a manual device with its name, type, MAC and notes is reproduced by a rebuild.
func TestManualDeviceSurvivesRebuild(t *testing.T) {
	h := it.New(t)
	ctx := context.Background()
	h.Ingest(it.Run{Subnets: []contract.SubnetScan{it.Scan("192.168.0.0/24")}, Obs: []contract.Observation{
		it.ARP("192.168.0.20", "00:00:5e:00:53:20", "nas.lan")}})
	sw := addManual(t, h, inventory.ManualDevice{Name: "switch", Type: "switch", Notes: "n"}, it.T0.Add(2*time.Minute))
	h.Fact(func(tx *sql.Tx) error { return inventory.SetDeviceAttr(ctx, tx, sw, "name", "TL-SL1218P", it.T0.Add(3*time.Minute)) })
	h.Fact(func(tx *sql.Tx) error { return inventory.SetManualMAC(ctx, tx, sw, "00:00:5e:00:53:77", it.T0.Add(3*time.Minute)) })
	h.Ingest(it.Run{Start: it.T0.Add(15 * time.Minute), Subnets: []contract.SubnetScan{it.Scan("192.168.0.0/24")}, Obs: []contract.Observation{
		it.ARP("192.168.0.20", "00:00:5e:00:53:20", "nas.lan")}})

	before := h.Snapshot()
	if err := inventory.Rebuild(ctx, h.Store, h.Applier); err != nil {
		t.Fatal(err)
	}
	if after := h.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Errorf("rebuild changed projections:\nbefore %v\nafter  %v", before, after)
	}
	if r, ok := manualDevice(t, h, sw); !ok || r.Name != "TL-SL1218P" || r.MAC != "00:00:5e:00:53:77" || r.Manual != "1" {
		t.Errorf("after rebuild: %+v, %v", r, ok)
	}
}
