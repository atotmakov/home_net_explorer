package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// Config is the server configuration: environment variables, overridable by flags.
type Config struct {
	Listen        string         // HNE_LISTEN / --listen (default ":8080")
	DataDir       string         // HNE_DATA / --data (default "/data")
	ScanInterval  time.Duration  // HNE_SCAN_INTERVAL (default 900s)
	Subnets       []netip.Prefix // HNE_SUBNETS: optional restriction; empty = auto-discovery
	DNSServer     netip.Addr     // HNE_DNS_SERVER: optional; must be private (Principle I)
	NoBuiltinScan bool           // --no-builtin-scan
	DownloadsDir  string         // HNE_DOWNLOADS: collector binaries (default /app/downloads)
}

func loadConfig(args []string, getenv func(string) string) (Config, error) {
	def := func(key, fallback string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return fallback
	}
	cfg := Config{}
	fs := flag.NewFlagSet("hne-server", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfg.Listen, "listen", def("HNE_LISTEN", ":8080"), "listen address")
	fs.StringVar(&cfg.DataDir, "data", def("HNE_DATA", "/data"), "data directory")
	fs.BoolVar(&cfg.NoBuiltinScan, "no-builtin-scan", false, "disable the built-in collector")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}

	cfg.DownloadsDir = def("HNE_DOWNLOADS", "/app/downloads")

	interval, err := time.ParseDuration(def("HNE_SCAN_INTERVAL", "900s"))
	if err != nil || interval <= 0 {
		return cfg, fmt.Errorf("HNE_SCAN_INTERVAL must be a positive duration (e.g. 900s)")
	}
	cfg.ScanInterval = interval

	if v := getenv("HNE_SUBNETS"); strings.TrimSpace(v) != "" {
		for _, s := range strings.Split(v, ",") {
			p, err := contract.ParseSubnet(strings.TrimSpace(s))
			if err != nil {
				return cfg, fmt.Errorf("HNE_SUBNETS: %w", err)
			}
			cfg.Subnets = append(cfg.Subnets, p)
		}
	}

	if v := strings.TrimSpace(getenv("HNE_DNS_SERVER")); v != "" {
		a, err := netip.ParseAddr(v)
		if err != nil {
			return cfg, fmt.Errorf("HNE_DNS_SERVER: %w", err)
		}
		if !contract.IsPrivateAddr(a) {
			return cfg, errors.New("HNE_DNS_SERVER must be a private (RFC 1918) address: PTR lookups must not leave the home network")
		}
		cfg.DNSServer = a
	}
	return cfg, nil
}

// healthcheckURL turns a listen address into the local /healthz URL.
func healthcheckURL(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		host, port = "", listen
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/healthz"
}

// runHealthcheck is the container HEALTHCHECK (distroless has no curl): exit 0 if healthy.
func runHealthcheck(listen string) int {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(healthcheckURL(listen))
	if err != nil {
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
