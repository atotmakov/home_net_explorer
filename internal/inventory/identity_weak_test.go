package inventory_test

import (
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	it "github.com/atotmakov/home_net_explorer/internal/inventory/inventorytest"
)

func routed(ip, host, method string) contract.Observation {
	o := contract.Observation{IP: ip, Hostname: host, Method: method}
	if host != "" {
		o.HostnameSource = contract.HostnameSourceDNS
	}
	return o
}

func icmpScan(cidr string) contract.SubnetScan {
	return contract.SubnetScan{CIDR: cidr, Method: contract.MethodICMPTCP, Complete: true, HostsProbed: 254}
}

func TestWeakIdentityKeys(t *testing.T) {
	h := it.New(t)
	h.Ingest(it.Run{Collector: "desktop", Subnets: []contract.SubnetScan{icmpScan("192.168.50.0/24")},
		Obs: []contract.Observation{routed("192.168.50.7", "camera.lan", contract.ObsTCP), routed("192.168.50.1", "", contract.ObsICMP)}})
	ds := devices(t, h)
	if len(ds) != 2 {
		t.Fatalf("devices = %+v", ds)
	}
	keys := map[string]string{ds[0].IdentityKey: ds[0].Strength, ds[1].IdentityKey: ds[1].Strength}
	for _, k := range []string{"host:192.168.50.0/24:camera.lan", "ip:192.168.50.0/24:192.168.50.1"} {
		if keys[k] != "weak" {
			t.Errorf("identity %s missing or not weak: %v", k, keys)
		}
	}
}

func TestWeakDeviceFoldsIntoMAC(t *testing.T) {
	h := it.New(t)
	sub := "192.168.50.0/24"
	h.Ingest(it.Run{Collector: "desktop", Subnets: []contract.SubnetScan{icmpScan(sub)},
		Obs: []contract.Observation{routed("192.168.50.7", "camera.lan", contract.ObsTCP)}})
	// Later the subnet becomes on-link and the camera answers ARP.
	h.Ingest(it.Run{Collector: "desktop", Start: it.T0.Add(15 * time.Minute), Subnets: []contract.SubnetScan{it.Scan(sub)},
		Obs: []contract.Observation{it.ARP("192.168.50.7", "3c:22:fb:44:55:66", "camera.lan")}})

	var status, mergedInto string
	h.Store.DB().QueryRow(`SELECT status, COALESCE((SELECT identity_key FROM devices t WHERE t.id = d.merged_into), '')
		FROM devices d WHERE identity_key = 'host:192.168.50.0/24:camera.lan'`).Scan(&status, &mergedInto)
	if status != "merged_away" || mergedInto != "mac:3c:22:fb:44:55:66" {
		t.Errorf("weak device status=%s merged_into=%s, want merged_away into the MAC device", status, mergedInto)
	}
	if n := scalar(t, h, `SELECT count(*) FROM sightings s JOIN devices d ON d.id = s.device_id WHERE d.identity_key = 'mac:3c:22:fb:44:55:66'`); n != 2 {
		t.Errorf("MAC device has %d sightings, want both histories (2)", n)
	}
	var first string
	h.Store.DB().QueryRow(`SELECT first_seen FROM devices WHERE identity_key = 'mac:3c:22:fb:44:55:66'`).Scan(&first)
	if first != "2026-10-05T10:00:00.000Z" {
		t.Errorf("first_seen = %s, want the weak device's earlier first sighting", first)
	}
	if n := scalar(t, h, `SELECT count(*) FROM events WHERE type = 'merged'`); n != 1 {
		t.Errorf("merged events = %d, want 1", n)
	}
}

func TestSameMACFromTwoCollectorsIsOneDevice(t *testing.T) {
	h := it.New(t)
	scan := []contract.SubnetScan{it.Scan("192.168.1.0/24")}
	nas := it.ARP("192.168.1.20", "00:11:32:aa:bb:cc", "nas.lan")
	h.Ingest(it.Run{Collector: "nas", Subnets: scan, Obs: []contract.Observation{nas}})
	h.Ingest(it.Run{Collector: "desktop", Start: it.T0.Add(2 * time.Minute), Subnets: scan, Obs: []contract.Observation{nas}})
	if n := scalar(t, h, `SELECT count(*) FROM devices WHERE status != 'merged_away'`); n != 1 {
		t.Errorf("devices = %d, want exactly one (SC-006)", n)
	}
	if n := scalar(t, h, `SELECT count(DISTINCT collector_id) FROM sightings`); n != 2 {
		t.Errorf("sightings from %d collectors, want 2", n)
	}
}

func TestMultiHomedDevice(t *testing.T) {
	h := it.New(t)
	h.Ingest(it.Run{Collector: "desktop", Subnets: []contract.SubnetScan{it.Scan("192.168.1.0/24"), it.Scan("192.168.8.0/24")},
		Obs: []contract.Observation{it.ARP("192.168.1.10", "10:7b:44:01:02:03", ""), it.ARP("192.168.8.176", "10:7b:44:01:02:03", "")}})
	if n := scalar(t, h, `SELECT count(*) FROM devices`); n != 1 {
		t.Errorf("devices = %d, want 1", n)
	}
	if n := scalar(t, h, `SELECT count(*) FROM device_addresses WHERE current = 1`); n != 2 {
		t.Errorf("current addresses = %d, want 2 (one per subnet)", n)
	}
}

func TestRoutedSightingOfKnownIPUsesStrongDevice(t *testing.T) {
	h := it.New(t)
	sub := "192.168.50.0/24"
	h.Ingest(it.Run{Collector: "nas", Subnets: []contract.SubnetScan{it.Scan(sub)},
		Obs: []contract.Observation{it.ARP("192.168.50.7", "3c:22:fb:44:55:66", "")}})
	h.Ingest(it.Run{Collector: "desktop", Start: it.T0.Add(time.Minute), Subnets: []contract.SubnetScan{icmpScan(sub)},
		Obs: []contract.Observation{routed("192.168.50.7", "", contract.ObsICMP)}})
	if n := scalar(t, h, `SELECT count(*) FROM devices`); n != 1 {
		t.Errorf("devices = %d: a MAC-less sighting of an address held by a known device must not create a weak duplicate", n)
	}
}
