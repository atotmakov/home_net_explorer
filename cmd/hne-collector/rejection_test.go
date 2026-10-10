package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/atotmakov/home_net_explorer/internal/collect/router/routertest"
	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// Feature 002, US2: no login retries after a rejection (FR-011), router results in check
// (FR-012), and the router never changes an exit code (FR-010).

const markerFile = "hne-collector.router-rejected"

var sha256Line = regexp.MustCompile(`^[0-9a-f]{64}$`)

func routerHarness(t *testing.T, mode routertest.Mode) (*harness, *fakeServer, *routertest.Fake) {
	t.Helper()
	srv := newFakeServer(t)
	f := routertest.New(t, "root", routerPassword)
	f.SetMode(mode)
	h := newHarness(t, srv.srv.URL)
	h.routerRT = f.Transport()
	h.withRouters(t, srv.srv.URL, routerEntry())
	return h, srv, f
}

func markerLines(t *testing.T, h *harness) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(h.dir, markerFile))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(b))
}

func TestRejectedLoginIsNotRetried(t *testing.T) {
	for _, mode := range []routertest.Mode{routertest.WrongPassword, routertest.Locked} {
		h, srv, f := routerHarness(t, mode)
		if code := h.run("scan", "--once"); code != 0 {
			t.Fatalf("mode %d: scan = %d\n%s", mode, code, h.stderr.String())
		}
		lines := markerLines(t, h)
		if len(lines) != 1 || !sha256Line.MatchString(lines[0]) {
			t.Fatalf("mode %d: marker = %q, want one SHA-256 line", mode, lines)
		}
		requests := len(f.Requests())

		if code := h.run("scan", "--once"); code != 0 {
			t.Errorf("mode %d: second scan = %d", mode, code)
		}
		if n := len(f.Requests()); n != requests {
			t.Errorf("mode %d: the router was contacted again after a rejection (%d requests)", mode, n-requests)
		}
		if run := lastUpload(t, srv); len(run.Sources) != 1 || run.Sources[0].Outcome != contract.OutcomeSkippedAfterRejection {
			t.Errorf("mode %d: sources = %+v, want skipped_after_rejection", mode, run.Sources)
		}

		// Editing the router config changes its hash: the next scan tries again.
		f.SetMode(routertest.OK)
		e := routerEntry()
		e["subnet"] = "192.168.0.0/24"
		h.withRouters(t, srv.srv.URL, e)
		h.run("scan", "--once")
		if f.Logins() != 2 {
			t.Errorf("mode %d: logins after a config change = %d, want 2", mode, f.Logins())
		}
		if run := lastUpload(t, srv); run.Sources[0].Outcome != contract.OutcomeOK {
			t.Errorf("mode %d: outcome after the fix = %s", mode, run.Sources[0].Outcome)
		}
	}
}

func TestTransientRouterFailuresAreRetried(t *testing.T) {
	for _, mode := range []routertest.Mode{routertest.SessionBusy, routertest.GarbledList} {
		h, _, f := routerHarness(t, mode)
		h.run("scan", "--once")
		h.run("scan", "--once")
		if f.Logins() != 2 || markerLines(t, h) != nil {
			t.Errorf("mode %d: logins = %d, marker = %v; transient failures are retried next scan", mode, f.Logins(), markerLines(t, h))
		}
	}
	h, _, _ := routerHarness(t, routertest.OK)
	h.routerRT = routertest.Redirect(closedURL(t))
	h.run("scan", "--once")
	if markerLines(t, h) != nil {
		t.Error("an unreachable router must not be marked as rejected")
	}
}

func TestCheckClearsRejectionAndTriesOnce(t *testing.T) {
	h, _, f := routerHarness(t, routertest.WrongPassword)
	h.run("scan", "--once")
	if markerLines(t, h) == nil {
		t.Fatal("no marker after a rejected login")
	}
	f.SetMode(routertest.OK)
	if code := h.run("check"); code != 0 {
		t.Fatalf("check = %d\n%s", code, h.stderr.String())
	}
	if f.Logins() != 2 {
		t.Errorf("check made %d login attempts, want exactly 1", f.Logins()-1)
	}
	if markerLines(t, h) != nil {
		t.Error("check must clear the rejection marker")
	}
	out := h.stderr.String()
	for _, want := range []string{
		"192.168.0.0/24     router huawei-hg8145v5 at 192.168.0.1",
		"Routers:",
		"huawei-hg8145v5 at 192.168.0.1 (login from config): OK, 13 online / 16 offline devices listed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("check output lacks %q:\n%s", want, out)
		}
	}
}

func TestCheckRouterResults(t *testing.T) {
	cases := []struct {
		mode routertest.Mode
		want string
	}{
		{routertest.WrongPassword, "login rejected (router skipped until its config changes or you run check)"},
		{routertest.Locked, "locked by the router"},
		{routertest.SessionBusy, "busy (someone is logged into the router)"},
		{routertest.GarbledList, "page not understood (firmware?)"},
	}
	for _, tc := range cases {
		h, _, _ := routerHarness(t, tc.mode)
		if code := h.run("check"); code != 0 {
			t.Errorf("mode %d: check = %d (a router failure never changes the exit code)", tc.mode, code)
		}
		if !strings.Contains(h.stderr.String(), "huawei-hg8145v5 at 192.168.0.1 (login from config): "+tc.want) {
			t.Errorf("mode %d: check output lacks %q:\n%s", tc.mode, tc.want, h.stderr.String())
		}
	}
	h, _, _ := routerHarness(t, routertest.WrongPassword)
	h.run("check")
	if markerLines(t, h) == nil {
		t.Error("a login rejected during check must be recorded too")
	}
	h, _, _ = routerHarness(t, routertest.OK)
	h.routerRT = routertest.Redirect(closedURL(t))
	h.run("check")
	if !strings.Contains(h.stderr.String(), "huawei-hg8145v5 at 192.168.0.1 (login from config): unreachable") {
		t.Errorf("unreachable router:\n%s", h.stderr.String())
	}
}

func TestCheckJSONRouterRecord(t *testing.T) {
	h, _, _ := routerHarness(t, routertest.OK)
	if code := h.run("check", "--json"); code != 0 {
		t.Fatalf("check --json = %d", code)
	}
	found := false
	sc := bufio.NewScanner(strings.NewReader(h.stdout.String()))
	for sc.Scan() {
		var m map[string]any
		if json.Unmarshal(sc.Bytes(), &m) != nil || m["msg"] != "router read" {
			continue
		}
		found = true
		want := map[string]any{"model": "huawei-hg8145v5", "address": "192.168.0.1", "subnet": "192.168.0.0/24",
			"outcome": "ok", "online": float64(13), "offline": float64(16)}
		for k, v := range want {
			if m[k] != v {
				t.Errorf("router read %s = %v, want %v", k, m[k], v)
			}
		}
		for _, k := range []string{"username", "password", "url"} {
			if _, ok := m[k]; ok {
				t.Errorf("router read carries %q", k)
			}
		}
	}
	if !found {
		t.Errorf("check --json printed no router read record:\n%s", h.stdout.String())
	}
}

// Feature 003 (FR-014): a UI router's rejected login is recorded with the login fetched from the
// server; the next scan fetches the login again but doesn't contact the router until the login
// changes on the server (no check needed).
func TestServerRouterRejectionMarker(t *testing.T) {
	h, srv, f := serverRouterHarness(t)
	f.SetMode(routertest.WrongPassword)
	h.run("scan", "--once")
	if run := lastUpload(t, srv); run.Sources[0].Outcome != contract.OutcomeLoginRejected {
		t.Fatalf("first scan = %+v", run.Sources)
	}
	lines := markerLines(t, h)
	if len(lines) != 1 || !sha256Line.MatchString(lines[0]) {
		t.Fatalf("marker = %q", lines)
	}
	requests := len(f.Requests())

	h.run("scan", "--once")
	if srv.loginRequests != 2 {
		t.Errorf("login fetches = %d, want 2 (one per scan)", srv.loginRequests)
	}
	if len(f.Requests()) != requests {
		t.Error("the router was contacted again with the rejected login")
	}
	if run := lastUpload(t, srv); run.Sources[0].Outcome != contract.OutcomeSkippedAfterRejection {
		t.Errorf("second scan = %+v", run.Sources)
	}

	srv.logins[1] = contract.RouterLogin{Username: "root", Password: "pw-fixed-in-ui"}
	h.run("scan", "--once")
	if len(f.Requests()) == requests {
		t.Error("a new password in the UI must re-enable the router without check")
	}

	srv.logins[1] = contract.RouterLogin{Username: "root", Password: routerPassword}
	f.SetMode(routertest.OK)
	h.run("scan", "--once") // rejected again above with pw-fixed-in-ui; now right
	if run := lastUpload(t, srv); run.Sources[0].Outcome != contract.OutcomeOK {
		t.Errorf("after the right password = %+v", run.Sources)
	}
	h.run("check")
	if markerLines(t, h) != nil {
		t.Error("check must clear the marker")
	}
}
