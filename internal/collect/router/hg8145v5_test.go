package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/collect/router/routertest"
	"github.com/atotmakov/home_net_explorer/internal/contract"
)

func newSource(t *testing.T, rt http.RoundTripper, pass string) *hg8145v5 {
	t.Helper()
	cfg := validConfig()
	cfg.Password = Secret(pass)
	src, err := New(cfg, rt)
	if err != nil {
		t.Fatal(err)
	}
	h := src.(*hg8145v5)
	h.timeout = 2 * time.Second
	return h
}

var fullRead = []string{
	"POST /asp/GetRandCount.asp",
	"POST /login.cgi",
	"GET /html/bbsp/common/GetLanUserDevInfo.asp",
	"GET /index.asp",
	"POST /logout.cgi",
}

func TestReadOK(t *testing.T) {
	f := routertest.New(t, "root", testPassword)
	h := newSource(t, f.Transport(), testPassword)
	res := h.Read(context.Background())
	if res.Outcome != contract.OutcomeOK || res.Online != 13 || res.Offline != 16 || len(res.Observations) != 13 {
		t.Fatalf("read = %s, %d online / %d offline, %d observations; want ok 13 / 16 / 13",
			res.Outcome, res.Online, res.Offline, len(res.Observations))
	}
	for _, o := range res.Observations {
		if o.Method != contract.ObsRouterTable || !contract.ValidMAC(o.MAC) {
			t.Errorf("observation = %+v", o)
		}
	}
	if got := f.Requests(); !slices.Equal(got, fullRead) {
		t.Errorf("requests = %q\nwant %q", got, fullRead)
	}
	if f.Logouts() != 1 || f.SessionOpen() {
		t.Errorf("logouts = %d, session open = %v; the collector must log out (FR-004, SC-006)", f.Logouts(), f.SessionOpen())
	}
	if c := f.Credentials(); len(c) != 1 || c[0] != "root:"+testPassword {
		t.Errorf("login attempts = %d; exactly one with the configured credentials (FR-003)", len(c))
	}
	// The session was released, so the next read logs in again (the router allows one session).
	if res := h.Read(context.Background()); res.Outcome != contract.OutcomeOK {
		t.Errorf("second read = %s", res.Outcome)
	}
}

func TestReadLogsOutAfterGarbledList(t *testing.T) {
	f := routertest.New(t, "root", testPassword)
	f.SetMode(routertest.GarbledList)
	res := newSource(t, f.Transport(), testPassword).Read(context.Background())
	if res.Outcome != contract.OutcomePageNotUnderstood || len(res.Observations) != 0 || res.Online != 0 || res.Offline != 0 {
		t.Errorf("garbled list = %+v", res)
	}
	if f.Logouts() != 1 || f.SessionOpen() {
		t.Errorf("no logout after a garbled list (logouts %d)", f.Logouts())
	}
}

// cancelAfterLogin cancels the read's context as soon as the login response arrives.
type cancelAfterLogin struct {
	rt     http.RoundTripper
	cancel context.CancelFunc
}

func (c *cancelAfterLogin) RoundTrip(r *http.Request) (*http.Response, error) {
	res, err := c.rt.RoundTrip(r)
	if r.URL.Path == "/login.cgi" {
		c.cancel()
	}
	return res, err
}

func TestReadLogsOutAfterCancel(t *testing.T) {
	f := routertest.New(t, "root", testPassword)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	res := newSource(t, &cancelAfterLogin{rt: f.Transport(), cancel: cancel}, testPassword).Read(ctx)
	if res.Outcome == contract.OutcomeOK {
		t.Errorf("cancelled read reported ok")
	}
	if f.Logouts() != 1 || f.SessionOpen() {
		t.Errorf("a cancelled read must still log out (logouts %d)", f.Logouts())
	}
}

func TestReadFailures(t *testing.T) {
	cases := []struct {
		label string
		mode  routertest.Mode
		pass  string
		want  string
	}{
		{"wrong password", routertest.WrongPassword, testPassword, contract.OutcomeLoginRejected},
		{"credentials differ", routertest.OK, "not-the-password", contract.OutcomeLoginRejected},
		{"locked", routertest.Locked, testPassword, contract.OutcomeLocked},
		{"session busy", routertest.SessionBusy, testPassword, contract.OutcomeSessionBusy},
	}
	for _, tc := range cases {
		f := routertest.New(t, "root", testPassword)
		f.SetMode(tc.mode)
		res := newSource(t, f.Transport(), tc.pass).Read(context.Background())
		if res.Outcome != tc.want || len(res.Observations) != 0 || res.Online != 0 || res.Offline != 0 {
			t.Errorf("%s: %+v, want %s with no data", tc.label, res, tc.want)
		}
		if f.Logins() != 1 {
			t.Errorf("%s: %d login attempts, want exactly 1 (no retries, FR-011)", tc.label, f.Logins())
		}
		for _, r := range f.Requests() {
			if r == "GET /html/bbsp/common/GetLanUserDevInfo.asp" {
				t.Errorf("%s: read the list without a session", tc.label)
			}
		}
	}
}

func TestReadNoOntToken(t *testing.T) {
	f := routertest.New(t, "root", testPassword)
	f.SetMode(routertest.NoOntToken)
	res := newSource(t, f.Transport(), testPassword).Read(context.Background())
	if res.Outcome != contract.OutcomeOK || len(res.Observations) != 13 {
		t.Errorf("read without onttoken = %s / %d", res.Outcome, len(res.Observations))
	}
	if slices.Contains(f.Requests(), "POST /logout.cgi") {
		t.Error("logout sent without a token (the router expires the session itself)")
	}
}

func TestReadUnreachable(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	res := newSource(t, routertest.Redirect(closed.URL), testPassword).Read(context.Background())
	if res.Outcome != contract.OutcomeUnreachable {
		t.Errorf("closed port = %s, want unreachable", res.Outcome)
	}

	f := routertest.New(t, "root", testPassword)
	f.SetMode(routertest.Hang)
	h := newSource(t, f.Transport(), testPassword)
	h.timeout = 200 * time.Millisecond
	start := time.Now()
	res = h.Read(context.Background())
	if res.Outcome != contract.OutcomeUnreachable {
		t.Errorf("hanging router = %s, want unreachable", res.Outcome)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("hanging router held the read for %v; the timeout is 200ms", d)
	}
}
