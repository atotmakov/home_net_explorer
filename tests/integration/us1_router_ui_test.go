package integration_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/atotmakov/home_net_explorer/internal/contract/contracttest"
)

// Feature 003, US1: configure a router from its device page.

const routerMarker = "pw-ui-7Qz!marker"

// routerDevice uploads valid_router.json and returns the id of the device at 192.168.0.4
// (mac 00:00:5e:10:00:02), its identity key and the collector token.
func routerDevice(t *testing.T, e *env) (int64, string, string) {
	t.Helper()
	cfg := e.createCollector("desktop")
	if status, _ := e.upload(cfg.Token, contracttest.Fixture(t, "valid_router.json")); status != http.StatusCreated {
		t.Fatalf("upload = %d", status)
	}
	var id int64
	var key string
	if err := e.app.Store.DB().QueryRow(`SELECT id, identity_key FROM devices WHERE mac = '00:00:5e:10:00:02'`).Scan(&id, &key); err != nil {
		t.Fatal(err)
	}
	return id, key, cfg.Token
}

func (e *env) post(path string, form url.Values) (*http.Response, string) {
	e.t.Helper()
	return e.req("POST", path, form)
}

func (e *env) setType(id int64, typ string) {
	e.t.Helper()
	if res, body := e.post(fmt.Sprintf("/devices/%d/attrs", id), url.Values{"type": {typ}}); res.StatusCode != http.StatusSeeOther {
		e.t.Fatalf("set type %s = %d %s", typ, res.StatusCode, body)
	}
}

func (e *env) saveRouter(id int64, form url.Values) (int, string) {
	e.t.Helper()
	res, body := e.post(fmt.Sprintf("/devices/%d/router", id), form)
	return res.StatusCode, body
}

func (e *env) storedPassword(key string) string {
	e.t.Helper()
	v, ok, err := e.app.Store.GetRouterSettings(context.Background(), key)
	if err != nil || !ok {
		return ""
	}
	l, err := e.app.Store.RouterLogin(context.Background(), v.ID)
	if err != nil {
		e.t.Fatal(err)
	}
	return l.Password
}

func TestUS1RouterSettingsOnDevicePage(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.login()
	id, key, _ := routerDevice(t, e)
	page := fmt.Sprintf("/devices/%d", id)

	if body := e.get(page); strings.Contains(body, `id="router"`) {
		t.Error("the Router card is shown for a device that is not a router")
	}
	e.setType(id, "router")
	body := e.get(page)
	if !strings.Contains(body, `id="router"`) || !strings.Contains(body, `<option value="huawei-hg8145v5"`) {
		t.Fatalf("no Router card with the supported models:\n%s", body)
	}

	if code, body := e.saveRouter(id, url.Values{"model": {"huawei-hg8145v5"}, "username": {"root"}, "password": {routerMarker}}); code != http.StatusSeeOther {
		t.Fatalf("save router = %d %s", code, body)
	}
	body = e.get(page)
	for _, want := range []string{"Password: set", `value="root"`, "192.168.0.4", "192.168.0.0/24"} {
		if !strings.Contains(body, want) {
			t.Errorf("device page lacks %q", want)
		}
	}
	if e.storedPassword(key) != routerMarker {
		t.Fatal("password not stored")
	}

	// An empty password keeps the stored one; a new one replaces it.
	e.saveRouter(id, url.Values{"model": {"huawei-hg8145v5"}, "username": {"admin"}, "password": {""}})
	if e.storedPassword(key) != routerMarker {
		t.Error("an empty password field must keep the stored password")
	}
	e.saveRouter(id, url.Values{"model": {"huawei-hg8145v5"}, "username": {"admin"}, "password": {"pw-new"}})
	if e.storedPassword(key) != "pw-new" {
		t.Error("a new password must replace the stored one")
	}

	long := strings.Repeat("x", 129)
	for label, form := range map[string]url.Values{
		"unknown model":      {"model": {"netgear-r7000"}, "username": {"root"}, "password": {"pw"}},
		"empty username":     {"model": {"huawei-hg8145v5"}, "username": {""}, "password": {"pw"}},
		"username too long":  {"model": {"huawei-hg8145v5"}, "username": {long}, "password": {"pw"}},
		"password too long":  {"model": {"huawei-hg8145v5"}, "username": {"root"}, "password": {long}},
		"subnet not current": {"model": {"huawei-hg8145v5"}, "username": {"root"}, "password": {"pw"}, "subnet": {"10.20.30.0/24"}},
	} {
		if code, _ := e.saveRouter(id, form); code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", label, code)
		}
	}

	// Remove, and type change away from router, both delete the settings.
	if res, _ := e.post(fmt.Sprintf("/devices/%d/router/remove", id), url.Values{}); res.StatusCode != http.StatusSeeOther {
		t.Errorf("remove = %d", res.StatusCode)
	}
	if e.storedPassword(key) != "" {
		t.Error("router still configured after Remove")
	}
	e.saveRouter(id, url.Values{"model": {"huawei-hg8145v5"}, "username": {"root"}, "password": {"pw"}})
	e.setType(id, "printer")
	if e.storedPassword(key) != "" {
		t.Error("router still configured after the type changed to printer")
	}
}

func TestUS1RouterNeedsPasswordAndAddress(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.login()
	id, _, _ := routerDevice(t, e)
	e.setType(id, "router")
	if code, _ := e.saveRouter(id, url.Values{"model": {"huawei-hg8145v5"}, "username": {"root"}, "password": {""}}); code != http.StatusBadRequest {
		t.Errorf("first save without a password = %d, want 400", code)
	}

	// Ignoring its only subnet leaves the device without a usable address.
	if res, _ := e.post("/subnets", url.Values{"cidr": {"192.168.0.0/24"}, "ignored": {"true"}}); res.StatusCode >= 400 {
		t.Fatalf("ignore subnet = %d", res.StatusCode)
	}
	body := e.get(fmt.Sprintf("/devices/%d", id))
	if !strings.Contains(body, "No current private address") {
		t.Error("the Router card does not say the device has no usable address")
	}
	if code, _ := e.saveRouter(id, url.Values{"model": {"huawei-hg8145v5"}, "username": {"root"}, "password": {"pw"}}); code != http.StatusBadRequest {
		t.Errorf("save without a usable address = %d, want 400", code)
	}
}

func TestUS1RouterEditsNeedTheOwner(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.login()
	id, key, _ := routerDevice(t, e)
	e.setType(id, "router")

	// A cross-site request is refused (feature 001 protections).
	r, _ := http.NewRequest("POST", fmt.Sprintf("%s/devices/%d/router", e.srv.URL, id),
		strings.NewReader(url.Values{"model": {"huawei-hg8145v5"}, "username": {"root"}, "password": {"pw"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "http://evil.example")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	res, err := e.http.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("cross-site save = %d, want 403", res.StatusCode)
	}

	// Without a session nothing changes.
	anon := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r, _ = http.NewRequest("POST", fmt.Sprintf("%s/devices/%d/router", e.srv.URL, id),
		strings.NewReader(url.Values{"model": {"huawei-hg8145v5"}, "username": {"root"}, "password": {"pw"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", e.srv.URL)
	if res, err := anon.Do(r); err == nil {
		res.Body.Close()
		if res.StatusCode == http.StatusSeeOther && strings.Contains(res.Header.Get("Location"), fmt.Sprintf("/devices/%d", id)) {
			t.Error("an anonymous request was accepted")
		}
	}
	if e.storedPassword(key) != "" {
		t.Error("router saved without the owner's session or from another site")
	}
}
