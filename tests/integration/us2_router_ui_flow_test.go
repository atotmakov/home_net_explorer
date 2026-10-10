package integration_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// Feature 003, US2 (SC-001, SC-003), server half: a router configured in the UI is offered to an
// existing collector at its next ping, with its current login. The collector half is
// cmd/hne-collector (server_routers_test.go).

func (e *env) api(token, path string, out any) int {
	e.t.Helper()
	req, _ := http.NewRequest("GET", e.srv.URL+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if out != nil && res.StatusCode == http.StatusOK {
		if err := json.Unmarshal(b, out); err != nil {
			e.t.Fatalf("%s: %v (%s)", path, err, b)
		}
	}
	return res.StatusCode
}

func TestUS2UIRouterReachesExistingCollector(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.login()
	id, _, token := routerDevice(t, e) // the collector exists before the router is configured

	var ping contract.PingResponse
	e.api(token, "/api/v1/ping", &ping)
	if len(ping.Routers) != 0 {
		t.Fatalf("routers before any setup = %+v", ping.Routers)
	}

	e.setType(id, "router")
	e.saveRouter(id, url.Values{"model": {"huawei-hg8145v5"}, "username": {"root"}, "password": {"pw-first"}})
	e.api(token, "/api/v1/ping", &ping)
	if len(ping.Routers) != 1 || ping.Routers[0].Address != "192.168.0.4" || ping.Routers[0].Subnet != "192.168.0.0/24" {
		t.Fatalf("routers at the next ping = %+v", ping.Routers)
	}
	path := fmt.Sprintf("/api/v1/routers/%d/login", ping.Routers[0].ID)
	var l contract.RouterLogin
	if code := e.api(token, path, &l); code != http.StatusOK || l.Username != "root" || l.Password != "pw-first" {
		t.Fatalf("login = %d %+v", code, l)
	}

	// SC-003: a password change in the UI is what the next read gets.
	e.saveRouter(id, url.Values{"model": {"huawei-hg8145v5"}, "username": {"root"}, "password": {"pw-second"}})
	if e.api(token, path, &l); l.Password != "pw-second" {
		t.Errorf("login after the change = %+v", l)
	}

	// Removing the router takes it off the list.
	e.post(fmt.Sprintf("/devices/%d/router/remove", id), url.Values{})
	ping = contract.PingResponse{}
	e.api(token, "/api/v1/ping", &ping)
	if len(ping.Routers) != 0 {
		t.Errorf("routers after removal = %+v", ping.Routers)
	}
	if code := e.api(token, path, nil); code != http.StatusNotFound {
		t.Errorf("login of a removed router = %d, want 404", code)
	}
}
