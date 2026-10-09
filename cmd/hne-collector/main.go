// Command hne-collector scans the subnets visible from this machine and uploads the
// observations to the Home Net Explorer server (contracts/collector-cli.md).
//
//	hne-collector check                     show interfaces, planned subnets and the server status
//	hne-collector scan --once [--dry-run]   one collection, then upload everything pending
//	hne-collector run                       scan every interval_seconds until stopped
//	hne-collector version
//
// Global flag --json switches log output to one JSON object per line on stdout.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/clock"
	"github.com/atotmakov/home_net_explorer/internal/collect"
	"github.com/atotmakov/home_net_explorer/internal/collect/router"
	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/upload"
)

// version and commit are set at build time with -ldflags "-X main.version=... -X main.commit=...".
var (
	version = "dev"
	commit  = "unknown"
)

// Exit codes (contracts/collector-cli.md).
const (
	exitOK          = 0
	exitConfig      = 2
	exitUnreachable = 3
	exitToken       = 4
	exitSpooled     = 5
	exitFailure     = 1
)

const configFile = "hne-collector.json"

// config is hne-collector.json, overridable by HNE_SERVER_URL, HNE_TOKEN, HNE_SUBNETS and
// HNE_EXTRA_SUBNETS.
type config struct {
	ServerURL       string          `json:"server_url"`
	Name            string          `json:"name"`
	Token           string          `json:"token"`
	Subnets         []string        `json:"subnets"`
	ExtraSubnets    []string        `json:"extra_subnets"` // scanned in addition to subnets/auto-discovery
	IntervalSeconds int             `json:"interval_seconds"`
	Routers         []router.Config `json:"routers"` // opt-in router sources (feature 002)

	prefixes      []netip.Prefix
	extraPrefixes []netip.Prefix
}

// environment is everything run needs from the outside world (replaced in tests).
type environment struct {
	dir            string // directory of hne-collector.json, spool/ and the log
	getenv         func(string) string
	stdout, stderr io.Writer
	newEngine      func(cfg config) (*collect.Engine, func() error, error)
	// routerTransport carries router traffic (nil: direct connections). Tests send it to a
	// routertest.Fake while configs keep private router addresses.
	routerTransport http.RoundTripper
	ctx             context.Context // parent of the command's context (nil: background)
}

func main() {
	dir := "."
	if exe, err := os.Executable(); err == nil {
		dir = filepath.Dir(exe)
	}
	os.Exit(run(os.Args[1:], environment{dir: dir, getenv: os.Getenv, stdout: os.Stdout, stderr: os.Stderr, newEngine: platformEngine}))
}

func loadConfig(dir string, getenv func(string) string) (config, error) {
	cfg := config{IntervalSeconds: 900} // default when the file omits it
	b, err := os.ReadFile(filepath.Join(dir, configFile))
	if err != nil {
		return cfg, fmt.Errorf("read %s (download it from the server's Collectors page): %w", configFile, err)
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("%s: %w", configFile, err)
	}
	if v := strings.TrimSpace(getenv("HNE_SERVER_URL")); v != "" {
		cfg.ServerURL = v
	}
	if v := strings.TrimSpace(getenv("HNE_TOKEN")); v != "" {
		cfg.Token = v
	}
	if v := strings.TrimSpace(getenv("HNE_SUBNETS")); v != "" {
		cfg.Subnets = strings.Split(v, ",")
	}
	if v := strings.TrimSpace(getenv("HNE_EXTRA_SUBNETS")); v != "" {
		cfg.ExtraSubnets = strings.Split(v, ",")
	}

	u, err := url.Parse(cfg.ServerURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return cfg, fmt.Errorf("server_url %q must be an http(s) URL", cfg.ServerURL)
	}
	if cfg.Token == "" {
		return cfg, errors.New("token is missing")
	}
	if !contract.ValidCollectorName(cfg.Name) {
		return cfg, fmt.Errorf("name %q must match [a-z0-9-]{1,64}", cfg.Name)
	}
	if cfg.IntervalSeconds < 0 {
		return cfg, errors.New("interval_seconds must be >= 0")
	}
	// Empty subnets (the default): discover every on-link private subnet at each scan.
	for i, s := range cfg.Subnets {
		s = strings.TrimSpace(s)
		cfg.Subnets[i] = s
		p, err := contract.ParseSubnet(s)
		if err != nil {
			return cfg, fmt.Errorf("subnets: %w", err)
		}
		cfg.prefixes = append(cfg.prefixes, p)
	}
	for _, s := range cfg.ExtraSubnets {
		p, err := contract.ParseSubnet(strings.TrimSpace(s))
		if err != nil {
			return cfg, fmt.Errorf("extra_subnets: %w", err)
		}
		cfg.extraPrefixes = append(cfg.extraPrefixes, p)
	}
	if len(cfg.Routers) > contract.MaxSources {
		return cfg, fmt.Errorf("routers: at most %d entries", contract.MaxSources)
	}
	for i, r := range cfg.Routers {
		if err := r.Validate(); err != nil { // never contains credentials (FR-006)
			return cfg, fmt.Errorf("routers[%d]: %w", i, err)
		}
	}
	return cfg, nil
}

func platformEngine(cfg config) (*collect.Engine, func() error, error) {
	prober, presence, neighbors, routes, closeFn := collect.Platform()
	return &collect.Engine{
		Prober: prober, Presence: presence, Neighbors: neighbors, Routes: routes,
		NewResolver: func(v contract.Vantage) collect.Resolver {
			server, _ := collect.PickDNSServer(netip.Addr{}, v)
			r, err := collect.NewNameResolver(server)
			if err != nil {
				return nil
			}
			return r
		},
		Clock:     clock.Real{},
		Collector: contract.CollectorInfo{Name: cfg.Name, Version: version, OS: runtime.GOOS},
	}, closeFn, nil
}

func run(args []string, env environment) int {
	jsonOut, dryRun := false, false
	var cmd string
	for _, a := range args {
		switch a {
		case "--json":
			jsonOut = true
		case "--dry-run":
			dryRun = true
		case "--once":
			// scan is always a single collection; run is the loop
		default:
			if cmd == "" && !strings.HasPrefix(a, "-") {
				cmd = a
			} else {
				fmt.Fprintf(env.stderr, "hne-collector: unknown argument %q\n", a)
				return exitConfig
			}
		}
	}
	if cmd == "version" {
		fmt.Fprintf(env.stdout, "hne-collector %s (commit %s, schema versions: %d)\n", version, commit, contract.SchemaVersion)
		return exitOK
	}
	if cmd != "check" && cmd != "scan" && cmd != "run" {
		fmt.Fprintln(env.stderr, "usage: hne-collector check | scan --once [--dry-run] | run | version  [--json]")
		return exitConfig
	}

	log := slog.New(slog.NewTextHandler(env.stderr, nil))
	if jsonOut {
		log = slog.New(slog.NewJSONHandler(env.stdout, nil))
	}
	cfg, err := loadConfig(env.dir, env.getenv)
	if err != nil {
		log.Error("config error", "err", err)
		return exitConfig
	}
	engine, closeEngine, err := env.newEngine(cfg)
	if err != nil {
		log.Error("cannot start the scanner", "err", err)
		return exitFailure
	}
	defer closeEngine()
	engine.IntervalSeconds = cfg.IntervalSeconds
	// FR-011: a router whose login was rejected is skipped until its config changes; check
	// clears the marker and tries once more.
	rejected := loadRejected(env.dir)
	if cmd == "check" {
		os.Remove(filepath.Join(env.dir, rejectedFile))
		rejected = nil
	}
	for _, rc := range cfg.Routers {
		src, err := router.New(rc, env.routerTransport)
		if err != nil {
			log.Error("config error", "err", err)
			return exitConfig
		}
		if rejected[routerHash(rc)] {
			src = router.Skipped(src)
		}
		engine.Routers = append(engine.Routers, src)
	}

	c := &collector{
		cfg:     cfg,
		env:     env,
		log:     log,
		jsonOut: jsonOut,
		engine:  engine,
		client:  &upload.Client{BaseURL: cfg.ServerURL, Token: cfg.Token, Log: log},
		spool:   upload.Spool{Dir: filepath.Join(env.dir, "spool")},
	}
	parent := env.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()
	switch cmd {
	case "check":
		return c.check(ctx)
	case "scan":
		return c.scanOnce(ctx, dryRun)
	default:
		return c.loop(ctx)
	}
}

type collector struct {
	cfg     config
	env     environment
	log     *slog.Logger
	jsonOut bool
	engine  *collect.Engine
	client  *upload.Client
	spool   upload.Spool
}

func (c *collector) check(ctx context.Context) int {
	v, err := c.engine.Routes.Vantage(ctx)
	if err != nil {
		c.log.Error("cannot read network interfaces", "err", err)
		return exitFailure
	}
	w := c.env.stderr
	fmt.Fprintf(w, "Interfaces of %s:\n", v.Hostname)
	for _, ifc := range v.Interfaces {
		fmt.Fprintf(w, "  %-20s %s/%d %s\n", ifc.Name, ifc.IP, ifc.PrefixLen, ifc.MAC)
	}
	fmt.Fprintln(w, "Subnets to scan:")
	plan := collect.Plan(v, collect.ScanOptions{Targets: c.cfg.prefixes, Ignored: c.lastIgnored(), Extra: c.cfg.extraPrefixes}, c.engine.Routers)
	if len(plan) == 0 {
		fmt.Fprintln(w, "  (none: no private subnet of /22 or narrower is attached)")
	}
	for _, p := range plan {
		switch p.Method {
		case contract.MethodSkipped:
			fmt.Fprintf(w, "  %-18s skipped (%s)\n", p.Prefix, p.SkipReason)
		case contract.MethodICMPTCP:
			fmt.Fprintf(w, "  %-18s routed: ICMP/TCP presence only, no MACs\n", p.Prefix)
		case contract.MethodRouterTable:
			fallback := ""
			if p.Fallback {
				fallback = " (fallback: ICMP/TCP)"
			}
			fmt.Fprintf(w, "  %-18s router %s%s\n", p.Prefix, p.Router, fallback)
		default:
			fmt.Fprintf(w, "  %-18s on-link via %s: ARP\n", p.Prefix, p.Interface)
		}
	}
	c.checkRouters(ctx, w)
	ping, err := c.client.Ping(ctx)
	if code := c.pingExit(err); code != exitOK {
		return code
	}
	fmt.Fprintf(w, "Server %s: OK (collector %q, server time %s)\n", c.cfg.ServerURL, ping.Collector, ping.ServerTime.Format(time.RFC3339))
	return exitOK
}

func (c *collector) pingExit(err error) int {
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, upload.ErrTokenRejected):
		c.log.Error("the server rejected the token; create a new one on the Collectors page", "server", c.cfg.ServerURL)
		return exitToken
	default:
		c.log.Error("server unreachable", "server", c.cfg.ServerURL, "err", err)
		return exitUnreachable
	}
}

// ignored subnets: from ping when the server answers, else the last known list.
func (c *collector) ignored(ctx context.Context) ([]netip.Prefix, error) {
	ping, err := c.client.Ping(ctx)
	if err != nil {
		return c.lastIgnored(), err
	}
	var out []netip.Prefix
	for _, s := range ping.IgnoredSubnets {
		if p, err := netip.ParsePrefix(s); err == nil {
			out = append(out, p)
		}
	}
	if b, err := json.Marshal(ping.IgnoredSubnets); err == nil {
		os.WriteFile(c.ignoredCache(), b, 0o600)
	}
	return out, nil
}

// ignoredCache keeps the last ignored_subnets from the server (outside spool/, which holds
// only runs) for scans while the server is unreachable.
func (c *collector) ignoredCache() string {
	return filepath.Join(c.env.dir, "hne-collector.ignored")
}

func (c *collector) lastIgnored() []netip.Prefix {
	b, err := os.ReadFile(c.ignoredCache())
	if err != nil {
		return nil
	}
	var ss []string
	json.Unmarshal(b, &ss)
	var out []netip.Prefix
	for _, s := range ss {
		if p, err := netip.ParsePrefix(s); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// scanOnce runs one collection and uploads everything pending.
func (c *collector) scanOnce(ctx context.Context, dryRun bool) int {
	ignored, err := c.ignored(ctx)
	if errors.Is(err, upload.ErrTokenRejected) && !dryRun {
		return c.pingExit(err)
	}
	if err != nil && !dryRun {
		c.log.Warn("server unreachable; using the last known ignored subnets", "err", err)
	}
	run, err := c.engine.Scan(ctx, collect.ScanOptions{Targets: c.cfg.prefixes, Ignored: ignored, Extra: c.cfg.extraPrefixes})
	if err != nil {
		c.log.Error("scan failed", "err", err)
		return exitFailure
	}
	c.recordRejections(run.Sources)
	run.SentAt = time.Now().UTC()
	if dryRun {
		enc := json.NewEncoder(c.env.stdout)
		enc.SetIndent("", "  ")
		enc.Encode(run)
		return exitOK
	}
	if _, err := c.spool.Save(run); err != nil {
		c.log.Error("cannot write the spool", "dir", c.spool.Dir, "err", err)
		return exitFailure
	}
	res, err := c.client.Flush(ctx, c.spool)
	result := "stored"
	if res.Last.Status != "" {
		result = res.Last.Status
	}
	code := exitOK
	switch {
	case errors.Is(err, upload.ErrTokenRejected):
		result, code = "token rejected (run kept in spool)", exitToken
	case err != nil:
		result, code = "spooled (server unreachable, will retry)", exitSpooled
		c.log.Warn("upload deferred", "err", err)
	}
	c.summary(run, result)
	return code
}

func (c *collector) summary(run *contract.CollectionRun, result string) {
	scanned := 0
	for _, s := range run.Subnets {
		if s.Method != contract.MethodSkipped {
			scanned++
		}
	}
	attrs := []any{"subnets", scanned, "found", len(run.Observations),
		"duration", run.FinishedAt.Sub(run.StartedAt).Round(time.Millisecond), "upload", result}
	if len(run.Sources) > 0 {
		outcomes := make([]string, len(run.Sources))
		for i, s := range run.Sources {
			outcomes[i] = s.Outcome
		}
		attrs = append(attrs, "router", strings.Join(outcomes, ","))
	}
	c.log.Info("scan finished", attrs...)
}

// loop scans every interval and retries pending uploads with backoff (1 minute up to 1 hour).
func (c *collector) loop(ctx context.Context) int {
	if f, err := newRotatingFile(filepath.Join(c.env.dir, "hne-collector.log"), 1<<20, 3); err == nil {
		defer f.Close()
		c.log = slog.New(slog.NewTextHandler(io.MultiWriter(c.env.stderr, f), nil))
		c.client.Log = c.log
	}
	interval := time.Duration(c.cfg.IntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	c.log.Info("collector started", "server", c.cfg.ServerURL, "interval", interval, "version", version)
	backoff := upload.NewBackoff()
	nextScan := time.Now()
	var nextRetry time.Time
	for {
		now := time.Now()
		switch {
		case !now.Before(nextScan):
			code := c.scanOnce(ctx, false)
			if code == exitToken {
				return exitToken
			}
			nextScan = now.Add(interval)
			if code == exitSpooled {
				nextRetry = now.Add(backoff.Next())
			} else {
				backoff.Reset()
				nextRetry = time.Time{}
			}
		case !nextRetry.IsZero() && !now.Before(nextRetry):
			_, err := c.client.Flush(ctx, c.spool)
			switch {
			case errors.Is(err, upload.ErrTokenRejected):
				return c.pingExit(err)
			case err != nil:
				nextRetry = now.Add(backoff.Next())
				c.log.Info("upload still deferred; will retry", "at", nextRetry.Format(time.TimeOnly), "err", err)
			default:
				backoff.Reset()
				nextRetry = time.Time{}
			}
		}
		wake := nextScan
		if !nextRetry.IsZero() && nextRetry.Before(wake) {
			wake = nextRetry
		}
		select {
		case <-ctx.Done():
			c.log.Info("collector stopped")
			return exitOK
		case <-time.After(time.Until(wake)):
		}
	}
}

// rotatingFile is a size-capped log: name, name.1 … name.(keep-1).
type rotatingFile struct {
	path string
	max  int64
	keep int
	f    *os.File
	size int64
}

func newRotatingFile(path string, max int64, keep int) (*rotatingFile, error) {
	r := &rotatingFile{path: path, max: max, keep: keep}
	return r, r.open()
}

func (r *rotatingFile) open() error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	info, _ := f.Stat()
	r.f, r.size = f, info.Size()
	return nil
}

func (r *rotatingFile) Write(p []byte) (int, error) {
	if r.size+int64(len(p)) > r.max {
		r.f.Close()
		for i := r.keep - 1; i >= 1; i-- {
			src := r.path
			if i > 1 {
				src = fmt.Sprintf("%s.%d", r.path, i-1)
			}
			os.Rename(src, fmt.Sprintf("%s.%d", r.path, i))
		}
		if err := r.open(); err != nil {
			return 0, err
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

func (r *rotatingFile) Close() error { return r.f.Close() }
