package contract_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/contract/contracttest"
	"github.com/atotmakov/home_net_explorer/internal/store"
)

// Feature 003: ping lists UI-configured routers (no credentials); the login endpoint gives the
// login to collector tokens.

const routerSecret = "pw-api-4Rt!marker"

// withRouter uploads valid_router.json and configures the device at 192.168.0.4 as a router.
func withRouter(t *testing.T, s *server) int64 {
	t.Helper()
	if status, body := s.do("POST", "/api/v1/collections", s.token, contracttest.Fixture(t, "valid_router.json")); status != http.StatusCreated {
		t.Fatalf("upload = %d %s", status, body)
	}
	ctx := context.Background()
	err := s.app.Store.Tx(ctx, func(tx *sql.Tx) error {
		return store.SaveRouter(ctx, tx, "mac:00:00:5e:10:00:02",
			store.RouterSettings{Model: "huawei-hg8145v5", Username: "root", Password: routerSecret}, time.Now())
	})
	if err != nil {
		t.Fatal(err)
	}
	v, _, err := s.app.Store.GetRouterSettings(ctx, "mac:00:00:5e:10:00:02")
	if err != nil {
		t.Fatal(err)
	}
	return v.ID
}

func TestPingListsRouters(t *testing.T) {
	s := newServer(t)
	id := withRouter(t, s)
	status, body := s.do("GET", "/api/v1/ping", s.token, nil)
	if status != http.StatusOK {
		t.Fatalf("ping = %d", status)
	}
	if err := contracttest.Validate(contracttest.Schema(t, "PingResponse"), body); err != nil {
		t.Errorf("ping fails its schema: %v", err)
	}
	var p contract.PingResponse
	json.Unmarshal(body, &p)
	want := contract.RouterRef{ID: id, Model: "huawei-hg8145v5", Address: "192.168.0.4", Subnet: "192.168.0.0/24"}
	if len(p.Routers) != 1 || p.Routers[0] != want {
		t.Errorf("routers = %+v, want [%+v]", p.Routers, want)
	}
	if strings.Contains(string(body), routerSecret) || strings.Contains(string(body), "password") {
		t.Errorf("ping carries credentials: %s", body)
	}
}

func TestRouterLoginEndpoint(t *testing.T) {
	s := newServer(t)
	id := withRouter(t, s)
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/routers/%d/login", s.srv.URL, id), nil)
	req.Header.Set("Authorization", "Bearer "+s.token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login = %d", res.StatusCode)
	}
	if cc := res.Header.Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	var l contract.RouterLogin
	if err := json.NewDecoder(res.Body).Decode(&l); err != nil || l.Username != "root" || l.Password != routerSecret {
		t.Errorf("login = %+v, %v", l, err)
	}

	for _, path := range []string{"/api/v1/routers/9999/login", "/api/v1/routers/abc/login"} {
		status, body := s.do("GET", path, s.token, nil)
		expectError(t, path, status, body, http.StatusNotFound, "router_not_found")
	}
}
