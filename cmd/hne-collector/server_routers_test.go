package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atotmakov/home_net_explorer/internal/collect/router/routertest"
	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// Feature 003, US2: routers configured in the web UI reach collectors through ping, and their
// login is fetched from the server before each read.

var uiRouter = contract.RouterRef{ID: 1, Model: "huawei-hg8145v5", Address: "192.168.0.1", Subnet: "192.168.0.0/24"}

// serverRouterHarness: the server lists uiRouter with the right login; the config lists no routers.
func serverRouterHarness(t *testing.T) (*harness, *fakeServer, *routertest.Fake) {
	t.Helper()
	srv := newFakeServer(t)
	srv.routers = []contract.RouterRef{uiRouter}
	srv.logins[1] = contract.RouterLogin{Username: "root", Password: routerPassword}
	f := routertest.New(t, "root", routerPassword)
	h := newHarness(t, srv.srv.URL)
	h.routerRT = f.Transport()
	return h, srv, f
}

func TestServerRouterIsRead(t *testing.T) {
	h, srv, f := serverRouterHarness(t)
	if code := h.run("scan", "--once"); code != 0 {
		t.Fatalf("scan = %d\n%s", code, h.stderr.String())
	}
	run := lastUpload(t, srv)
	if len(run.Sources) != 1 || run.Sources[0].Outcome != contract.OutcomeOK || run.Sources[0].Address != "192.168.0.1" {
		t.Fatalf("sources = %+v", run.Sources)
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
	if srv.loginRequests != 1 {
		t.Errorf("login requests = %d, want 1 per scan", srv.loginRequests)
	}
	if f.SessionOpen() {
		t.Error("router session left open")
	}

	// The list is cached for scans while the server is down, without credentials.
	cache, err := os.ReadFile(filepath.Join(h.dir, "hne-collector.routers"))
	if err != nil {
		t.Fatalf("no router cache: %v", err)
	}
	if !strings.Contains(string(cache), "192.168.0.1") || strings.Contains(string(cache), routerPassword) || strings.Contains(string(cache), "password") {
		t.Errorf("router cache = %s", cache)
	}
}

func TestServerRouterWhileServerDown(t *testing.T) {
	h, srv, f := serverRouterHarness(t)
	h.run("scan", "--once") // fills the cache
	down := closedURL(t)
	writeConfig(t, h.dir, map[string]any{"server_url": down, "name": "desktop", "token": "tok", "subnets": []string{}, "interval_seconds": 900})
	requests := len(f.Requests())
	if code := h.run("scan", "--once"); code != 5 {
		t.Fatalf("scan with the server down = %d, want 5 (spooled)", code)
	}
	if len(f.Requests()) != requests {
		t.Error("the router was contacted without a login")
	}
	files, _ := filepath.Glob(filepath.Join(h.dir, "spool", "*.json"))
	if len(files) != 1 {
		t.Fatalf("spool = %v", files)
	}
	b, _ := os.ReadFile(files[0])
	run, err := contract.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Sources) != 1 || run.Sources[0].Outcome != contract.OutcomeLoginUnavailable {
		t.Errorf("sources with the server down = %+v, want login_unavailable from the cached list", run.Sources)
	}
	_ = srv
}

func TestServerRouterLoginNotFound(t *testing.T) {
	h, srv, _ := serverRouterHarness(t)
	delete(srv.logins, 1)
	h.run("scan", "--once")
	if run := lastUpload(t, srv); len(run.Sources) != 1 || run.Sources[0].Outcome != contract.OutcomeLoginUnavailable {
		t.Errorf("sources = %+v, want login_unavailable", run.Sources)
	}
}

func TestServerManagedConfigEntries(t *testing.T) {
	h, srv, _ := serverRouterHarness(t)
	h.withRouters(t, srv.srv.URL, map[string]any{"model": "huawei-hg8145v5", "url": "http://192.168.0.1"})
	if code := h.run("scan", "--once", "--dry-run"); code != 0 {
		t.Errorf("an entry without credentials must be accepted: exit %d\n%s", code, h.stderr.String())
	}
	h.withRouters(t, srv.srv.URL, map[string]any{"model": "huawei-hg8145v5", "url": "http://192.168.0.1", "username": "root"})
	if code := h.run("scan", "--once", "--dry-run"); code != 2 {
		t.Errorf("a username without a password: exit %d, want 2", code)
	}
}

// A config entry with its own credentials wins over the UI router at the same address (FR-013).
func TestConfigCredentialsWin(t *testing.T) {
	h, srv, f := serverRouterHarness(t)
	srv.logins[1] = contract.RouterLogin{Username: "root", Password: "server-password"}
	h.withRouters(t, srv.srv.URL, routerEntry()) // root / routerPassword, at 192.168.0.1
	h.run("scan", "--once")
	if srv.loginRequests != 0 {
		t.Errorf("login fetched %d times although the config has credentials", srv.loginRequests)
	}
	if c := f.Credentials(); len(c) != 1 || c[0] != "root:"+routerPassword {
		t.Errorf("router saw %v, want the config's credentials", c)
	}
	if run := lastUpload(t, srv); len(run.Sources) != 1 || run.Sources[0].Outcome != contract.OutcomeOK {
		t.Errorf("sources = %+v", run.Sources)
	}
}

func TestCheckShowsLoginSource(t *testing.T) {
	h, srv, _ := serverRouterHarness(t)
	if code := h.run("check"); code != 0 {
		t.Fatalf("check = %d\n%s", code, h.stderr.String())
	}
	if want := "huawei-hg8145v5 at 192.168.0.1 (login from server): OK, 13 online / 16 offline devices listed"; !strings.Contains(h.stderr.String(), want) {
		t.Errorf("check lacks %q:\n%s", want, h.stderr.String())
	}

	h.withRouters(t, srv.srv.URL, routerEntry())
	h.run("check")
	if want := "huawei-hg8145v5 at 192.168.0.1 (login from config): OK"; !strings.Contains(h.stderr.String(), want) {
		t.Errorf("check lacks %q:\n%s", want, h.stderr.String())
	}

	h.withRouters(t, srv.srv.URL)
	delete(srv.logins, 1)
	h.run("check")
	if want := "(login from server): login unavailable from the server"; !strings.Contains(h.stderr.String(), want) {
		t.Errorf("check lacks %q:\n%s", want, h.stderr.String())
	}
}
