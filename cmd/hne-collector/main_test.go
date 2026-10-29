package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/clock"
	"github.com/atotmakov/home_net_explorer/internal/collect"
	"github.com/atotmakov/home_net_explorer/internal/collect/collecttest"
	"github.com/atotmakov/home_net_explorer/internal/collect/ports"
	"github.com/atotmakov/home_net_explorer/internal/contract"
	"gopkg.in/yaml.v3"
)

type fakeServer struct {
	srv          *httptest.Server
	pingStatus   int
	uploadStatus int
	uploads      int
	bodies       [][]byte // every upload request body
	onUpload     func()   // called after each upload is recorded

	// Feature 003: routers configured in the web UI and their logins.
	routers       []contract.RouterRef
	accessPoints  []contract.RouterRef // feature 012
	logins        map[int64]contract.RouterLogin
	loginRequests int

	// Feature 015: the port plan of each ping in turn (the last one repeats); nil = none.
	plans []*contract.PortScan
	pings int
}

func newFakeServer(t *testing.T) *fakeServer {
	f := &fakeServer{pingStatus: 200, uploadStatus: 201, logins: map[int64]contract.RouterLogin{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(contract.ErrorResponse{Error: contract.CodeInvalidToken})
			return
		}
		switch r.URL.Path {
		case "/api/v1/ping":
			var plan *contract.PortScan
			if len(f.plans) > 0 {
				plan = f.plans[min(f.pings, len(f.plans)-1)]
			}
			f.pings++
			w.WriteHeader(f.pingStatus)
			json.NewEncoder(w).Encode(contract.PingResponse{Collector: "desktop", ServerTime: time.Now(),
				SupportedSchemaVersions: []int{1}, IgnoredSubnets: []string{}, Routers: f.routers, AccessPoints: f.accessPoints,
				PortScan: plan})
		case "/api/v1/collections":
			f.uploads++
			b, _ := io.ReadAll(r.Body)
			f.bodies = append(f.bodies, b)
			if f.onUpload != nil {
				f.onUpload()
			}
			w.WriteHeader(f.uploadStatus)
			json.NewEncoder(w).Encode(contract.UploadResult{Status: contract.StatusStored})
		default:
			var id int64
			if _, err := fmt.Sscanf(r.URL.Path, "/api/v1/routers/%d/login", &id); err == nil {
				f.loginRequests++
				if l, ok := f.logins[id]; ok {
					json.NewEncoder(w).Encode(l)
					return
				}
				w.WriteHeader(http.StatusNotFound)
				json.NewEncoder(w).Encode(contract.ErrorResponse{Error: "router_not_found"})
				return
			}
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// writeConfig writes hne-collector.yaml (feature 009).
func writeConfig(t *testing.T, dir string, c map[string]any) {
	t.Helper()
	b, err := yaml.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hne-collector.yaml"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

type harness struct {
	dir            string
	env            map[string]string
	stdout, stderr bytes.Buffer
	routerRT       http.RoundTripper // sends router requests to a routertest.Fake
	apRT           http.RoundTripper // sends access point requests to an eaptest.AP (feature 012)
	ctx            context.Context   // parent context of the command (nil: background)
	answerAll      bool              // the network path answers every address (a VPN, feature 005)
	hosts          []string          // more hosts on the fake network (feature 015)
	portsOpen      map[string][]int  // open TCP ports per address on the fake network (feature 015)
	checked        []string          // addresses the fake port checker was asked about
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
	fnet.AnswerAll = h.answerAll
	for i, ip := range h.hosts {
		fnet.Add(ip, collecttest.Host{MAC: fmt.Sprintf("00:00:5e:00:53:%02x", i+1)})
	}
	var mu sync.Mutex
	h.checked = nil
	return run(args, environment{
		dir:             h.dir,
		getenv:          func(k string) string { return h.env[k] },
		stdout:          &h.stdout,
		stderr:          &h.stderr,
		routerTransport: h.routerRT,
		apTransport:     h.apRT,
		ctx:             h.ctx,
		newPortChecker: func() *ports.Checker {
			return &ports.Checker{MaxPort: 100, BannerTimeout: 5 * time.Millisecond,
				Dial: func(ctx context.Context, a netip.AddrPort, _ time.Duration) (net.Conn, error) {
					if a.Port() == 1 {
						mu.Lock()
						h.checked = append(h.checked, a.Addr().String())
						mu.Unlock()
					}
					if slices.Contains(h.portsOpen[a.Addr().String()], int(a.Port())) {
						c, s := net.Pipe()
						go func() { s.Read(make([]byte, 1)); s.Close() }()
						return c, nil
					}
					return nil, errors.New("connection refused")
				},
				UDP: func(context.Context, netip.AddrPort, []byte, time.Duration) ([]byte, error) {
					return nil, errors.New("no answer")
				}}
		},
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
		t.Error("missing hne-collector.yaml must be a config error")
	}
}

// Feature 009: hne-collector.yaml with # comments, as the server generates it.
func TestYAMLConfig(t *testing.T) {
	dir := t.TempDir()
	c := `# Address of the Home Net Explorer server.
server_url: http://192.168.1.100:8181
name: desktop   # shown on the Collectors page
token: file-token

# Empty: scan every private subnet this computer is connected to.
subnets: []
extra_subnets: ["192.168.0.0/24"]
interval_seconds: 600
routers:
  - model: huawei-hg8145v5
    url: http://192.168.0.1
    username: root
    password: "pw: #1"
`
	if err := os.WriteFile(filepath.Join(dir, "hne-collector.yaml"), []byte(c), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(dir, func(string) string { return "" })
	if err != nil {
		t.Fatalf("hne-collector.yaml: %v", err)
	}
	if cfg.ServerURL != "http://192.168.1.100:8181" || cfg.Name != "desktop" || cfg.Token != "file-token" ||
		len(cfg.Subnets) != 0 || len(cfg.ExtraSubnets) != 1 || cfg.IntervalSeconds != 600 {
		t.Errorf("cfg = %+v", cfg)
	}
	// Feature 013: an older routers section still loads; only its size is kept, never the login.
	if cfg.Routers.Entries != 1 || cfg.Routers.WithLogin != 1 {
		t.Errorf("routers = %+v, want 1 entry with a login", cfg.Routers)
	}
	if s := fmt.Sprintf("%+v %#v", cfg, cfg); strings.Contains(s, "pw: #1") {
		t.Errorf("the loaded config keeps the router password: %s", s)
	}

	// The YAML file wins over an old hne-collector.json next to it.
	old := `{"server_url": "http://10.0.0.9:8080", "name": "old", "token": "old-token", "subnets": []}`
	if err := os.WriteFile(filepath.Join(dir, "hne-collector.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if cfg, err := loadConfig(dir, func(string) string { return "" }); err != nil || cfg.Name != "desktop" {
		t.Errorf("with both files: name = %q, err = %v; want hne-collector.yaml used", cfg.Name, err)
	}

	// A misspelt setting is a config error rather than silently ignored.
	if err := os.WriteFile(filepath.Join(dir, "hne-collector.yaml"), []byte(c+"intervall_seconds: 60\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(dir, func(string) string { return "" }); err == nil || !strings.Contains(err.Error(), "intervall_seconds") {
		t.Errorf("unknown setting: err = %v, want an error naming it", err)
	}
}

// Feature 008: an older hne-collector.json, with or without // comments, still works when there
// is no hne-collector.yaml (feature 009).
func TestConfigWithComments(t *testing.T) {
	dir := t.TempDir()
	c := `{
  // Address of the Home Net Explorer server.
  "server_url": "http://192.168.1.100:8181",
  "name": "desktop", // shown on the Collectors page
  "token": "file-token",
  // Empty: scan every private subnet this computer is connected to.
  "subnets": [],
  "extra_subnets": ["192.168.0.0/24"],
  "interval_seconds": 600,
  "routers": []
}
`
	if err := os.WriteFile(filepath.Join(dir, "hne-collector.json"), []byte(c), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(dir, func(string) string { return "" })
	if err != nil {
		t.Fatalf("config with comments: %v", err)
	}
	if cfg.ServerURL != "http://192.168.1.100:8181" || cfg.Name != "desktop" || cfg.IntervalSeconds != 600 || len(cfg.ExtraSubnets) != 1 {
		t.Errorf("cfg = %+v", cfg)
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

// Feature 005: when the network path answers for every address, the collector skips the routed
// subnet and says so in its log.
func TestFakedAnswersReported(t *testing.T) {
	srv := newFakeServer(t)
	h := newHarness(t, srv.srv.URL)
	writeConfig(t, h.dir, map[string]any{"server_url": srv.srv.URL, "name": "desktop", "token": "tok",
		"subnets": []string{"192.168.0.0/24"}, "interval_seconds": 900})

	h.answerAll = true
	if code := h.run("scan", "--once"); code != 0 {
		t.Fatalf("scan = %d\n%s", code, h.stderr.String())
	}
	log := h.stderr.String()
	if !strings.Contains(log, "192.168.0.0/24") || !strings.Contains(log, "faked") {
		t.Errorf("no warning about the faked subnet:\n%s", log)
	}
	run, err := contract.Decode(bytes.NewReader(srv.bodies[len(srv.bodies)-1]))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range run.Subnets {
		if s.CIDR == "192.168.0.0/24" && (s.Method != contract.MethodSkipped || s.SkipReason != contract.SkipFakedAnswers) {
			t.Errorf("uploaded subnet = %+v, want skipped/faked_answers", s)
		}
	}
	if len(run.Observations) > 2 {
		t.Errorf("%d observations uploaded from a faking path", len(run.Observations))
	}
}
