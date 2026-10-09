package inventory_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/inventory"
	it "github.com/atotmakov/home_net_explorer/internal/inventory/inventorytest"
)

// Feature 002: router_table observations identify devices by MAC exactly like ARP.

const ispLAN = "192.168.0.0/24"

func routerScan(complete bool) contract.SubnetScan {
	s := contract.SubnetScan{CIDR: ispLAN, Method: contract.MethodRouterTable, Complete: complete}
	if complete {
		s.HostsProbed = 29
	}
	return s
}

func listed(ip, mac, host, via string) contract.Observation {
	o := contract.Observation{IP: ip, MAC: mac, Hostname: host, Via: via, Method: contract.ObsRouterTable}
	if host != "" {
		o.HostnameSource = contract.HostnameSourceRouter
	}
	return o
}

func routerAt(h *it.Harness, m int, scan contract.SubnetScan, obs ...contract.Observation) {
	h.Ingest(it.Run{Collector: "desktop", Start: minutes(m), Subnets: []contract.SubnetScan{scan}, Obs: obs})
}

// US1 AS-2, SC-002: an IP-only device from earlier ICMP/TCP scans folds into the MAC device.
func TestRouterObservationFoldsWeakDevice(t *testing.T) {
	h := it.New(t)
	h.Ingest(it.Run{Collector: "desktop", Subnets: []contract.SubnetScan{icmpScan(ispLAN)},
		Obs: []contract.Observation{routed("192.168.0.4", "", contract.ObsICMP)}})
	routerAt(h, 15, routerScan(true), listed("192.168.0.4", "00:11:32:aa:bb:01", "host-01", "LAN1"))

	var status, into string
	h.Store.DB().QueryRow(`SELECT status, COALESCE((SELECT identity_key FROM devices t WHERE t.id = d.merged_into), '')
		FROM devices d WHERE identity_key = 'ip:192.168.0.0/24:192.168.0.4'`).Scan(&status, &into)
	if status != "merged_away" || into != "mac:00:11:32:aa:bb:01" {
		t.Errorf("weak device status=%s merged_into=%s, want merged_away into the MAC device", status, into)
	}
	if n := scalar(t, h, `SELECT count(*) FROM devices WHERE status != 'merged_away'`); n != 1 {
		t.Errorf("devices left = %d, want 1 (no duplicate)", n)
	}
	for _, d := range devices(t, h) {
		if d.IdentityKey == "mac:00:11:32:aa:bb:01" {
			if d.Strength != "strong" || d.Hostname != "host-01" || !strings.Contains(d.Manufacturer, "Synology") {
				t.Errorf("router-identified device = %+v", d)
			}
			if d.FirstSeen != "2026-10-05T10:00:00.000Z" {
				t.Errorf("first_seen = %s, want the weak device's earlier sighting", d.FirstSeen)
			}
		}
	}
}

func TestRouterRandomizedAndDuplicateHostnames(t *testing.T) {
	h := it.New(t)
	routerAt(h, 0, routerScan(true),
		listed("192.168.0.7", "02:00:5e:10:00:05", "BEAR", "SSID2"),
		listed("192.168.0.8", "00:00:5e:10:00:06", "BEAR", "LAN2"))
	ds := devices(t, h)
	if len(ds) != 2 {
		t.Fatalf("two MACs with the same hostname must stay two devices: %+v", ds)
	}
	for _, d := range ds {
		if d.MAC == "02:00:5e:10:00:05" && (!d.Randomized || d.Manufacturer != "") {
			t.Errorf("locally administered MAC = %+v, want randomized without manufacturer", d)
		}
	}
}

func viaRows(t *testing.T, h *it.Harness) []string {
	t.Helper()
	rows, err := h.Store.DB().Query(`SELECT via || ':' || seen_count FROM sightings ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

// via is part of the sighting tuple; an unchanged report extends one sighting.
func TestRouterViaInSightingTuple(t *testing.T) {
	h := it.New(t)
	tv := func(via string) contract.Observation { return listed("192.168.0.4", "00:00:5e:10:00:02", "tv", via) }
	routerAt(h, 0, routerScan(true), tv("LAN1"))
	routerAt(h, 15, routerScan(true), tv("LAN1"))
	routerAt(h, 30, routerScan(true), tv("LAN1"))
	if got := viaRows(t, h); !reflect.DeepEqual(got, []string{"LAN1:3"}) {
		t.Errorf("sightings = %v, want one LAN1 sighting seen 3 times", got)
	}
	routerAt(h, 45, routerScan(true), tv("LAN2"))
	if got := viaRows(t, h); !reflect.DeepEqual(got, []string{"LAN1:3", "LAN2:1"}) {
		t.Errorf("sightings = %v, want a new sighting after the move to LAN2", got)
	}

	before := h.Snapshot()
	if err := inventory.Rebuild(context.Background(), h.Store, h.Applier); err != nil {
		t.Fatal(err)
	}
	if after := h.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Errorf("rebuild changed projections (via must come from the stored runs):\nbefore %v\nafter  %v", before, after)
	}
}

// US1 AS-5, SC-003: complete router reads drive offline detection like ARP scans; failed reads
// (complete = false) never do (FR-010).
func TestRouterOfflineDetection(t *testing.T) {
	phone := listed("192.168.0.50", "da:a1:19:00:00:50", "phone", "SSID1")
	tv := listed("192.168.0.4", "00:00:5e:10:00:02", "tv", "LAN1")

	h := it.New(t)
	routerAt(h, 0, routerScan(true), phone, tv)
	routerAt(h, 15, routerScan(false))
	routerAt(h, 30, routerScan(false))
	routerAt(h, 45, routerScan(false))
	routerAt(h, 60, routerScan(false))
	if s := status(t, h, phone.MAC); s != "online" {
		t.Fatalf("failed router reads marked the device %s", s)
	}

	h = it.New(t)
	routerAt(h, 0, routerScan(true), phone, tv)
	routerAt(h, 15, routerScan(true), tv)
	routerAt(h, 30, routerScan(true), tv)
	if s := status(t, h, phone.MAC); s != "online" {
		t.Fatalf("after 2 router reads without it: %s, want online", s)
	}
	routerAt(h, 45, routerScan(true), tv)
	if s := status(t, h, phone.MAC); s != "offline" {
		t.Errorf("after 3 complete router reads without it: %s, want offline (same as ARP)", s)
	}
	if s := status(t, h, tv.MAC); s != "online" {
		t.Errorf("listed device = %s", s)
	}
}
