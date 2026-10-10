package integration_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// Feature 004, US3: remove all remote collectors, keeping what they found.

func TestMaintRemoveAllCollectors(t *testing.T) {
	var logs bytes.Buffer
	e := newEnv(t, envOpts{log: &logs})
	e.login()
	routerID, desktop := configuredRouter(t, e, "pw-maint") // "desktop" uploaded valid_router.json
	laptop := e.createCollector("laptop")
	if code, _ := e.upload(laptop.Token, withID(t, "valid_full.json", "5a7c1e2d-3b4f-4a6e-9d8c-000000000011", nil)); code != http.StatusCreated {
		t.Fatalf("laptop upload = %d", code)
	}
	devicesBefore := e.get("/devices")
	runs := e.runCount()

	for _, form := range []url.Values{{}, {"confirm": {"collector"}}} {
		if res, _ := e.post("/maintenance/collectors", form); res.StatusCode != http.StatusBadRequest {
			t.Errorf("confirm=%q: status %d, want 400", form.Get("confirm"), res.StatusCode)
		}
	}
	e.anonPost("/maintenance/collectors", url.Values{"confirm": {"collectors"}})
	if code := e.crossSitePost("/maintenance/collectors", url.Values{"confirm": {"collectors"}}); code != http.StatusForbidden {
		t.Errorf("cross-site = %d, want 403", code)
	}
	if code := e.api(desktop, "/api/v1/ping", nil); code != http.StatusOK {
		t.Fatal("a refused request removed the collectors")
	}

	res, _ := e.post("/maintenance/collectors", url.Values{"confirm": {"collectors"}})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/settings?done=collectors&collectors=2" {
		t.Fatalf("remove = %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	page := e.get("/collectors")
	if strings.Contains(page, ">desktop<") || strings.Contains(page, ">laptop<") || !strings.Contains(page, ">nas<") {
		t.Errorf("Collectors page after removal:\n%s", page)
	}
	for _, tok := range []string{desktop, laptop.Token} {
		if code, _ := e.upload(tok, withID(t, "valid_minimal.json", "5a7c1e2d-3b4f-4a6e-9d8c-000000000012", nil)); code != http.StatusUnauthorized {
			t.Errorf("upload with a removed token = %d, want 401", code)
		}
		if code := e.api(tok, "/api/v1/ping", nil); code != http.StatusUnauthorized {
			t.Errorf("ping with a removed token = %d, want 401", code)
		}
		if code := e.api(tok, fmt.Sprintf("/api/v1/routers/%d/login", routerID), nil); code != http.StatusUnauthorized {
			t.Errorf("router login with a removed token = %d, want 401", code)
		}
	}
	if e.runCount() != runs {
		t.Error("removing collectors deleted their history")
	}
	if e.get("/devices") != devicesBefore {
		t.Error("removing collectors changed the Devices page")
	}
	if body := e.get("/settings"); !strings.Contains(body, "desktop (removed)") {
		t.Error("history does not name the removed collector")
	}
	if !strings.Contains(logs.String(), "action=collectors") {
		t.Error("no maintenance log line")
	}

	// The name can be reused; the new collector works.
	again := e.createCollector("desktop")
	if code, r := e.upload(again.Token, withID(t, "valid_router.json", "5a7c1e2d-3b4f-4a6e-9d8c-000000000013", nil)); code != http.StatusCreated || r.Status != contract.StatusStored {
		t.Errorf("new desktop upload = %d %s", code, r.Status)
	}
}
