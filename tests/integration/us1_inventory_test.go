package integration_test

import (
	"context"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/inventory"
	it "github.com/atotmakov/home_net_explorer/internal/inventory/inventorytest"
)

// User Story 1: the NAS scans its own subnet and shows the inventory (quickstart.md §3).
func TestUS1ScanAndInventory(t *testing.T) {
	e := newEnv(t, envOpts{builtinScan: true})
	e.login()

	// The loop scans once at start-up; wait for it, then scan on demand.
	e.waitFor("initial scan", func() bool { return e.runCount() == 1 })
	e.waitFor("scanner idle", func() bool { return !e.app.Scanner.Status().Running })
	res, _ := e.req("POST", "/scan", url.Values{})
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /scan = %d, want 202", res.StatusCode)
	}
	e.waitFor("on-demand scan", func() bool { return e.runCount() == 2 })

	body := e.get("/devices")
	for _, want := range []string{"192.168.1.100", "a0:b1:c2:d3:e4:f5", "router.lan", "nas.lan", "Synology", "192.168.1.50"} {
		if !strings.Contains(body, want) {
			t.Errorf("/devices is missing %q", want)
		}
	}
	if body := e.get("/devices?q=router"); strings.Contains(body, "nas.lan") || !strings.Contains(body, "router.lan") {
		t.Error("filter q=router did not filter")
	}
	if body := e.get("/devices?sort=ip&dir=desc"); strings.Index(body, "192.168.1.100") > strings.Index(body, "192.168.1.20") {
		t.Error("ip sort desc not applied")
	}

	// Friendly name, notes and type survive later scans.
	id := deviceID(t, e, "router.lan")
	res, _ = e.req("POST", "/devices/"+id+"/attrs", url.Values{"name": {"Main router"}, "notes": {"ISP box"}, "type": {"router"}})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("attrs = %d", res.StatusCode)
	}
	e.req("POST", "/scan", url.Values{})
	e.waitFor("third scan", func() bool { return e.runCount() == 3 })
	e.waitFor("scanner idle", func() bool { return !e.app.Scanner.Status().Running })
	if body := e.get("/devices/" + id); !strings.Contains(body, "Main router") || !strings.Contains(body, "ISP box") {
		t.Error("user attributes lost after a scan")
	}
	if res, _ := e.req("POST", "/devices/"+id+"/attrs", url.Values{"type": {"toaster"}}); res.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown type = %d, want 400", res.StatusCode)
	}

	// Scheduled scans fire on the interval (fake clock).
	e.waitFor("loop waiting", func() bool { return e.clock.Waiters() > 0 })
	e.clock.Advance(15 * time.Minute)
	e.waitFor("scheduled scan", func() bool { return e.runCount() == 4 })

	// Rebuild invariant over everything above.
	e.waitFor("scanner idle", func() bool { return !e.app.Scanner.Status().Running })
	before := snapshot(t, e)
	if err := inventory.Rebuild(context.Background(), e.app.Store, e.app.Applier); err != nil {
		t.Fatal(err)
	}
	if after := snapshot(t, e); !reflect.DeepEqual(before, after) {
		t.Error("rebuild changed projections")
	}
}

func TestUS1HomeAndSettings(t *testing.T) {
	e := newEnv(t, envOpts{builtinScan: true})
	e.login()
	e.waitFor("initial scan", func() bool { return e.runCount() == 1 })
	e.waitFor("scanner idle", func() bool { return !e.app.Scanner.Status().Running })

	home := e.get("/")
	if !strings.Contains(home, "192.168.1.0/24") || !strings.Contains(home, "Scan now") {
		t.Error("home page lacks subnet status or the Scan now button")
	}
	settings := e.get("/settings")
	if !strings.Contains(settings, "192.168.1.0/24") {
		t.Error("settings page does not list the discovered subnet")
	}
	res, _ := e.req("POST", "/settings", url.Values{"interval_minutes": {"30"}, "offline_multiplier": {"4"}})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /settings = %d", res.StatusCode)
	}
	if v, _, _ := e.app.Store.Setting(context.Background(), "builtin_interval_seconds"); v != "1800" {
		t.Errorf("interval setting = %q", v)
	}
	if res, _ := e.req("POST", "/settings", url.Values{"interval_minutes": {"0"}}); res.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid interval = %d, want 400", res.StatusCode)
	}
}

func TestScanWithoutBuiltinCollector(t *testing.T) {
	e := newEnv(t, envOpts{builtinScan: false})
	e.login()
	if res, _ := e.req("POST", "/scan", url.Values{}); res.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("POST /scan with the built-in collector disabled = %d, want 503", res.StatusCode)
	}
}

func deviceID(t *testing.T, e *env, hostname string) string {
	t.Helper()
	var id string
	if err := e.app.Store.DB().QueryRow(`SELECT CAST(id AS TEXT) FROM devices WHERE hostname = ?`, hostname).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func snapshot(t *testing.T, e *env) map[string][]string {
	t.Helper()
	return it.SnapshotStore(t, e.app.Store)
}
