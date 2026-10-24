package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// Feature 012, US1: configure an access point from its device page, like a router (feature 003).

const apMarker = "pw-ap-9Kx!marker"

func (e *env) saveAccessPoint(id int64, form url.Values) (int, string) {
	e.t.Helper()
	res, body := e.post(fmt.Sprintf("/devices/%d/access-point", id), form)
	return res.StatusCode, body
}

func TestAccessPointSettingsOnDevicePage(t *testing.T) {
	e := newEnv(t, envOpts{})
	id, key, token := routerDevice(t, e) // 192.168.0.4 stands in for the EAP225
	page := fmt.Sprintf("/devices/%d", id)
	ok := url.Values{"model": {"tplink-eap225"}, "username": {"admin"}, "password": {apMarker}}

	if code, _ := e.saveAccessPoint(id, ok); code != http.StatusBadRequest {
		t.Errorf("save on an untyped device = %d, want 400", code)
	}
	if body := e.get(page); strings.Contains(body, `id="access-point"`) {
		t.Error("the Access point card is shown for an untyped device")
	}
	e.setType(id, "router")
	if body := e.get(page); strings.Contains(body, `id="access-point"`) || strings.Contains(body, `value="tplink-eap225"`) {
		t.Error("a router shows the Access point card or the access point model")
	}
	e.setType(id, "access_point")
	body := e.get(page)
	if !strings.Contains(body, `id="access-point"`) || !strings.Contains(body, `<option value="tplink-eap225"`) {
		t.Fatalf("no Access point card with the supported models:\n%s", body)
	}
	if strings.Contains(body, `id="router"`) || strings.Contains(body, `value="huawei-hg8145v5"`) {
		t.Error("an access point shows the Router card or the router model")
	}

	if code, _ := e.saveAccessPoint(id, url.Values{"model": {"tplink-eap225"}, "username": {"admin"}}); code != http.StatusBadRequest {
		t.Errorf("first save without a password = %d, want 400", code)
	}
	for label, form := range map[string]url.Values{
		"router model":   {"model": {"huawei-hg8145v5"}, "username": {"admin"}, "password": {"pw"}},
		"unknown model":  {"model": {"ubiquiti-uap"}, "username": {"admin"}, "password": {"pw"}},
		"empty username": {"model": {"tplink-eap225"}, "username": {""}, "password": {"pw"}},
		"public subnet":  {"model": {"tplink-eap225"}, "username": {"admin"}, "password": {"pw"}, "subnet": {"8.8.8.0/24"}},
	} {
		if code, _ := e.saveAccessPoint(id, form); code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", label, code)
		}
	}
	if code, body := e.saveAccessPoint(id, ok); code != http.StatusSeeOther {
		t.Fatalf("save access point = %d %s", code, body)
	}
	body = e.get(page)
	for _, want := range []string{"Password: set", `value="admin"`, "192.168.0.4"} {
		if !strings.Contains(body, want) {
			t.Errorf("device page lacks %q", want)
		}
	}
	if strings.Contains(body, apMarker) {
		t.Error("the device page shows the stored password")
	}
	if e.storedPassword(key) != apMarker {
		t.Fatal("password not stored")
	}
	// Collectors learn the access point from ping, without credentials, and not as a router.
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/v1/ping", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	pres, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var raw json.RawMessage
	json.NewDecoder(pres.Body).Decode(&raw)
	pres.Body.Close()
	var ping contract.PingResponse
	json.Unmarshal(raw, &ping)
	if len(ping.AccessPoints) != 1 || ping.AccessPoints[0].Model != "tplink-eap225" || ping.AccessPoints[0].Address != "192.168.0.4" ||
		len(ping.Routers) != 0 {
		t.Errorf("ping = %s, want the access point under access_points only", raw)
	}
	if strings.Contains(string(raw), apMarker) || strings.Contains(string(raw), "password") {
		t.Errorf("ping leaks the login: %s", raw)
	}
	// The router form cannot overwrite an access point's settings.
	if code, _ := e.saveRouter(id, url.Values{"model": {"huawei-hg8145v5"}, "username": {"root"}, "password": {"pw"}}); code != http.StatusBadRequest {
		t.Errorf("router save on an access point = %d, want 400", code)
	}

	if res, _ := e.post(fmt.Sprintf("/devices/%d/access-point/remove", id), url.Values{}); res.StatusCode != http.StatusSeeOther {
		t.Errorf("remove = %d", res.StatusCode)
	}
	if e.storedPassword(key) != "" {
		t.Error("access point still configured after Remove")
	}

	// Changing the type away deletes the settings.
	e.saveAccessPoint(id, ok)
	e.setType(id, "switch")
	if e.storedPassword(key) != "" {
		t.Error("access point settings kept after the type changed to switch")
	}

	// Cross-site posts are refused (feature 007).
	e.setType(id, "access_point")
	req, _ = http.NewRequest("POST", e.srv.URL+fmt.Sprintf("/devices/%d/access-point", id), strings.NewReader(ok.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://evil.example")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("cross-site save = %d, want 403", res.StatusCode)
	}
}
