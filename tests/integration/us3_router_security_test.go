package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/auth"
	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// Feature 003, US3: only active collector tokens get router logins (FR-009, SC-005), and the
// stored password appears in no page and no server log line (FR-004, SC-002).

func configuredRouter(t *testing.T, e *env, password string) (routerID int64, token string) {
	t.Helper()
	id, key, token := routerDevice(t, e)
	e.setType(id, "router")
	if code, body := e.saveRouter(id, url.Values{"model": {"huawei-hg8145v5"}, "username": {"root"}, "password": {password}}); code != http.StatusSeeOther {
		t.Fatalf("save router = %d %s", code, body)
	}
	v, _, err := e.app.Store.GetRouterSettings(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	return v.ID, token
}

func TestUS3RouterLoginNeedsACollectorToken(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.login()
	routerID, token := configuredRouter(t, e, routerMarker)
	path := fmt.Sprintf("%s/api/v1/routers/%d/login", e.srv.URL, routerID)

	revoked := e.createCollector("old-laptop")
	var revokedID int64
	e.app.Store.DB().QueryRow(`SELECT id FROM collectors WHERE name = 'old-laptop'`).Scan(&revokedID)
	if err := auth.RevokeCollector(context.Background(), e.app.Store.DB(), revokedID, time.Now()); err != nil {
		t.Fatal(err)
	}

	check := func(label string, client *http.Client, bearer string) {
		t.Helper()
		req, _ := http.NewRequest("GET", path, nil)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401", label, res.StatusCode)
		}
		var er contract.ErrorResponse
		if json.Unmarshal(b, &er) != nil || er.Error != contract.CodeInvalidToken {
			t.Errorf("%s: body %s, want invalid_token", label, b)
		}
		if bytes.Contains(b, []byte(routerMarker)) {
			t.Errorf("%s: the refusal carries the password", label)
		}
	}
	check("no token", http.DefaultClient, "")
	check("unknown token", http.DefaultClient, "not-a-token")
	check("revoked token", http.DefaultClient, revoked.Token)
	check("owner's browser session only", e.http, "") // e.http carries the owner's session cookie

	if code := e.api(token, fmt.Sprintf("/api/v1/routers/%d/login", routerID), nil); code != http.StatusOK {
		t.Errorf("active collector token: %d, want 200", code)
	}
}

func TestUS3PasswordInNoPageOrLog(t *testing.T) {
	var logs bytes.Buffer
	e := newEnv(t, envOpts{log: &logs})
	e.login()
	routerID, token := configuredRouter(t, e, routerMarker)
	var dev int64
	e.app.Store.DB().QueryRow(`SELECT id FROM devices WHERE mac = '00:00:5e:10:00:02'`).Scan(&dev)

	var pages strings.Builder
	for _, p := range []string{"/", "/devices", fmt.Sprintf("/devices/%d", dev), "/collectors", "/settings"} {
		pages.WriteString(e.get(p))
	}
	_, created := e.req("POST", "/collectors", url.Values{"name": {"new-pc"}})
	pages.WriteString(created)
	if strings.Contains(pages.String(), routerMarker) {
		t.Error("a page shows the router password")
	}
	// A failed save re-renders the device page: the typed password must not be echoed either.
	_, failed := e.post(fmt.Sprintf("/devices/%d/router", dev), url.Values{"model": {"bogus"}, "username": {"root"}, "password": {routerMarker + "-typed"}})
	if strings.Contains(failed, routerMarker) {
		t.Error("the error page echoes the typed password")
	}

	e.api(token, fmt.Sprintf("/api/v1/routers/%d/login", routerID), nil)
	if strings.Contains(logs.String(), routerMarker) {
		t.Error("the server log contains the router password")
	}
	if !strings.Contains(logs.String(), "/login") {
		t.Error("the server log capture did not see the login request (test setup)")
	}
}
