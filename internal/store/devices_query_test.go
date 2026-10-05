package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
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
	d, err := h.Store.GetDevice(ctx, rows[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Hostname != "printer.local" || len(d.Addresses) != 1 || d.Addresses[0].CIDR != "192.168.8.0/24" {
		t.Errorf("device detail = %+v", d)
	}
	if _, err := h.Store.GetDevice(ctx, 9999); err != store.ErrNotFound {
		t.Errorf("missing device: %v", err)
	}
	c, err := h.Store.HomeCounts(ctx)
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
