package integration_test

import (
	"bytes"
	"context"
	"database/sql"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/inventory"
)

// Feature 004, US4: drop all data, keeping the owner and their login.

func TestMaintDropAllData(t *testing.T) {
	var logs bytes.Buffer
	e := newEnv(t, envOpts{builtinScan: true, log: &logs})
	e.login()
	e.waitFor("the start-up scan", func() bool { return e.runCount() == 1 })
	_, token := configuredRouter(t, e, "pw-maint")
	if err := e.app.Ingester.Do(context.Background(), func(tx *sql.Tx) error {
		ctx := context.Background()
		if err := inventory.SetSubnetAttr(ctx, tx, "192.168.1.0/24", "name", "home", t0); err != nil {
			return err
		}
		return inventory.SetSubnetAttr(ctx, tx, "192.168.0.0/24", "ignored", "true", t0)
	}); err != nil {
		t.Fatal(err)
	}
	if res, _ := e.post("/settings", url.Values{"interval_minutes": {"30"}, "offline_multiplier": {"5"}}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("save settings = %d", res.StatusCode)
	}
	if res, _ := e.post("/maintenance/pause", url.Values{}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("pause = %d", res.StatusCode)
	}
	dropAt := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	e.clock.Set(dropAt)

	for _, form := range []url.Values{{}, {"confirm": {"all"}}, {"confirm": {"devices"}}} {
		if res, _ := e.post("/maintenance/everything", form); res.StatusCode != http.StatusBadRequest {
			t.Errorf("confirm=%q: status %d, want 400", form.Get("confirm"), res.StatusCode)
		}
	}
	e.anonPost("/maintenance/everything", url.Values{"confirm": {"everything"}})
	if code := e.crossSitePost("/maintenance/everything", url.Values{"confirm": {"everything"}}); code != http.StatusForbidden {
		t.Errorf("cross-site = %d, want 403", code)
	}
	if e.runCount() == 0 {
		t.Fatal("a refused request dropped the data")
	}

	res, _ := e.post("/maintenance/everything", url.Values{"confirm": {"everything"}})
	if res.StatusCode != http.StatusSeeOther || !strings.HasPrefix(res.Header.Get("Location"), "/settings?done=everything&devices=") ||
		!strings.Contains(res.Header.Get("Location"), "&collectors=1") {
		t.Fatalf("drop = %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	if !strings.Contains(logs.String(), "action=everything") {
		t.Error("no maintenance log line")
	}

	// Still logged in, with the same password.
	settings := e.get("/settings") // e.get fails on a login redirect
	if !strings.Contains(settings, `value="15"`) || !strings.Contains(settings, `name="offline_multiplier" min="1" max="10" value="3"`) {
		t.Errorf("settings not back to defaults:\n%s", settings)
	}
	if strings.Contains(settings, "paused since") || strings.Contains(settings, `value="home"`) {
		t.Error("the pause or a subnet name survived") // subnets themselves may already be rediscovered
	}
	if n := e.count(`SELECT count(*) FROM collectors`); n != 1 {
		t.Errorf("%d collectors left, want only nas", n)
	}
	for _, table := range []string{"devices", "collection_runs", "user_subnet_attrs", "user_device_attrs", "router_settings"} {
		if n := e.count(`SELECT count(*) FROM ` + table); n != 0 {
			t.Errorf("%s: %d rows left", table, n)
		}
	}
	if code := e.api(token, "/api/v1/ping", nil); code != http.StatusUnauthorized {
		t.Errorf("old token after drop = %d, want 401", code)
	}

	// The scanner runs again: the next built-in scan is stored.
	e.waitFor("a scan after the drop", func() bool { return e.runCount() >= 1 })
	if body := e.get("/"); !strings.Contains(body, "Scan now") && !strings.Contains(body, "Scanning") {
		t.Error("the built-in scanner is not running after the drop")
	}

	// A pre-drop run uploaded by a new collector is discarded.
	cfg := e.createCollector("desktop")
	if code, r := e.upload(cfg.Token, withID(t, "valid_router.json", "5a7c1e2d-3b4f-4a6e-9d8c-000000000021", nil)); code != http.StatusOK || r.Status != contract.StatusDiscarded {
		t.Errorf("pre-drop run = %d %s, want discarded", code, r.Status)
	}

	// Log out and in again with the same password.
	e.post("/logout", url.Values{})
	if res, _ := e.post("/login", url.Values{"password": {password}}); res.StatusCode != http.StatusSeeOther {
		t.Errorf("login after drop = %d", res.StatusCode)
	}
	e.get("/devices")
}
