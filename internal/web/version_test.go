package web_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/clock"
	"github.com/atotmakov/home_net_explorer/internal/store/storetest"
	"github.com/atotmakov/home_net_explorer/internal/web"
)

func versionServer(t *testing.T) http.Handler {
	t.Helper()
	s := storetest.New(t)
	clk := clock.NewFake(time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC))
	srv, err := web.New(web.Options{Clock: clk, Store: s,
		Version: "0.2.57", Commit: "abc1234"})
	if err != nil {
		t.Fatal(err)
	}
	return srv.Handler()
}

func get(t *testing.T, h http.Handler, path string) (*http.Response, string) {
	t.Helper()
	req := httptest.NewRequest("GET", "http://"+host+path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	b, _ := io.ReadAll(rec.Result().Body)
	return rec.Result(), string(b)
}

func TestFooterShowsVersionOnEveryPage(t *testing.T) {
	h := versionServer(t)
	for _, p := range []string{"/", "/devices", "/settings"} {
		if _, body := get(t, h, p); !strings.Contains(body, "0.2.57") || !strings.Contains(body, `title="commit abc1234"`) {
			t.Errorf("%s footer lacks the version/commit", p)
		}
	}
}

func TestVersionEndpoint(t *testing.T) {
	h := versionServer(t)
	res, body := get(t, h, "/version") // public, like /healthz
	if res.StatusCode != http.StatusOK {
		t.Fatalf("/version = %d", res.StatusCode)
	}
	var v struct{ Version, Commit string }
	if err := json.Unmarshal([]byte(body), &v); err != nil || v.Version != "0.2.57" || v.Commit != "abc1234" {
		t.Errorf("/version = %s (%v)", body, err)
	}
}
