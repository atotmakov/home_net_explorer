package integration_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/atotmakov/home_net_explorer/internal/collect/accesspoint/eaptest"
	"github.com/atotmakov/home_net_explorer/internal/collect/collecttest"
)

// Feature 012, US3 (SC-004, SC-005): two access points set up in the web UI, read by the built-in
// collector, put their Wi-Fi clients under them on the Map.

// byHost sends each request to the fake that stands in for its host.
type byHost map[string]http.RoundTripper

func (b byHost) RoundTrip(r *http.Request) (*http.Response, error) {
	rt, ok := b[r.URL.Hostname()]
	if !ok {
		return nil, fmt.Errorf("no fake at %s", r.URL.Hostname())
	}
	return rt.RoundTrip(r)
}

func TestAccessPointEndToEnd(t *testing.T) {
	const pwA, pwB = "pw-hall-1", "pw-office-2"
	hall := eaptest.Start(t, eaptest.New, "admin", pwA)
	office := eaptest.Start(t, eaptest.Old, "admin", pwB)
	hall.SetClients(
		eaptest.Client("00-00-5E-10-00-21", "192.168.1.21", "Home", 1, "phone-a"),
		eaptest.Client("00-00-5E-10-00-22", "192.168.1.22", "Home", 1, "phone-b"))
	office.SetClients(eaptest.OldClient("00-00-5E-10-00-23", "192.168.1.23", "Home", "2.4GHz", "tablet"))
	e := newEnv(t, envOpts{builtinScan: true,
		apRT: byHost{"192.168.1.5": hall.Transport(), "192.168.1.10": office.Transport()},
		hosts: map[string]collecttest.Host{
			"192.168.1.5":  {MAC: "00:00:5e:10:00:05", Hostname: "eap-hall"},
			"192.168.1.10": {MAC: "00:00:5e:10:00:10", Hostname: "eap-office"},
			"192.168.1.21": {MAC: "00:00:5e:10:00:21"},
			"192.168.1.22": {MAC: "00:00:5e:10:00:22"},
			"192.168.1.23": {MAC: "00:00:5e:10:00:23"},
		}})
	e.waitFor("initial scan", func() bool { return e.runCount() == 1 })
	scan := func() {
		t.Helper()
		e.waitFor("scanner idle", func() bool { return !e.app.Scanner.Status().Running })
		n := e.runCount()
		if res, _ := e.req("POST", "/scan", url.Values{}); res.StatusCode != http.StatusAccepted {
			t.Fatalf("POST /scan = %d", res.StatusCode)
		}
		e.waitFor("scan", func() bool { return e.runCount() == n+1 })
		e.waitFor("scanner idle", func() bool { return !e.app.Scanner.Status().Running })
	}

	// The owner marks both devices as access points and enters their logins.
	for host, pw := range map[string]string{"eap-hall": pwA, "eap-office": pwB} {
		id, _ := strconv.ParseInt(deviceID(t, e, host), 10, 64)
		e.setType(id, "access_point")
		if code, body := e.saveAccessPoint(id, url.Values{"model": {"tplink-eap225"}, "username": {"admin"}, "password": {pw}}); code != http.StatusSeeOther {
			t.Fatalf("save %s = %d %s", host, code, body)
		}
	}
	scan()

	m := e.mapDoc()
	under := func(m mapDoc, apIP, via, ip string) bool {
		ap, ok1 := m.device(apIP)
		d, ok2 := m.device(ip)
		port := "port-" + strconv.FormatInt(ap.DeviceID, 10) + "-" + via
		return ok1 && ok2 && m.has("port", ap.ID, port) && m.has("port", port, d.ID)
	}
	for ip, want := range map[string][2]string{
		"192.168.1.21": {"192.168.1.5", "Home (5 GHz)"},
		"192.168.1.22": {"192.168.1.5", "Home (5 GHz)"},
		"192.168.1.23": {"192.168.1.10", "Home (2.4 GHz)"},
	} {
		if !under(m, want[0], want[1], ip) {
			t.Errorf("%s is not under %s %q on the map: %+v", ip, want[0], want[1], m.Edges)
		}
	}
	body := e.get("/collectors")
	for _, want := range []string{"access point tplink-eap225 at 192.168.1.5: OK, 2 clients",
		"access point tplink-eap225 at 192.168.1.10: OK, 1 clients"} {
		if !strings.Contains(body, want) {
			t.Errorf("Collectors page lacks %q", want)
		}
	}
	if body := e.get("/devices/" + deviceID(t, e, "phone-a")); !strings.Contains(body, "via eap-hall · Home (5 GHz)") {
		t.Errorf("phone's device page does not name its access point:\n%s", body)
	}

	// SC-005: the phone moves to the other access point.
	hall.SetClients(eaptest.Client("00-00-5E-10-00-22", "192.168.1.22", "Home", 1, "phone-b"))
	office.SetClients(eaptest.OldClient("00-00-5E-10-00-23", "192.168.1.23", "Home", "2.4GHz", "tablet"),
		eaptest.OldClient("00-00-5E-10-00-21", "192.168.1.21", "Home", "2.4GHz", "phone-a"))
	scan()
	m = e.mapDoc()
	if !under(m, "192.168.1.10", "Home (2.4 GHz)", "192.168.1.21") || under(m, "192.168.1.5", "Home (5 GHz)", "192.168.1.21") {
		t.Errorf("the phone that moved is not under the office AP only: %+v", m.Edges)
	}

	// SC-004: a wrong password costs one login attempt over three scans.
	id, _ := strconv.ParseInt(deviceID(t, e, "eap-office"), 10, 64)
	e.saveAccessPoint(id, url.Values{"model": {"tplink-eap225"}, "username": {"admin"}, "password": {"wrong"}})
	before := office.Logins()
	for i := 0; i < 3; i++ {
		scan()
	}
	if n := office.Logins() - before; n != 1 {
		t.Errorf("login attempts with a wrong password over three scans = %d, want 1", n)
	}
}
