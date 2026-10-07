package web_test

import (
	"encoding/json"
	"io"
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

func versionServer(t *testing.T) http.Handler {
	t.Helper()
	s := storetest.New(t)
	clk := clock.NewFake(time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC))
	srv, err := web.New(web.Options{Owner: auth.NewOwner(s.DB(), clk), Clock: clk, Store: s,
		Version: "0.2.57", Commit: "abc1234"})
	if err != nil {
		t.Fatal(err)
	}
	return srv.Handler()
}

func get(t *testing.T, h http.Handler, path string, cookie *http.Cookie) (*http.Response, string) {
	t.Helper()
	req := httptest.NewRequest("GET", "http://"+host+path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	b, _ := io.ReadAll(rec.Result().Body)
	return rec.Result(), string(b)
}

func TestFooterShowsVersionOnEveryPage(t *testing.T) {
	h := versionServer(t)
	_, body := get(t, h, "/setup", nil)
	if !strings.Contains(body, "0.2.57") || !strings.Contains(body, `title="commit abc1234"`) {
		t.Errorf("setup page footer lacks the version/commit:\n%s", body)
	}

	// After setup, a logged-in page shows it too.
	form := url.Values{"password": {"correct horse"}, "confirm": {"correct horse"}}
	req := httptest.NewRequest("POST", "http://"+host+"/setup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://"+host)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var session *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == web.SessionCookie {
			session = c
		}
	}
	if session == nil {
		t.Fatal("no session after setup")
	}
	if _, body := get(t, h, "/", session); !strings.Contains(body, "0.2.57") {
		t.Error("home page footer lacks the version")
	}
}

func TestVersionEndpoint(t *testing.T) {
	h := versionServer(t)
	res, body := get(t, h, "/version", nil) // public, like /healthz
	if res.StatusCode != http.StatusOK {
		t.Fatalf("/version = %d", res.StatusCode)
	}
	var v struct{ Version, Commit string }
	if err := json.Unmarshal([]byte(body), &v); err != nil || v.Version != "0.2.57" || v.Commit != "abc1234" {
		t.Errorf("/version = %s (%v)", body, err)
	}
}
