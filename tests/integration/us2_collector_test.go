package integration_test

import (
	"bytes"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/contract/contracttest"
)

type collectorConfig struct {
	ServerURL       string           `json:"server_url"`
	Name            string           `json:"name"`
	Token           string           `json:"token"`
	Subnets         []string         `json:"subnets"`
	IntervalSeconds int              `json:"interval_seconds"`
	Routers         []map[string]any `json:"routers"` // feature 003: UI routers, no credentials
}

var configRe = regexp.MustCompile(`(?s)<pre id="collector-config">(.*?)</pre>`)

func (e *env) createCollector(name string) collectorConfig {
	e.t.Helper()
	res, body := e.req("POST", "/collectors", url.Values{"name": {name}})
	if res.StatusCode != http.StatusOK {
		e.t.Fatalf("POST /collectors = %d\n%s", res.StatusCode, body)
	}
	m := configRe.FindStringSubmatch(body)
	if m == nil {
		e.t.Fatalf("no hne-collector.json shown after creating the collector:\n%s", body)
	}
	var cfg collectorConfig
	if err := json.Unmarshal([]byte(html.UnescapeString(m[1])), &cfg); err != nil {
		e.t.Fatalf("collector config is not JSON: %v\n%s", err, m[1])
	}
	if !strings.Contains(body, `download="hne-collector.json"`) {
		e.t.Error("no hne-collector.json download link")
	}
	return cfg
}

func (e *env) upload(token string, body []byte) (int, contract.UploadResult) {
	e.t.Helper()
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/v1/collections", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	var r contract.UploadResult
	b, _ := io.ReadAll(res.Body)
	json.Unmarshal(b, &r)
	return res.StatusCode, r
}

func withID(t *testing.T, fixture, id string, f func(m map[string]any)) []byte {
	return contracttest.Modify(t, fixture, func(m map[string]any) {
		m["collection_id"] = id
		if f != nil {
			f(m)
		}
	})
}

// User Story 2: a remote collector uploads what the NAS cannot see (quickstart.md §4).
func TestUS2RemoteCollector(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.login()

	cfg := e.createCollector("desktop")
	if cfg.Name != "desktop" || cfg.Token == "" || cfg.ServerURL != e.srv.URL || cfg.IntervalSeconds != 900 || cfg.Subnets == nil {
		t.Errorf("collector config = %+v", cfg)
	}
	if page := e.get("/collectors"); strings.Contains(page, cfg.Token) {
		t.Error("the token must be shown only once, right after creation")
	}

	// Collector clock in sync with the server (valid_full.json was sent at 10:06:31).
	e.clock.Set(time.Date(2026, 10, 5, 10, 6, 32, 0, time.UTC))
	if status, _ := e.upload(cfg.Token, contracttest.Fixture(t, "valid_full.json")); status != http.StatusCreated {
		t.Fatalf("upload = %d", status)
	}
	if body := e.get("/devices"); !strings.Contains(body, "192.168.8.23") || !strings.Contains(body, "printer.local") {
		t.Error("devices from the desktop-only subnet are not listed")
	}
	page := e.get("/collectors")
	if !strings.Contains(page, "desktop") || !strings.Contains(page, "192.168.8.0/24") {
		t.Error("/collectors does not show the collector and its subnets")
	}
	if strings.Contains(page, "clock skew") {
		t.Error("collector flagged although its clock is in sync")
	}

	// A sent_at 6 minutes ahead of the server's clock flags the collector ("exceeds 5 minutes").
	skewed := withID(t, "valid_full.json", "7c9e6679-7425-40de-944b-e07fc1f90ae8", func(m map[string]any) {
		m["sent_at"] = "2026-10-05T10:12:32.000Z"
	})
	if status, r := e.upload(cfg.Token, skewed); status != http.StatusCreated || r.ClockSkewMs != 360000 {
		t.Fatalf("skewed upload = %d skew=%d", status, r.ClockSkewMs)
	}
	if page := e.get("/collectors"); !strings.Contains(page, "clock skew") {
		t.Error("collector with 6 minutes of skew is not flagged")
	}

	// New subnet (SC-010): tracked automatically, devices shown, owner notified.
	status, r := e.upload(cfg.Token, contracttest.Fixture(t, "valid_new_subnet.json"))
	if status != http.StatusCreated || len(r.NewSubnets) != 1 || r.NewSubnets[0] != "10.20.30.0/24" {
		t.Fatalf("new subnet upload = %d new_subnets=%v (the skipped /16 must not be listed)", status, r.NewSubnets)
	}
	if body := e.get("/devices"); !strings.Contains(body, "10.20.30.44") {
		t.Error("devices of the new subnet are not listed")
	}
	if home := e.get("/"); !strings.Contains(home, "New subnet") || !strings.Contains(home, "10.20.30.0/24") {
		t.Error("home page shows no new-subnet notice")
	}
	var n int
	e.app.Store.DB().QueryRow(`SELECT count(*) FROM subnets WHERE cidr = '10.0.0.0/16'`).Scan(&n)
	if n != 0 {
		t.Error("the skipped /16 must not become a subnet record")
	}
	if settings := e.get("/settings"); !strings.Contains(settings, "10.0.0.0/16") {
		t.Error("/settings does not list the skipped (too large) subnet")
	}

	// Ignoring a subnet: collectors learn it from ping; later observations are stored but not shown.
	if res, _ := e.req("POST", "/subnets", url.Values{"cidr": {"10.20.30.0/24"}, "ignored": {"true"}}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("ignore subnet = %d", res.StatusCode)
	}
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/v1/ping", nil)
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var ping contract.PingResponse
	json.NewDecoder(res.Body).Decode(&ping)
	res.Body.Close()
	if len(ping.IgnoredSubnets) != 1 || ping.IgnoredSubnets[0] != "10.20.30.0/24" {
		t.Errorf("ping ignored_subnets = %v", ping.IgnoredSubnets)
	}
	later := withID(t, "valid_new_subnet.json", "9b2d3c4e-1f20-4a5b-8c6d-7e8f90a1b2c4", func(m map[string]any) {
		obs := m["observations"].([]any)
		obs[1].(map[string]any)["ip"] = "10.20.30.99"
		obs[1].(map[string]any)["mac"] = "52:54:00:12:34:99"
	})
	if status, _ := e.upload(cfg.Token, later); status != http.StatusCreated {
		t.Fatalf("upload after ignore = %d", status)
	}
	e.app.Store.DB().QueryRow(`SELECT count(*) FROM collection_runs WHERE collection_id = '9b2d3c4e-1f20-4a5b-8c6d-7e8f90a1b2c4'`).Scan(&n)
	if n != 1 {
		t.Error("run for an ignored subnet must still be stored")
	}
	if body := e.get("/devices"); strings.Contains(body, "10.20.30.99") || strings.Contains(body, "10.20.30.44") {
		t.Error("devices of an ignored subnet are still listed")
	}

	// Revoking the token stops uploads.
	var id string
	e.app.Store.DB().QueryRow(`SELECT CAST(id AS TEXT) FROM collectors WHERE name = 'desktop'`).Scan(&id)
	if res, _ := e.req("POST", "/collectors/"+id+"/revoke", url.Values{}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("revoke = %d", res.StatusCode)
	}
	if status, _ := e.upload(cfg.Token, withID(t, "valid_minimal.json", "3f2b8c1e-5d4a-4c3b-9a1f-0e2d4c6b8a99", nil)); status != http.StatusUnauthorized {
		t.Errorf("upload after revoke = %d, want 401", status)
	}
}

func TestCollectorNameRules(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.login()
	for _, name := range []string{"Desktop PC", "", "nas"} {
		if res, _ := e.req("POST", "/collectors", url.Values{"name": {name}}); res.StatusCode != http.StatusBadRequest {
			t.Errorf("collector name %q accepted (%d)", name, res.StatusCode)
		}
	}
}

func TestCollectorDownloads(t *testing.T) {
	e := newEnv(t, envOpts{downloads: map[string]string{"hne-collector-windows-amd64.exe": "MZ fake binary"}})
	e.login()
	res, body := e.req("GET", "/downloads/hne-collector-windows-amd64.exe", nil)
	if res.StatusCode != http.StatusOK || body != "MZ fake binary" {
		t.Errorf("download = %d %q", res.StatusCode, body)
	}
	if res, _ := e.req("GET", "/downloads/hne-collector-linux-arm64", nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("missing binary = %d, want 404", res.StatusCode)
	}
	if res, _ := e.req("GET", "/downloads/..%2fhne.db", nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("path traversal = %d, want 404", res.StatusCode)
	}
}
