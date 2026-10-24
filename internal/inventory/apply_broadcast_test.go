package inventory_test

import (
	"testing"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	it "github.com/atotmakov/home_net_explorer/internal/inventory/inventorytest"
)

// Older collectors reported the broadcast address from the Windows ARP cache
// (192.168.1.255, ff:ff:ff:ff:ff:ff). The server never folds a subnet's network or broadcast
// address, or a group MAC, into a device.
func TestBroadcastIsNotADevice(t *testing.T) {
	h := it.New(t)
	neigh := func(ip, mac string) contract.Observation {
		return contract.Observation{IP: ip, MAC: mac, Method: contract.ObsNeighborCache}
	}
	h.Ingest(it.Run{Subnets: []contract.SubnetScan{it.Scan("192.168.1.0/24"), it.Scan("192.168.8.0/24")}, Obs: []contract.Observation{
		it.ARP("192.168.1.20", "00:11:32:aa:bb:cc", "nas.lan"),
		neigh("192.168.1.255", "ff:ff:ff:ff:ff:ff"),
		neigh("192.168.8.255", "ff:ff:ff:ff:ff:ff"),
		neigh("192.168.1.0", "3c:22:fb:44:55:00"),
		neigh("192.168.1.251", "01:00:5e:00:00:fb"),
	}})
	var n int
	if err := h.Store.DB().QueryRow(`SELECT count(*) FROM devices`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("devices = %d, want only the NAS", n)
	}
}
