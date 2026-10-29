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

// Feature 015: a port run stores its checks and open ports and derives the device's ports; it
// is not a presence scan (research R7, R9, R10).

const camMAC = "00:00:5e:00:53:79"

func openPorts(ports ...int) []contract.OpenPort {
	out := []contract.OpenPort{}
	for _, p := range ports {
		out = append(out, contract.OpenPort{Port: p, Proto: "tcp", Service: map[int]string{80: "http", 554: "rtsp", 22: "ssh"}[p],
			ServiceSource: "table"})
	}
	return out
}

func count(t *testing.T, h *it.Harness, q string, args ...any) int {
	t.Helper()
	var n int
	if err := h.Store.DB().QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestApplyPortRun(t *testing.T) {
	h := it.New(t)
	h.Ingest(it.Run{Subnets: []contract.SubnetScan{it.Scan(home)}, Obs: []contract.Observation{
		it.ARP("192.168.1.79", camMAC, "cam"), it.ARP("192.168.1.80", "00:00:5e:00:53:80", "")}})
	events := count(t, h, `SELECT count(*) FROM events`)

	h.Ingest(it.Run{Start: it.T0.Add(10 * time.Minute), PortChecks: []contract.PortCheck{
		{IP: "192.168.1.79", MAC: camMAC, Complete: true, Open: openPorts(80, 554)},
		{IP: "192.168.1.200", MAC: "00:00:5e:00:53:c8", Complete: true, Open: openPorts(22)}, // no such device
	}})

	if n := count(t, h, `SELECT count(*) FROM port_checks`); n != 1 {
		t.Errorf("port_checks = %d, want 1 (the check of an unknown device is dropped)", n)
	}
	if n := count(t, h, `SELECT count(*) FROM port_checks c JOIN devices d ON d.id = c.device_id
		WHERE d.mac = ? AND c.complete = 1 AND c.ip = '192.168.1.79'`, camMAC); n != 1 {
		t.Error("the check is not the camera's complete check")
	}
	if n := count(t, h, `SELECT count(*) FROM port_findings`); n != 2 {
		t.Errorf("port_findings = %d, want 2", n)
	}
	rows, err := h.Store.DB().Query(`SELECT p.port, p.state, p.service, p.ip, p.first_open, p.last_open FROM device_ports p
		JOIN devices d ON d.id = p.device_id WHERE d.mac = ? ORDER BY p.port`, camMAC)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for rows.Next() {
		var port int
		var state, service, ip, first, last string
		rows.Scan(&port, &state, &service, &ip, &first, &last)
		if first != last || first == "" {
			t.Errorf("port %d: first_open %q, last_open %q; want both the check's finish", port, first, last)
		}
		got = append(got, state+" "+service+" "+ip)
	}
	rows.Close()
	if want := []string{"open http 192.168.1.79", "open rtsp 192.168.1.79"}; !reflect.DeepEqual(got, want) {
		t.Errorf("device ports = %v, want %v", got, want)
	}

	// Not a presence scan: the other device (absent from the port run) is not offline, and no
	// presence event was added.
	if s := status(t, h, "00:00:5e:00:53:80"); s == "offline" {
		t.Error("a port run marked a device offline")
	}
	if n := count(t, h, `SELECT count(*) FROM events WHERE type NOT IN ('port_opened', 'port_closed')`); n != events {
		t.Errorf("presence events %d → %d after a port run", events, n)
	}

	before := h.Snapshot()
	if err := inventory.Rebuild(context.Background(), h.Store, h.Applier); err != nil {
		t.Fatal(err)
	}
	if after := h.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Errorf("rebuild changed projections:\nbefore %v\nafter  %v", before, after)
	}
}
