package integration_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// Feature 003, US2: the config shown for a new collector lists UI-configured routers without
// credentials (FR-007).
func TestUS2NewCollectorConfigListsRouters(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.login()
	id, _, _ := routerDevice(t, e)
	e.setType(id, "router")
	if code, body := e.saveRouter(id, url.Values{"model": {"huawei-hg8145v5"}, "username": {"root"}, "password": {routerMarker}}); code != http.StatusSeeOther {
		t.Fatalf("save router = %d %s", code, body)
	}

	res, body := e.req("POST", "/collectors", url.Values{"name": {"laptop"}})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("create collector = %d", res.StatusCode)
	}
	if strings.Contains(body, routerMarker) {
		t.Error("the Collectors page shows the router password")
	}
	cfg := e.createCollector("tablet")
	if len(cfg.Routers) != 1 {
		t.Fatalf("config routers = %+v, want the UI router", cfg.Routers)
	}
	r := cfg.Routers[0]
	if r["model"] != "huawei-hg8145v5" || r["url"] != "http://192.168.0.4" {
		t.Errorf("config router = %v", r)
	}
	for _, k := range []string{"username", "password"} {
		if _, ok := r[k]; ok {
			t.Errorf("config router carries %q", k)
		}
	}
}
