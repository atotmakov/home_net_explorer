package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/atotmakov/home_net_explorer/internal/collect/router/routertest"
	"github.com/atotmakov/home_net_explorer/internal/contract"
)

const routerPassword = "pw-7Hk2!router-marker"

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

func TestRouterConfigErrors(t *testing.T) {
	bad := map[string]func(r map[string]any){
		"unknown model":      func(r map[string]any) { r["model"] = "netgear-r7000" },
		"public address":     func(r map[string]any) { r["url"] = "http://8.8.8.8" },
		"hostname":           func(r map[string]any) { r["url"] = "http://router.lan" },
		"missing username":   func(r map[string]any) { delete(r, "username") },
		"missing password":   func(r map[string]any) { delete(r, "password") },
		"non-private subnet": func(r map[string]any) { r["subnet"] = "8.8.8.0/24" },
		"/31 subnet":         func(r map[string]any) { r["subnet"] = "192.168.0.0/31" },
	}
	for label, f := range bad {
		h := newHarness(t, "http://unused")
		r := routerEntry()
		f(r)
		h.withRouters(t, "http://unused", r)
		if code := h.run("scan", "--once", "--dry-run"); code != 2 {
			t.Errorf("%s: exit %d, want 2 (config error)\n%s", label, code, h.stderr.String())
		}
		if strings.Contains(h.stderr.String()+h.stdout.String(), routerPassword) {
			t.Errorf("%s: the config error reveals the router password", label)
		}
	}
	h := newHarness(t, "http://unused")
	var many []map[string]any
	for i := 0; i < 9; i++ {
		many = append(many, routerEntry())
	}
	h.withRouters(t, "http://unused", many...)
	if code := h.run("scan", "--once", "--dry-run"); code != 2 {
		t.Errorf("9 routers: exit %d, want 2 (at most %d)", code, contract.MaxSources)
	}
}

func TestRouterScanUploadsDevices(t *testing.T) {
	srv := newFakeServer(t)
	f := routertest.New(t, "root", routerPassword)
	h := newHarness(t, srv.srv.URL)
	h.routerRT = f.Transport()
	h.withRouters(t, srv.srv.URL, routerEntry())

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
	if n != 13 {
		t.Errorf("router observations = %d, want 13", n)
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
	f := routertest.New(t, "root", routerPassword)
	h := newHarness(t, "http://unused")
	h.routerRT = f.Transport()
	h.withRouters(t, "http://unused", routerEntry())
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
}
