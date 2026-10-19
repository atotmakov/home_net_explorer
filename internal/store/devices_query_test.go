package store_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/inventory"
	it "github.com/atotmakov/home_net_explorer/internal/inventory/inventorytest"
	"github.com/atotmakov/home_net_explorer/internal/store"
)

func seed(t *testing.T) *it.Harness {
	h := it.New(t)
	h.Ingest(it.Run{Subnets: []contract.SubnetScan{it.Scan("192.168.1.0/24")}, Obs: []contract.Observation{
		it.ARP("192.168.1.100", "a0:b1:c2:d3:e4:f5", "router.lan"),
		it.ARP("192.168.1.20", "00:11:32:aa:bb:cc", "nas.lan"),
		it.ARP("192.168.1.9", "da:a1:19:01:02:03", ""),
	}})
	h.Ingest(it.Run{Collector: "desktop", Start: it.T0.Add(5 * time.Minute),
		Subnets: []contract.SubnetScan{it.Scan("192.168.8.0/24")}, Obs: []contract.Observation{
			it.ARP("192.168.8.23", "3c:22:fb:44:55:66", "printer.local"),
		}})
	return h
}

func ips(rows []store.DeviceRow) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.PrimaryIP())
	}
	return out
}

func TestListDevicesSortsIPNumerically(t *testing.T) {
	h := seed(t)
	rows, err := h.Store.ListDevices(context.Background(), store.DeviceFilter{Sort: "ip"})
	if err != nil {
		t.Fatal(err)
	}
	got := ips(rows)
	want := []string{"192.168.1.9", "192.168.1.20", "192.168.1.100", "192.168.8.23"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ip sort = %v, want %v (numeric, not lexical)", got, want)
		}
	}
	rows, _ = h.Store.ListDevices(context.Background(), store.DeviceFilter{Sort: "ip", Desc: true})
	if ips(rows)[0] != "192.168.8.23" {
		t.Errorf("descending sort = %v", ips(rows))
	}
}

func TestListDevicesFilters(t *testing.T) {
	h := seed(t)
	ctx := context.Background()
	cases := []struct {
		name string
		f    store.DeviceFilter
		want int
	}{
		{"query by hostname", store.DeviceFilter{Q: "nas"}, 1},
		{"query by mac", store.DeviceFilter{Q: "3c:22"}, 1},
		{"query by ip", store.DeviceFilter{Q: "192.168.1."}, 3},
		{"subnet", store.DeviceFilter{Subnet: "192.168.8.0/24"}, 1},
		{"status", store.DeviceFilter{Status: "online"}, 4},
		{"manufacturer", store.DeviceFilter{Manufacturer: "synology"}, 1},
		{"randomized", store.DeviceFilter{Randomized: true}, 1},
		{"seen after", store.DeviceFilter{SeenAfter: it.T0.Add(2 * time.Minute)}, 1},
		{"seen before", store.DeviceFilter{SeenBefore: it.T0.Add(2 * time.Minute)}, 3},
	}
	for _, c := range cases {
		rows, err := h.Store.ListDevices(ctx, c.f)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != c.want {
			t.Errorf("%s: %d rows, want %d (%v)", c.name, len(rows), c.want, ips(rows))
		}
	}
	for _, s := range []string{"mac", "hostname", "manufacturer", "first_seen", "last_seen", "name"} {
		if _, err := h.Store.ListDevices(ctx, store.DeviceFilter{Sort: s}); err != nil {
			t.Errorf("sort %s: %v", s, err)
		}
	}
	if _, err := h.Store.ListDevices(ctx, store.DeviceFilter{Sort: "bogus"}); err == nil {
		t.Error("unknown sort key accepted")
	}
}

func TestGetDeviceAndCounts(t *testing.T) {
	h := seed(t)
	ctx := context.Background()
	rows, _ := h.Store.ListDevices(ctx, store.DeviceFilter{Q: "printer"})
	d, err := h.Store.GetDevice(ctx, rows[0].ID, it.T0.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if d.Hostname != "printer.local" || len(d.Addresses) != 1 || d.Addresses[0].CIDR != "192.168.8.0/24" {
		t.Errorf("device detail = %+v", d)
	}
	if _, err := h.Store.GetDevice(ctx, 9999, it.T0); err != store.ErrNotFound {
		t.Errorf("missing device: %v", err)
	}
	c, err := h.Store.HomeCounts(ctx, it.T0.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if c.Total != 4 || c.Online != 4 || c.Offline != 0 {
		t.Errorf("counts = %+v", c)
	}
	subs, err := h.Store.SubnetStatus(ctx, it.T0.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 2 || subs[0].LastScannedBy == "" {
		t.Errorf("subnet status = %+v", subs)
	}
}

// Feature 004 (research R4): ping lists the owner's ignore choices even for subnets not
// (re)discovered yet, e.g. right after "remove all devices".
func TestIgnoredSubnetsFromFacts(t *testing.T) {
	h := it.New(t)
	ctx := context.Background()
	fact := func(cidr, v string, at time.Time) {
		h.Fact(func(tx *sql.Tx) error { return inventory.SetSubnetAttr(ctx, tx, cidr, "ignored", v, at) })
	}
	fact("10.20.30.0/24", "true", it.T0)
	fact("10.20.31.0/24", "true", it.T0)
	fact("10.20.31.0/24", "false", it.T0.Add(time.Minute))
	got, err := h.Store.IgnoredSubnets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].String() != "10.20.30.0/24" {
		t.Errorf("IgnoredSubnets = %v, want [10.20.30.0/24]", got)
	}
}

// Feature 006: the shown state is online (seen within two scan intervals), stale (older), or
// offline (stored offline), computed from an injected "now".
func states(t *testing.T, h *it.Harness, now time.Time) map[string]string {
	t.Helper()
	rows, err := h.Store.ListDevices(context.Background(), store.DeviceFilter{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	for _, r := range rows {
		m[r.PrimaryIP()] = r.State
	}
	return m
}

func TestDeviceState(t *testing.T) {
	h := seed(t) // nas scans 192.168.1.0/24 at T0 (900 s); desktop scans 192.168.8.0/24 at T0+5m
	if got := states(t, h, it.T0.Add(10*time.Minute)); got["192.168.1.9"] != "online" || got["192.168.8.23"] != "online" {
		t.Errorf("10 min after the scans: %v, want all online", got)
	}
	// 31 min after T0: the 192.168.1.0/24 devices are older than 2 × 900 s, the printer is not.
	got := states(t, h, it.T0.Add(31*time.Minute))
	if got["192.168.1.20"] != "stale" || got["192.168.1.100"] != "stale" || got["192.168.8.23"] != "online" {
		t.Errorf("31 min after T0: %v, want 192.168.1.x stale and the printer online", got)
	}
	// One complete scan that misses 192.168.1.9 makes it offline (research R1).
	h.Ingest(it.Run{Start: it.T0.Add(15 * time.Minute), Subnets: []contract.SubnetScan{it.Scan("192.168.1.0/24")},
		Obs: []contract.Observation{it.ARP("192.168.1.100", "a0:b1:c2:d3:e4:f5", "router.lan"), it.ARP("192.168.1.20", "00:11:32:aa:bb:cc", "nas.lan")}})
	got = states(t, h, it.T0.Add(16*time.Minute))
	if got["192.168.1.9"] != "offline" || got["192.168.1.20"] != "online" {
		t.Errorf("after a scan without 192.168.1.9: %v", got)
	}
	rows, _ := h.Store.ListDevices(context.Background(), store.DeviceFilter{Q: "printer"})
	if d, err := h.Store.GetDevice(context.Background(), rows[0].ID, it.T0.Add(5*time.Minute+31*time.Minute)); err != nil || d.State != "stale" {
		t.Errorf("GetDevice state = %q (%v), want stale", d.State, err)
	}
}

// The interval is that of the latest complete scan of the device's subnet (any collector); with
// no complete scan, the built-in interval setting is used.
func TestDeviceStateInterval(t *testing.T) {
	h := it.New(t)
	x := it.ARP("10.1.1.5", "00:00:5e:00:01:05", "")
	h.Ingest(it.Run{Collector: "nas", Interval: 900, Subnets: []contract.SubnetScan{it.Scan("10.1.1.0/24")}, Obs: []contract.Observation{x}})
	h.Ingest(it.Run{Collector: "slow", Interval: 3600, Start: it.T0.Add(2 * time.Minute),
		Subnets: []contract.SubnetScan{it.Scan("10.1.1.0/24")}, Obs: []contract.Observation{x}})
	if got := states(t, h, it.T0.Add(42*time.Minute)); got["10.1.1.5"] != "online" {
		t.Errorf("40 min after the latest (3600 s) scan: %v, want online", got)
	}

	h = it.New(t)
	if err := h.Store.SetSetting(context.Background(), "builtin_interval_seconds", "60"); err != nil {
		t.Fatal(err)
	}
	h.Ingest(it.Run{Subnets: []contract.SubnetScan{it.Partial("10.2.2.0/24")}, Obs: []contract.Observation{it.ARP("10.2.2.5", "00:00:5e:00:02:05", "")}})
	got := states(t, h, it.T0.Add(3*time.Minute))
	if got["10.2.2.5"] != "stale" {
		t.Errorf("no complete scan, built-in interval 60 s, 3 min later: %v, want stale", got)
	}
}

// Home counts equal the Devices filter for each state; unknown filter values show everything.
func TestHomeCountsByState(t *testing.T) {
	h := seed(t)
	h.Ingest(it.Run{Start: it.T0.Add(15 * time.Minute), Subnets: []contract.SubnetScan{it.Scan("192.168.1.0/24")},
		Obs: []contract.Observation{it.ARP("192.168.1.100", "a0:b1:c2:d3:e4:f5", "router.lan")}})
	ctx := context.Background()
	now := it.T0.Add(40 * time.Minute) // router seen at T0+15 (online), printer at T0+5 (stale), two offline
	c, err := h.Store.HomeCounts(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if c.Online != 1 || c.Stale != 1 || c.Offline != 2 || c.Total != 4 {
		t.Errorf("counts = %+v, want 1 online / 1 stale / 2 offline", c)
	}
	for state, want := range map[string]int{"online": c.Online, "stale": c.Stale, "offline": c.Offline, "new": 4, "bogus": 4} {
		rows, err := h.Store.ListDevices(ctx, store.DeviceFilter{Status: state, Now: now})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != want {
			t.Errorf("filter %q: %d devices, want %d", state, len(rows), want)
		}
	}
}

// A subnet is stale when no complete scan of it is within two of the collector's intervals.
func TestSubnetStatusStale(t *testing.T) {
	h := seed(t) // 192.168.1.0/24 finished at T0+1m, 900 s
	for _, c := range []struct {
		at    time.Duration
		stale bool
	}{{29 * time.Minute, false}, {32 * time.Minute, true}} {
		subs, err := h.Store.SubnetStatus(context.Background(), it.T0.Add(c.at))
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range subs {
			if s.CIDR == "192.168.1.0/24" && s.Stale != c.stale {
				t.Errorf("at T0+%v stale = %v, want %v", c.at, s.Stale, c.stale)
			}
		}
	}
}
