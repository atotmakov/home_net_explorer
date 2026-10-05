package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestConfigDefaults(t *testing.T) {
	cfg, err := loadConfig(nil, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":8080" || cfg.DataDir != "/data" || cfg.ScanInterval != 900*time.Second {
		t.Errorf("defaults = %+v", cfg)
	}
	if len(cfg.Subnets) != 0 {
		t.Errorf("default subnets = %v, want none (auto-discovery)", cfg.Subnets)
	}
	if cfg.DNSServer.IsValid() {
		t.Errorf("default DNS server = %v, want unset", cfg.DNSServer)
	}
}

func TestConfigEnvAndFlags(t *testing.T) {
	cfg, err := loadConfig([]string{"--listen", ":9090", "--no-builtin-scan"}, env(map[string]string{
		"HNE_DATA":          "/tmp/x",
		"HNE_SCAN_INTERVAL": "5m",
		"HNE_SUBNETS":       "192.168.1.0/24, 10.20.30.0/24",
		"HNE_DNS_SERVER":    "192.168.1.100",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":9090" || !cfg.NoBuiltinScan || cfg.DataDir != "/tmp/x" || cfg.ScanInterval != 5*time.Minute {
		t.Errorf("cfg = %+v", cfg)
	}
	if len(cfg.Subnets) != 2 || cfg.Subnets[1].String() != "10.20.30.0/24" {
		t.Errorf("subnets = %v", cfg.Subnets)
	}
	if cfg.DNSServer.String() != "192.168.1.100" {
		t.Errorf("dns = %v", cfg.DNSServer)
	}
}

func TestConfigRejects(t *testing.T) {
	bad := []map[string]string{
		{"HNE_SUBNETS": "8.8.8.0/24"},        // not private
		{"HNE_SUBNETS": "192.168.0.0/15"},    // wider than /16
		{"HNE_SUBNETS": "10.0.0.0/31"},       // narrower than /30
		{"HNE_SUBNETS": "192.168.1.0"},       // not a CIDR
		{"HNE_DNS_SERVER": "8.8.8.8"},        // public resolver (Principle I)
		{"HNE_DNS_SERVER": "nonsense"},       // not an address
		{"HNE_SCAN_INTERVAL": "0s"},          // must be positive
		{"HNE_SCAN_INTERVAL": "fifteen mins"}, // unparsable
	}
	for _, m := range bad {
		if _, err := loadConfig(nil, env(m)); err == nil {
			t.Errorf("loadConfig(%v) succeeded, want error", m)
		}
	}
}

func TestHealthcheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	if code := runHealthcheck(srv.Listener.Addr().String()); code != 0 {
		t.Errorf("healthcheck against healthy server = %d, want 0", code)
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	if code := runHealthcheck(addr); code != 1 {
		t.Errorf("healthcheck with nothing listening = %d, want 1", code)
	}
}

func TestHealthcheckURL(t *testing.T) {
	cases := map[string]string{
		":8080":          "http://127.0.0.1:8080/healthz",
		"0.0.0.0:8080":   "http://127.0.0.1:8080/healthz",
		"127.0.0.1:9000": "http://127.0.0.1:9000/healthz",
	}
	for listen, want := range cases {
		if got := healthcheckURL(listen); got != want {
			t.Errorf("healthcheckURL(%q) = %q, want %q", listen, got, want)
		}
	}
}
