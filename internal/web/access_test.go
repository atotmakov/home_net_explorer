package web_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/clock"
	"github.com/atotmakov/home_net_explorer/internal/store/storetest"
	"github.com/atotmakov/home_net_explorer/internal/web"
)

// Feature 007: the web UI has no login; it is open to the home network. Cross-site requests
// are still refused, and collectors still need their bearer tokens.

const host = "nas.local:8080"

func newHandler(t *testing.T) http.Handler {
	t.Helper()
	s := storetest.New(t)
	clk := clock.NewFake(time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC))
	srv, err := web.New(web.Options{Store: s, Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	return srv.Handler()
}

func do(t *testing.T, h http.Handler, method, path string, form url.Values, origin string) (*http.Response, string) {
	t.Helper()
	body := ""
	if form != nil {
		body = form.Encode()
	}
	req := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
	req.Host = host
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	res := rec.Result()
	b, _ := io.ReadAll(res.Body)
	return res, string(b)
}

func TestPagesOpenWithoutLogin(t *testing.T) {
	h := newHandler(t)
	for _, p := range []string{"/", "/devices", "/settings", "/collectors"} {
		res, body := do(t, h, "GET", p, nil, "")
		if res.StatusCode != http.StatusOK {
			t.Errorf("GET %s = %d (Location %q), want 200 without logging in", p, res.StatusCode, res.Header.Get("Location"))
		}
		if strings.Contains(body, "Log out") {
			t.Errorf("GET %s still offers Log out", p)
		}
		if len(res.Cookies()) != 0 {
			t.Errorf("GET %s set cookies %v", p, res.Cookies())
		}
	}
}

// Old bookmarks of the login and setup pages lead home.
func TestOldLoginPagesRedirectHome(t *testing.T) {
	h := newHandler(t)
	for _, p := range []string{"/login", "/setup"} {
		res, _ := do(t, h, "GET", p, nil, "")
		if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/" {
			t.Errorf("GET %s = %d Location=%q, want 303 -> /", p, res.StatusCode, res.Header.Get("Location"))
		}
	}
}

func TestCrossOriginPostRejected(t *testing.T) {
	h := newHandler(t)
	res, _ := do(t, h, "POST", "/settings", url.Values{"builtin_interval_minutes": {"5"}}, "http://evil.example")
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("cross-origin POST = %d, want 403", res.StatusCode)
	}
}

func TestPublicEndpoints(t *testing.T) {
	h := newHandler(t)
	if res, _ := do(t, h, "GET", "/healthz", nil, ""); res.StatusCode != http.StatusOK {
		t.Errorf("/healthz = %d", res.StatusCode)
	}
	if res, _ := do(t, h, "GET", "/api/v1/ping", nil, ""); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("/api/v1/ping without a token = %d, want 401 (collectors still need their token)", res.StatusCode)
	}
	if res, _ := do(t, h, "GET", "/static/vendor/htmx.min.js", nil, ""); res.StatusCode != http.StatusOK {
		t.Errorf("static asset = %d", res.StatusCode)
	}
}
