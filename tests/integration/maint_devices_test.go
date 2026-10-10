package integration_test

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/inventory"
)

// Feature 004, US1: remove all devices, with their history and the owner's edits.

func TestMaintRemoveAllDevices(t *testing.T) {
	var logs bytes.Buffer
	e := newEnv(t, envOpts{log: &logs})
	e.login()
	_, token := configuredRouter(t, e, "pw-maint") // uploads valid_router.json as "desktop"
	var dev int64
	e.app.Store.DB().QueryRow(`SELECT id FROM devices WHERE mac = '00:00:5e:10:00:02'`).Scan(&dev)
	if res, _ := e.post(fmt.Sprintf("/devices/%d/attrs", dev), url.Values{"name": {"Old name"}}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("name device = %d", res.StatusCode)
	}
	if err := e.app.Ingester.Do(context.Background(), func(tx *sql.Tx) error {
		return inventory.SetSubnetAttr(context.Background(), tx, "192.168.8.0/24", "ignored", "true", t0)
	}); err != nil {
		t.Fatal(err)
	}
	if code, _ := e.upload(token, shifted(t, "valid_router.json", "5a7c1e2d-3b4f-4a6e-9d8c-000000000001", time.Hour)); code != http.StatusCreated {
		t.Fatalf("second upload = %d", code)
	}
	devices := e.count(`SELECT count(*) FROM devices WHERE status != 'merged_away'`)
	if devices == 0 || e.runCount() != 2 {
		t.Fatalf("setup: %d devices, %d runs", devices, e.runCount())
	}
	removeAt := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC) // after the fixtures' scans
	e.clock.Set(removeAt)

	// Refused without the right word, the owner's session, or from another site.
	for _, form := range []url.Values{{}, {"confirm": {"device"}}, {"confirm": {"everything"}}} {
		if res, body := e.post("/maintenance/devices", form); res.StatusCode != http.StatusBadRequest || !strings.Contains(body, "Type devices to confirm") {
			t.Errorf("confirm=%q: status %d", form.Get("confirm"), res.StatusCode)
		}
	}
	e.anonPost("/maintenance/devices", url.Values{"confirm": {"devices"}})
	if code := e.crossSitePost("/maintenance/devices", url.Values{"confirm": {"devices"}}); code != http.StatusForbidden {
		t.Errorf("cross-site = %d, want 403", code)
	}
	if e.runCount() != 2 || e.count(`SELECT count(*) FROM devices`) == 0 {
		t.Fatal("a refused request removed something")
	}

	res, _ := e.post("/maintenance/devices", url.Values{"confirm": {" Devices "}})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("remove = %d", res.StatusCode)
	}
	want := fmt.Sprintf("/settings?done=devices&devices=%d&runs=2", devices)
	if loc := res.Header.Get("Location"); loc != want {
		t.Errorf("Location = %q, want %q", loc, want)
	}
	if n := e.count(`SELECT count(*) FROM devices`); n != 0 {
		t.Errorf("%d devices left", n)
	}
	if body := e.get("/devices"); strings.Contains(body, "00:00:5e:10:00:02") {
		t.Error("the Devices page still lists a removed device")
	}
	if body := e.get("/collectors"); !strings.Contains(body, "desktop") {
		t.Error("removing devices removed a collector")
	}
	if !strings.Contains(logs.String(), "maintenance") || !strings.Contains(logs.String(), "action=devices") {
		t.Error("no maintenance log line")
	}

	// ping: no routers any more, the ignore choice is kept.
	var ping contract.PingResponse
	e.api(token, "/api/v1/ping", &ping)
	if len(ping.Routers) != 0 {
		t.Errorf("routers after removal = %+v", ping.Routers)
	}
	if len(ping.IgnoredSubnets) != 1 || ping.IgnoredSubnets[0] != "192.168.8.0/24" {
		t.Errorf("ignored subnets after removal = %v", ping.IgnoredSubnets)
	}

	// A run that started before the removal (e.g. spooled) is acknowledged but discarded.
	code, r := e.upload(token, withID(t, "valid_router.json", "5a7c1e2d-3b4f-4a6e-9d8c-000000000002", nil))
	if code != http.StatusOK || r.Status != contract.StatusDiscarded {
		t.Errorf("pre-removal run = %d %s, want 200 discarded", code, r.Status)
	}
	if e.runCount() != 0 || e.count(`SELECT count(*) FROM devices`) != 0 {
		t.Error("a pre-removal run brought data back")
	}

	// A new scan brings the devices back, without the old name.
	code, r = e.upload(token, shifted(t, "valid_router.json", "5a7c1e2d-3b4f-4a6e-9d8c-000000000003", 27*time.Hour))
	if code != http.StatusCreated || r.Status != contract.StatusStored {
		t.Fatalf("new run = %d %s", code, r.Status)
	}
	var name string
	if err := e.app.Store.DB().QueryRow(`SELECT user_name FROM devices WHERE mac = '00:00:5e:10:00:02'`).Scan(&name); err != nil {
		t.Fatalf("device not back: %v", err)
	}
	if name != "" {
		t.Errorf("device came back with the old name %q", name)
	}
}
