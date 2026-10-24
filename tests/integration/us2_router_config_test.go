package integration_test

import (
	"encoding/json"
	"html"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// Feature 013 (replaces 003 FR-007): routers are set up only in the web UI, like access points.
// The config shown for a new collector has no routers section; collectors get the routers from
// ping at each scan.
func TestUS2NewCollectorConfigHasNoRouters(t *testing.T) {
	e := newEnv(t, envOpts{})
	id, _, token := routerDevice(t, e)
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
	m := configRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no config shown")
	}
	cfg := html.UnescapeString(m[1])
	if strings.Contains(cfg, "routers") || strings.Contains(cfg, "192.168.0.4") {
		t.Errorf("the generated config still lists routers:\n%s", cfg)
	}
	e.createCollector("tablet") // parses as strict YAML with every remaining setting

	// The router reaches collectors through ping, without credentials.
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/v1/ping", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	pres, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer pres.Body.Close()
	var ping contract.PingResponse
	if err := json.NewDecoder(pres.Body).Decode(&ping); err != nil {
		t.Fatal(err)
	}
	if len(ping.Routers) != 1 || ping.Routers[0].Address != "192.168.0.4" || ping.Routers[0].Model != "huawei-hg8145v5" {
		t.Errorf("ping routers = %+v, want the UI router", ping.Routers)
	}
}
