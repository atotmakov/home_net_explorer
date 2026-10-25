package inventory_test

import (
	"context"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/inventory"
	it "github.com/atotmakov/home_net_explorer/internal/inventory/inventorytest"
)

// A device that moved to another subnet: a complete scan of the old subnet that misses it ends
// its address there, when it was seen on another subnet after its last sighting on the old one.
// A device that is just switched off keeps its last address (it goes offline instead).

const lan = "192.168.0.0/24"

func currentIPs(t *testing.T, h *it.Harness, mac string) []string {
	t.Helper()
	rows, err := h.Store.DB().Query(`SELECT a.ip FROM device_addresses a JOIN devices d ON d.id = a.device_id
		WHERE d.mac = ? AND a.current = 1 ORDER BY a.ip`, mac)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var ip string
		rows.Scan(&ip)
		out = append(out, ip)
	}
	return out
}

func TestMovedDeviceLeavesOldSubnet(t *testing.T) {
	h := it.New(t)
	const dune = "00:00:5e:00:53:43"
	at := func(m int) time.Time { return it.T0.Add(time.Duration(m) * time.Minute) }
	nas := func(m int, obs ...contract.Observation) {
		h.Ingest(it.Run{Collector: "nas", Start: at(m), Subnets: []contract.SubnetScan{it.Scan(home)}, Obs: obs})
	}
	desktop := func(m int, obs ...contract.Observation) {
		h.Ingest(it.Run{Collector: "desktop", Start: at(m), Subnets: []contract.SubnetScan{it.Scan(lan)}, Obs: obs})
	}
	tv := it.ARP("192.168.1.60", "00:00:5e:00:53:60", "")

	nas(0, it.ARP("192.168.1.43", dune, "DUNE"), tv)
	desktop(15, it.ARP("192.168.0.28", dune, "DUNE"))
	if got := currentIPs(t, h, dune); !slices.Equal(got, []string{"192.168.0.28", "192.168.1.43"}) {
		t.Fatalf("before the next scan of the old subnet: %v", got)
	}

	// The next complete scan of 192.168.1.0/24 misses the Dune (seen on 192.168.0.0/24 since) and
	// the TV (switched off: seen nowhere since).
	nas(30)
	if got := currentIPs(t, h, dune); !slices.Equal(got, []string{"192.168.0.28"}) {
		t.Errorf("moved device addresses = %v, want only 192.168.0.28", got)
	}
	if s := status(t, h, dune); s == "offline" {
		t.Error("the moved device went offline")
	}
	if got := currentIPs(t, h, tv.MAC); !slices.Equal(got, []string{"192.168.1.60"}) {
		t.Errorf("switched-off device addresses = %v, want its last address kept", got)
	}
	if s := status(t, h, tv.MAC); s != "offline" {
		t.Errorf("switched-off device status = %s, want offline", s)
	}

	// Back on the old subnet: the address is current again.
	nas(45, it.ARP("192.168.1.43", dune, "DUNE"))
	if got := currentIPs(t, h, dune); !slices.Equal(got, []string{"192.168.0.28", "192.168.1.43"}) {
		t.Errorf("after it came back: %v", got)
	}

	before := h.Snapshot()
	if err := inventory.Rebuild(context.Background(), h.Store, h.Applier); err != nil {
		t.Fatal(err)
	}
	if after := h.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Errorf("rebuild changed projections:\nbefore %v\nafter  %v", before, after)
	}
}

// Seen on the other subnet only before its last sighting on this one: the address stays.
func TestOlderSightingElsewhereKeepsAddress(t *testing.T) {
	h := it.New(t)
	const dev = "00:00:5e:00:53:44"
	h.Ingest(it.Run{Collector: "desktop", Start: it.T0, Subnets: []contract.SubnetScan{it.Scan(lan)},
		Obs: []contract.Observation{it.ARP("192.168.0.44", dev, "")}})
	h.Ingest(it.Run{Collector: "nas", Start: it.T0.Add(15 * time.Minute), Subnets: []contract.SubnetScan{it.Scan(home)},
		Obs: []contract.Observation{it.ARP("192.168.1.44", dev, "")}})
	h.Ingest(it.Run{Collector: "nas", Start: it.T0.Add(30 * time.Minute), Subnets: []contract.SubnetScan{it.Scan(home)}})
	if got := currentIPs(t, h, dev); !slices.Contains(got, "192.168.1.44") {
		t.Errorf("addresses = %v; 192.168.1.44 is the latest sighting and must stay", got)
	}
}
