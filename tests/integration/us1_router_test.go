package integration_test

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/atotmakov/home_net_explorer/internal/contract/contracttest"
)

// Feature 002, US1: router device lists through the HTTP API.

func (e *env) count(q string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.app.Store.DB().QueryRow(q, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func TestUS1RouterSubnetIsDiscovered(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.login()
	cfg := e.createCollector("desktop")
	status, r := e.upload(cfg.Token, contracttest.Fixture(t, "valid_router.json"))
	if status != http.StatusCreated {
		t.Fatalf("upload = %d", status)
	}
	if !slices.Contains(r.NewSubnets, "192.168.0.0/24") {
		t.Errorf("new_subnets = %v, want the router_table subnet (FR-009)", r.NewSubnets)
	}
	if n := e.count(`SELECT count(*) FROM run_subnets WHERE cidr = '192.168.0.0/24' AND method = 'router_table' AND complete = 1`); n != 1 {
		t.Errorf("router_table run_subnets rows = %d", n)
	}
}

func TestUS1RouterDevicesFoldWeakRecords(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.login()
	cfg := e.createCollector("desktop")

	// Earlier ICMP/TCP-only scans of the routed subnet left an IP-only record.
	routed := withID(t, "valid_routed.json", "1b4e28ba-2fa1-41d2-883f-0016d3cca499", func(m map[string]any) {
		m["subnets"].([]any)[0].(map[string]any)["cidr"] = "192.168.0.0/24"
		obs := m["observations"].([]any)
		obs[0].(map[string]any)["ip"] = "192.168.0.4"
		m["observations"] = obs[:1]
	})
	if status, _ := e.upload(cfg.Token, routed); status != http.StatusCreated {
		t.Fatalf("routed upload = %d", status)
	}
	if status, _ := e.upload(cfg.Token, contracttest.Fixture(t, "valid_router.json")); status != http.StatusCreated {
		t.Fatalf("router upload = %d", status)
	}

	if n := e.count(`SELECT count(*) FROM devices d JOIN device_addresses a ON a.device_id = d.id AND a.current = 1
		WHERE a.ip = '192.168.0.4' AND d.status != 'merged_away'`); n != 1 {
		t.Errorf("devices holding 192.168.0.4 = %d, want 1 (the IP-only record is merged, SC-002)", n)
	}
	if n := e.count(`SELECT count(*) FROM devices WHERE identity_key = 'mac:00:00:5e:10:00:02' AND status != 'merged_away'`); n != 1 {
		t.Error("the router-listed device is not identified by its MAC")
	}
	body := e.get("/devices")
	for _, want := range []string{"host-01", "192.168.0.4", "192.168.0.7", "192.168.0.9"} {
		if !strings.Contains(body, want) {
			t.Errorf("/devices lacks %s", want)
		}
	}

	var id int64
	e.app.Store.DB().QueryRow(`SELECT id FROM devices WHERE identity_key = 'mac:00:00:5e:10:00:02'`).Scan(&id)
	if page := e.get(fmt.Sprintf("/devices/%d", id)); !strings.Contains(page, "LAN1") {
		t.Error("the device page does not show how the router says it is connected (via LAN1)")
	}

	// Failed router reads (complete = false) never mark anything offline (FR-010).
	for i, at := range []string{"10:30", "10:45", "11:00", "11:15"} {
		failed := withID(t, "valid_router_failed.json", fmt.Sprintf("6b8d2f3e-4c5a-4b7f-8e9d-2a3f4e5d6c%02d", i), func(m map[string]any) {
			m["started_at"] = "2026-10-09T" + at + ":00.000Z"
			m["finished_at"] = "2026-10-09T" + at + ":30.000Z"
			m["sent_at"] = "2026-10-09T" + at + ":31.000Z"
			m["observations"].([]any)[0].(map[string]any)["observed_at"] = "2026-10-09T" + at + ":05.000Z"
		})
		if status, _ := e.upload(cfg.Token, failed); status != http.StatusCreated {
			t.Fatalf("failed-read upload %d = %d", i, status)
		}
	}
	if n := e.count(`SELECT count(*) FROM devices d JOIN device_addresses a ON a.device_id = d.id AND a.current = 1
		JOIN subnets s ON s.id = a.subnet_id WHERE s.cidr = '192.168.0.0/24' AND d.status = 'offline'`); n != 0 {
		t.Errorf("%d router devices went offline after failed reads", n)
	}
}
