package main

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/clock"
	"github.com/atotmakov/home_net_explorer/internal/collect"
	"github.com/atotmakov/home_net_explorer/internal/collect/collecttest"
	"github.com/atotmakov/home_net_explorer/internal/contract"
)

type fakeServer struct {
	srv          *httptest.Server
	pingStatus   int
	uploadStatus int
	uploads      int
}

func newFakeServer(t *testing.T) *fakeServer {
	f := &fakeServer{pingStatus: 200, uploadStatus: 201}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(contract.ErrorResponse{Error: contract.CodeInvalidToken})
			return
		}
		switch r.URL.Path {
		case "/api/v1/ping":
			w.WriteHeader(f.pingStatus)
			json.NewEncoder(w).Encode(contract.PingResponse{Collector: "desktop", ServerTime: time.Now(),
				SupportedSchemaVersions: []int{1}, IgnoredSubnets: []string{}})
		case "/api/v1/collections":
			f.uploads++
			w.WriteHeader(f.uploadStatus)
			json.NewEncoder(w).Encode(contract.UploadResult{Status: contract.StatusStored})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func writeConfig(t *testing.T, dir string, c map[string]any) {
	t.Helper()
	b, _ := json.Marshal(c)
	if err := os.WriteFile(filepath.Join(dir, "hne-collector.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

type harness struct {
	dir            string
	env            map[string]string
	stdout, stderr bytes.Buffer
}

func newHarness(t *testing.T, serverURL string) *harness {
	h := &harness{dir: t.TempDir(), env: map[string]string{}}
	writeConfig(t, h.dir, map[string]any{"server_url": serverURL, "name": "desktop", "token": "tok", "subnets": []string{}, "interval_seconds": 900})
	return h
}

func (h *harness) run(args ...string) int {
	h.stdout.Reset()
	h.stderr.Reset()
	fnet := collecttest.NewFakeNetwork().Add("192.168.1.100", collecttest.Host{MAC: "a0:b1:c2:d3:e4:f5", Hostname: "router.lan"})
	return run(args, environment{
		dir:    h.dir,
		getenv: func(k string) string { return h.env[k] },
		stdout: &h.stdout,
		stderr: &h.stderr,
		newEngine: func(cfg config) (*collect.Engine, func() error, error) {
			return &collect.Engine{
				Prober: fnet, Presence: fnet, Neighbors: fnet, Resolver: fnet,
				Routes:    collecttest.NewFakeRoutes("192.168.1.100", "home=192.168.1.10/24"),
				Clock:     clock.Real{},
				Collector: contract.CollectorInfo{Name: cfg.Name, Version: "test", OS: "windows"},
			}, func() error { return nil }, nil
		},
	})
}

func (h *harness) spooled(t *testing.T) int {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(h.dir, "spool", "*.json"))
	return len(m)
}

func closedURL(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return "http://" + addr
}

func TestConfigLoading(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, map[string]any{"server_url": "http://192.168.1.100:8181", "name": "desktop", "token": "file-token", "subnets": []string{}})
	cfg, err := loadConfig(dir, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerURL != "http://192.168.1.100:8181" || cfg.Token != "file-token" || len(cfg.Subnets) != 0 || cfg.IntervalSeconds != 900 {
		t.Errorf("cfg = %+v (empty subnets = auto-discovery; default interval 900)", cfg)
	}
	env := map[string]string{"HNE_SERVER_URL": "http://10.0.0.2:8080", "HNE_TOKEN": "env-token", "HNE_SUBNETS": "192.168.1.0/24, 10.20.30.0/24"}
	cfg, err = loadConfig(dir, func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerURL != "http://10.0.0.2:8080" || cfg.Token != "env-token" || len(cfg.Subnets) != 2 {
		t.Errorf("environment did not override the file: %+v", cfg)
	}
	env = map[string]string{"HNE_SUBNETS": "8.8.8.0/24"}
	if _, err := loadConfig(dir, func(k string) string { return env[k] }); err == nil {
		t.Error("a non-private subnet must be a config error")
	}
	if _, err := loadConfig(t.TempDir(), func(string) string { return "" }); err == nil {
		t.Error("missing hne-collector.json must be a config error")
	}
}

func TestCheckExitCodes(t *testing.T) {
	srv := newFakeServer(t)
	h := newHarness(t, srv.srv.URL)
	if code := h.run("check"); code != 0 {
		t.Errorf("check ok = %d, want 0\n%s", code, h.stderr.String())
	}
	if !strings.Contains(h.stderr.String(), "192.168.1.0/24") {
		t.Errorf("check does not list the on-link subnet:\n%s", h.stderr.String())
	}

	writeConfig(t, h.dir, map[string]any{"server_url": srv.srv.URL, "name": "desktop", "token": "tok", "subnets": []string{"8.8.8.0/24"}})
	if code := h.run("check"); code != 2 {
		t.Errorf("check with a non-private subnet = %d, want 2", code)
	}
	if code := newHarness(t, closedURL(t)).run("check"); code != 3 {
		t.Errorf("check with the server unreachable = %d, want 3", code)
	}
	bad := newHarness(t, srv.srv.URL)
	writeConfig(t, bad.dir, map[string]any{"server_url": srv.srv.URL, "name": "desktop", "token": "wrong"})
	if code := bad.run("check"); code != 4 {
		t.Errorf("check with a rejected token = %d, want 4", code)
	}
}

func TestScanOnceExitCodes(t *testing.T) {
	srv := newFakeServer(t)
	h := newHarness(t, srv.srv.URL)
	if code := h.run("scan", "--once"); code != 0 {
		t.Fatalf("scan --once = %d, want 0\n%s", code, h.stderr.String())
	}
	if srv.uploads != 1 || h.spooled(t) != 0 {
		t.Errorf("uploads=%d spooled=%d, want 1 and 0", srv.uploads, h.spooled(t))
	}
	for _, want := range []string{"subnets", "found", "stored"} {
		if !strings.Contains(h.stderr.String(), want) {
			t.Errorf("summary lacks %q:\n%s", want, h.stderr.String())
		}
	}

	down := newHarness(t, closedURL(t))
	if code := down.run("scan", "--once"); code != 5 {
		t.Errorf("scan with the server down = %d, want 5 (spooled)", code)
	}
	if down.spooled(t) != 1 {
		t.Errorf("run not kept in the spool when the server is down")
	}

	srv.uploadStatus = http.StatusUnauthorized
	rej := newHarness(t, srv.srv.URL)
	writeConfig(t, rej.dir, map[string]any{"server_url": srv.srv.URL, "name": "desktop", "token": "wrong"})
	if code := rej.run("scan", "--once"); code != 4 {
		t.Errorf("scan with a rejected token = %d, want 4", code)
	}
}

func TestDryRun(t *testing.T) {
	srv := newFakeServer(t)
	h := newHarness(t, srv.srv.URL)
	if code := h.run("scan", "--once", "--dry-run"); code != 0 {
		t.Fatalf("dry run = %d\n%s", code, h.stderr.String())
	}
	run, err := contract.Decode(bytes.NewReader(h.stdout.Bytes()))
	if err != nil {
		t.Fatalf("dry run did not print a CollectionRun: %v\n%s", err, h.stdout.String())
	}
	if err := contract.Validate(run); err != nil {
		t.Errorf("dry-run payload invalid: %v", err)
	}
	if h.spooled(t) != 0 || srv.uploads != 0 {
		t.Error("dry run must not spool or upload")
	}
}

func TestVersion(t *testing.T) {
	h := newHarness(t, "http://unused")
	if code := h.run("version"); code != 0 || !strings.Contains(h.stdout.String(), "schema") {
		t.Errorf("version = %d %q", code, h.stdout.String())
	}
}
