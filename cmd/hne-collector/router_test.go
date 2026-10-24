package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/atotmakov/home_net_explorer/internal/collect/router/routertest"
	"github.com/atotmakov/home_net_explorer/internal/contract"
)

const routerPassword = "pw-7Hk2!router-marker"

// routerEntry is a routers entry of an older hne-collector.yaml (features 002, 003).
func routerEntry() map[string]any {
	return map[string]any{"model": "huawei-hg8145v5", "url": "http://192.168.0.1", "username": "root", "password": routerPassword}
}

// withRouters rewrites the harness config with these router entries.
func (h *harness) withRouters(t *testing.T, serverURL string, routers ...map[string]any) {
	t.Helper()
	writeConfig(t, h.dir, map[string]any{"server_url": serverURL, "name": "desktop", "token": "tok",
		"subnets": []string{}, "interval_seconds": 900, "routers": routers})
}

func lastUpload(t *testing.T, srv *fakeServer) *contract.CollectionRun {
	t.Helper()
	if len(srv.bodies) == 0 {
		t.Fatal("nothing uploaded")
	}
	run, err := contract.Decode(bytes.NewReader(srv.bodies[len(srv.bodies)-1]))
	if err != nil {
		t.Fatal(err)
	}
	return run
}

// Feature 013: routers are set up only in the web UI, like access points. A routers section in
// an older hne-collector.yaml still loads, is never used (not even its login), and the
// collector says so.
func TestConfigRoutersIgnored(t *testing.T) {
	srv := newFakeServer(t)
	f := routertest.New(t, "root", routerPassword)
	h := newHarness(t, srv.srv.URL)
	h.routerRT = f.Transport()
	bad := routerEntry()
	bad["url"] = "http://8.8.8.8" // once a config error; now just ignored
	var many []map[string]any
	for i := 0; i < 9; i++ {
		many = append(many, routerEntry())
	}
	for label, entries := range map[string][]map[string]any{
		"with a login":    {routerEntry()},
		"without a login": {{"model": "huawei-hg8145v5", "url": "http://192.168.0.1"}},
		"invalid entry":   {bad},
		"9 entries":       many,
	} {
		h.withRouters(t, srv.srv.URL, entries...)
		if code := h.run("scan", "--once"); code != 0 {
			t.Fatalf("%s: scan = %d\n%s", label, code, h.stderr.String())
		}
		out := h.stderr.String() + h.stdout.String()
		if !strings.Contains(out, "the routers section of hne-collector.yaml is no longer used") {
			t.Errorf("%s: no warning about the routers section:\n%s", label, out)
		}
		if strings.Contains(out, routerPassword) {
			t.Errorf("%s: the output shows the router password", label)
		}
		if run := lastUpload(t, srv); len(run.Sources) != 0 {
			t.Errorf("%s: sources = %+v, want none (no router set up in the web UI)", label, run.Sources)
		}
	}
	if len(f.Requests()) != 0 {
		t.Errorf("the router was contacted %d times from the config file", len(f.Requests()))
	}
	h.withRouters(t, srv.srv.URL, routerEntry())
	h.run("scan", "--once")
	if !strings.Contains(h.stderr.String(), "enter it on the router's device page") {
		t.Errorf("no hint that the file's router login is ignored:\n%s", h.stderr.String())
	}
	h.withRouters(t, srv.srv.URL)
	h.run("scan", "--once")
	if strings.Contains(h.stderr.String(), "no longer used") {
		t.Errorf("an empty routers section still warns:\n%s", h.stderr.String())
	}

	// The same router set up in the web UI is read with the server's login, not the file's.
	srv.routers = []contract.RouterRef{uiRouter}
	srv.logins[1] = contract.RouterLogin{Username: "root", Password: routerPassword}
	h.withRouters(t, srv.srv.URL, map[string]any{"model": "huawei-hg8145v5", "url": "http://192.168.0.1",
		"username": "root", "password": "file-password"})
	h.run("scan", "--once")
	if c := f.Credentials(); len(c) != 1 || c[0] != "root:"+routerPassword {
		t.Errorf("router saw %v, want the server's login only", c)
	}
	if srv.loginRequests != 1 {
		t.Errorf("login fetched %d times, want 1", srv.loginRequests)
	}
}

func TestRouterScanUploadsDevices(t *testing.T) {
	h, srv, f := serverRouterHarness(t)
	if code := h.run("scan", "--once"); code != 0 {
		t.Fatalf("scan --once = %d\n%s", code, h.stderr.String())
	}
	run := lastUpload(t, srv)
	want := contract.RunSource{Type: "router", Model: "huawei-hg8145v5", Address: "192.168.0.1", Subnet: "192.168.0.0/24", Outcome: "ok", Online: 13, Offline: 16}
	if len(run.Sources) != 1 || run.Sources[0] != want {
		t.Errorf("sources = %+v, want [%+v]", run.Sources, want)
	}
	n := 0
	for _, o := range run.Observations {
		if o.Method == contract.ObsRouterTable {
			n++
		}
	}
	if n != 14 { // 13 online devices and the router itself
		t.Errorf("router observations = %d, want 14", n)
	}
	for _, o := range run.Observations {
		if o.IP == "192.168.0.1" && (o.MAC != "00:e0:fc:12:34:56" || o.Hostname != "HG8145V5") {
			t.Errorf("the router itself = %+v, want its MAC and model from the device-information page", o)
		}
	}
	if !strings.Contains(h.stderr.String(), "router=ok") {
		t.Errorf("summary lacks router=ok:\n%s", h.stderr.String())
	}
	if f.SessionOpen() {
		t.Error("the collector left a router session open")
	}

	// A router failure never changes the exit code; the rest of the scan is uploaded (FR-010).
	f.SetMode(routertest.WrongPassword)
	if code := h.run("scan", "--once"); code != 0 {
		t.Errorf("scan with a rejected router login = %d, want 0", code)
	}
	if run := lastUpload(t, srv); len(run.Sources) != 1 || run.Sources[0].Outcome != contract.OutcomeLoginRejected {
		t.Errorf("sources after a rejected login = %+v", run.Sources)
	}
	if !strings.Contains(h.stderr.String(), "router=login_rejected") {
		t.Errorf("summary lacks router=login_rejected:\n%s", h.stderr.String())
	}
}

func TestRouterDryRun(t *testing.T) {
	h, srv, _ := serverRouterHarness(t)
	if code := h.run("scan", "--once", "--dry-run"); code != 0 {
		t.Fatalf("dry run = %d\n%s", code, h.stderr.String())
	}
	run, err := contract.Decode(bytes.NewReader(h.stdout.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if err := contract.Validate(run); err != nil {
		t.Errorf("dry-run payload invalid: %v", err)
	}
	if !strings.Contains(h.stdout.String(), `"router_table"`) {
		t.Error("dry run lacks router_table data")
	}
	if srv.uploads != 0 {
		t.Errorf("dry run uploaded %d runs", srv.uploads)
	}
}
