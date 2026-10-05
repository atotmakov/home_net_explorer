package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/auth"
	"github.com/atotmakov/home_net_explorer/internal/clock"
	"github.com/atotmakov/home_net_explorer/internal/store/storetest"
	"github.com/atotmakov/home_net_explorer/internal/web"
)

const host = "nas.local:8080"

type client struct {
	t      *testing.T
	h      http.Handler
	cookie *http.Cookie
}

func newClient(t *testing.T) *client {
	t.Helper()
	s := storetest.New(t)
	clk := clock.NewFake(time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC))
	srv, err := web.New(web.Options{Owner: auth.NewOwner(s.DB(), clk), Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	return &client{t: t, h: srv.Handler()}
}

func (c *client) do(method, path string, form url.Values, origin string) *http.Response {
	c.t.Helper()
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequest(method, "http://"+host+path, body)
	req.Host = host
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	res := rec.Result()
	for _, ck := range res.Cookies() {
		if ck.Name == web.SessionCookie {
			c.cookie = ck
		}
	}
	return res
}

func expectRedirect(t *testing.T, res *http.Response, to string) {
	t.Helper()
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != to {
		t.Errorf("got %d Location=%q, want 303 -> %s", res.StatusCode, res.Header.Get("Location"), to)
	}
}

func TestFirstRunRequiresSetup(t *testing.T) {
	c := newClient(t)
	expectRedirect(t, c.do("GET", "/", nil, ""), "/setup")
	expectRedirect(t, c.do("GET", "/devices", nil, ""), "/setup")
	expectRedirect(t, c.do("GET", "/login", nil, ""), "/setup")
	if res := c.do("GET", "/setup", nil, ""); res.StatusCode != http.StatusOK {
		t.Errorf("GET /setup = %d", res.StatusCode)
	}
}

func TestSetupThenLogin(t *testing.T) {
	c := newClient(t)
	origin := "http://" + host

	res := c.do("POST", "/setup", url.Values{"password": {"correct horse"}, "confirm": {"correct horse"}}, origin)
	expectRedirect(t, res, "/")
	if c.cookie == nil {
		t.Fatal("setup did not start a session")
	}
	if !c.cookie.HttpOnly || c.cookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("session cookie must be HttpOnly + SameSite=Strict: %+v", c.cookie)
	}
	if res := c.do("GET", "/", nil, ""); res.StatusCode != http.StatusOK {
		t.Errorf("GET / with session = %d", res.StatusCode)
	}
	if res := c.do("GET", "/setup", nil, ""); res.StatusCode != http.StatusNotFound {
		t.Errorf("GET /setup after setup = %d, want 404", res.StatusCode)
	}

	c.do("POST", "/logout", url.Values{}, origin)
	c.cookie = nil
	expectRedirect(t, c.do("GET", "/", nil, ""), "/login")

	if res := c.do("POST", "/login", url.Values{"password": {"wrong horse"}}, origin); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong password = %d, want 401", res.StatusCode)
	}
	if c.cookie != nil {
		t.Error("wrong password started a session")
	}
	expectRedirect(t, c.do("POST", "/login", url.Values{"password": {"correct horse"}}, origin), "/")
	if res := c.do("GET", "/", nil, ""); res.StatusCode != http.StatusOK {
		t.Errorf("GET / after login = %d", res.StatusCode)
	}
}

func TestSetupRejectsMismatchAndShortPassword(t *testing.T) {
	c := newClient(t)
	origin := "http://" + host
	if res := c.do("POST", "/setup", url.Values{"password": {"correct horse"}, "confirm": {"different"}}, origin); res.StatusCode != http.StatusBadRequest {
		t.Errorf("mismatched confirm = %d, want 400", res.StatusCode)
	}
	if res := c.do("POST", "/setup", url.Values{"password": {"short"}, "confirm": {"short"}}, origin); res.StatusCode != http.StatusBadRequest {
		t.Errorf("short password = %d, want 400", res.StatusCode)
	}
}

func TestLoginRateLimited(t *testing.T) {
	c := newClient(t)
	origin := "http://" + host
	c.do("POST", "/setup", url.Values{"password": {"correct horse"}, "confirm": {"correct horse"}}, origin)
	c.cookie = nil
	for i := 0; i < 5; i++ {
		c.do("POST", "/login", url.Values{"password": {"nope nope nope"}}, origin)
	}
	if res := c.do("POST", "/login", url.Values{"password": {"correct horse"}}, origin); res.StatusCode != http.StatusTooManyRequests {
		t.Errorf("6th login attempt within a minute = %d, want 429", res.StatusCode)
	}
}

func TestCrossOriginPostRejected(t *testing.T) {
	c := newClient(t)
	res := c.do("POST", "/setup", url.Values{"password": {"correct horse"}, "confirm": {"correct horse"}}, "http://evil.example")
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("cross-origin POST = %d, want 403", res.StatusCode)
	}
}

func TestPublicEndpoints(t *testing.T) {
	c := newClient(t)
	res := c.do("GET", "/healthz", nil, "")
	if res.StatusCode != http.StatusOK {
		t.Errorf("/healthz = %d", res.StatusCode)
	}
	if res := c.do("GET", "/api/v1/ping", nil, ""); res.StatusCode == http.StatusSeeOther {
		t.Error("/api/v1/* must not redirect to the login flow (it uses bearer auth)")
	}
	if res := c.do("GET", "/static/vendor/htmx.min.js", nil, ""); res.StatusCode != http.StatusOK {
		t.Errorf("static asset = %d", res.StatusCode)
	}
}
