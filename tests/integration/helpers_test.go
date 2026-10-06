package integration_test

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/app"
	"github.com/atotmakov/home_net_explorer/internal/clock"
	"github.com/atotmakov/home_net_explorer/internal/collect"
	"github.com/atotmakov/home_net_explorer/internal/collect/collecttest"
	"github.com/atotmakov/home_net_explorer/internal/contract"
)

var t0 = time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)

const password = "correct horse battery"

type env struct {
	t     *testing.T
	app   *app.App
	srv   *httptest.Server
	http  *http.Client
	clock *clock.Fake
	net   *collecttest.FakeNetwork
}

type envOpts struct {
	builtinScan bool // run the built-in scan loop
}

// newEnv starts the full server over a temp data dir with a fake network behind the
// built-in collector's engine.
func newEnv(t *testing.T, o envOpts) *env {
	t.Helper()
	clk := clock.NewFake(t0)
	fnet := collecttest.NewFakeNetwork().
		Add("192.168.1.100", collecttest.Host{MAC: "a0:b1:c2:d3:e4:f5", Hostname: "router.lan"}).
		Add("192.168.1.20", collecttest.Host{MAC: "00:11:32:aa:bb:cc", Hostname: "nas.lan"}).
		Add("192.168.1.50", collecttest.Host{MAC: "da:a1:19:01:02:03"})
	engine := &collect.Engine{
		Prober: fnet, Presence: fnet, Neighbors: fnet, Resolver: fnet,
		Routes:    collecttest.NewFakeRoutes("192.168.1.100", "eth0=192.168.1.20/24"),
		Clock:     clk,
		Collector: contract.CollectorInfo{Name: "nas", Version: "test", OS: "linux"},
	}
	a, err := app.New(context.Background(), app.Options{
		DataDir:       filepath.Join(t.TempDir(), "data"),
		Clock:         clk,
		ScanInterval:  15 * time.Minute,
		NoBuiltinScan: !o.builtinScan,
		Engine:        engine,
		Version:       "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.Start(ctx)
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(func() {
		srv.Close()
		cancel()
		a.Close()
	})
	jar, _ := cookiejar.New(nil)
	e := &env{t: t, app: a, srv: srv, clock: clk, net: fnet,
		http: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	return e
}

func (e *env) req(method, path string, form url.Values) (*http.Response, string) {
	e.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	r, err := http.NewRequest(method, e.srv.URL+path, body)
	if err != nil {
		e.t.Fatal(err)
	}
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", e.srv.URL)
	}
	res, err := e.http.Do(r)
	if err != nil {
		e.t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	return res, string(b)
}

func (e *env) login() {
	e.t.Helper()
	res, _ := e.req("POST", "/setup", url.Values{"password": {password}, "confirm": {password}})
	if res.StatusCode != http.StatusSeeOther {
		e.t.Fatalf("setup: %d", res.StatusCode)
	}
}

func (e *env) get(path string) string {
	e.t.Helper()
	res, body := e.req("GET", path, nil)
	if res.StatusCode != http.StatusOK {
		e.t.Fatalf("GET %s = %d\n%s", path, res.StatusCode, body)
	}
	return body
}

// waitFor polls cond until it is true or 5 seconds pass.
func (e *env) waitFor(what string, cond func() bool) {
	e.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	e.t.Fatalf("timed out waiting for %s", what)
}

func (e *env) runCount() int {
	var n int
	if err := e.app.Store.DB().QueryRow(`SELECT count(*) FROM collection_runs`).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}
