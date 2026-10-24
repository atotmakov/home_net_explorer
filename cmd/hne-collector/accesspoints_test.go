package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atotmakov/home_net_explorer/internal/collect/accesspoint/eaptest"
	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// Feature 012: access points configured in the web UI reach collectors through ping; their
// login is fetched from the server before each read, like routers (feature 003).

const apPassword = "pw-ap-4Wq!collector-marker"

// The harness scans 192.168.1.0/24 on-link, so the fake access point sits there.
var uiAP = contract.RouterRef{ID: 7, Model: "tplink-eap225", Address: "192.168.1.5", Subnet: "192.168.1.0/24"}

func apHarness(t *testing.T) (*harness, *fakeServer, *eaptest.AP) {
	t.Helper()
	srv := newFakeServer(t)
	srv.accessPoints = []contract.RouterRef{uiAP}
	srv.logins[7] = contract.RouterLogin{Username: "admin", Password: apPassword}
	ap := eaptest.Start(t, eaptest.New, "admin", apPassword)
	ap.SetClients(
		eaptest.Client("00-00-5E-00-53-13", "192.168.1.13", "Home", 1, "Pixel-9a"),
		eaptest.Client("00-00-5E-00-53-40", "", "Home", 0, ""),
	)
	h := newHarness(t, srv.srv.URL)
	h.apRT = ap.Transport()
	return h, srv, ap
}

func TestServerAccessPointIsRead(t *testing.T) {
	h, srv, ap := apHarness(t)
	if code := h.run("scan", "--once"); code != 0 {
		t.Fatalf("scan = %d\n%s", code, h.stderr.String())
	}
	run := lastUpload(t, srv)
	want := contract.RunSource{Type: "access_point", Model: "tplink-eap225", Address: "192.168.1.5", Subnet: "192.168.1.0/24",
		Outcome: "ok", Online: 1, Offline: 1}
	if len(run.Sources) != 1 || run.Sources[0] != want {
		t.Fatalf("sources = %+v, want [%+v]", run.Sources, want)
	}
	var phone *contract.Observation
	for i, o := range run.Observations {
		if o.IP == "192.168.1.13" {
			phone = &run.Observations[i]
		}
	}
	if phone == nil || phone.Method != contract.ObsAPClient || phone.Via != "Home (5 GHz)" || phone.AccessPoint != "192.168.1.5" ||
		phone.MAC != "00:00:5e:00:53:13" || phone.Hostname != "Pixel-9a" {
		t.Errorf("phone = %+v", phone)
	}
	if srv.loginRequests != 1 || ap.Logins() != 1 || ap.Logouts() != 1 {
		t.Errorf("login fetches %d, AP logins %d, logouts %d; want 1 each", srv.loginRequests, ap.Logins(), ap.Logouts())
	}

	// The list is cached for scans while the server is down, without credentials.
	cache, err := os.ReadFile(filepath.Join(h.dir, "hne-collector.access-points"))
	if err != nil {
		t.Fatalf("no access point cache: %v", err)
	}
	if !strings.Contains(string(cache), "192.168.1.5") || strings.Contains(string(cache), apPassword) || strings.Contains(string(cache), "password") {
		t.Errorf("access point cache = %s", cache)
	}

	// Server down: the cached list is used, the AP is not contacted without a login.
	writeConfig(t, h.dir, map[string]any{"server_url": closedURL(t), "name": "desktop", "token": "tok", "subnets": []string{}, "interval_seconds": 900})
	requests := len(ap.Requests())
	if code := h.run("scan", "--once"); code != 5 {
		t.Fatalf("scan with the server down = %d, want 5 (spooled)", code)
	}
	if len(ap.Requests()) != requests {
		t.Error("the access point was contacted without a login")
	}
	files, _ := filepath.Glob(filepath.Join(h.dir, "spool", "*.json"))
	if len(files) != 1 {
		t.Fatalf("spool = %v", files)
	}
	b, _ := os.ReadFile(files[0])
	spooled, err := contract.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if len(spooled.Sources) != 1 || spooled.Sources[0].Outcome != contract.OutcomeLoginUnavailable {
		t.Errorf("sources with the server down = %+v, want login_unavailable", spooled.Sources)
	}
}

// check prints, per access point, the endpoint, the client count and the key names of the first
// entry (gate G1), never a MAC, address, name or credential of a client.
func TestCheckShowsAccessPoints(t *testing.T) {
	h, _, _ := apHarness(t)
	if code := h.run("check"); code != 0 {
		t.Fatalf("check = %d\n%s", code, h.stderr.String())
	}
	out := h.stderr.String()
	want := "tplink-eap225 at 192.168.1.5 (login from server): OK, 2 clients, 1 without an IP address " +
		"(endpoint status.client.user; keys: IP, MAC, Radio, SSID, hostname)"
	if !strings.Contains(out, "Access points:") || !strings.Contains(out, want) {
		t.Errorf("check lacks %q:\n%s", want, out)
	}
	for _, secret := range []string{apPassword, "00-00-5E-00-53-13", "00:00:5e:00:53:13", "192.168.1.13", "Pixel-9a"} {
		if strings.Contains(out, secret) {
			t.Errorf("check output shows %q", secret)
		}
	}
}

// A rejected login is tried once, then skipped until check runs (spec FR-010).
func TestAccessPointRejectionAndCheck(t *testing.T) {
	h, srv, ap := apHarness(t)
	srv.logins[7] = contract.RouterLogin{Username: "admin", Password: "wrong"}
	for i := 0; i < 3; i++ {
		h.run("scan", "--once")
	}
	if ap.Logins() != 1 {
		t.Errorf("login attempts over 3 scans = %d, want 1", ap.Logins())
	}
	if run := lastUpload(t, srv); len(run.Sources) != 1 || run.Sources[0].Outcome != contract.OutcomeSkippedAfterRejection {
		t.Errorf("sources = %+v, want skipped_after_rejection", run.Sources)
	}
	h.run("check")
	if ap.Logins() != 2 {
		t.Errorf("check made %d login attempts, want 1 more", ap.Logins()-1)
	}
	if !strings.Contains(h.stderr.String(), "login rejected") {
		t.Errorf("check output:\n%s", h.stderr.String())
	}
}

// Gate G1: check shows why a client list was not understood (key names and flags only).
func TestCheckShowsWhyNotUnderstood(t *testing.T) {
	h, _, ap := apHarness(t)
	ap.SetList([]byte(`{"success":true,"timeout":true}`))
	h.run("check")
	want := "page not understood (firmware?): status.client.user answered not logged in (timeout=true): the access point did not accept the login session"
	if !strings.Contains(h.stderr.String(), want) {
		t.Errorf("check lacks %q:\n%s", want, h.stderr.String())
	}
}
