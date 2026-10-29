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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/clock"
	"github.com/atotmakov/home_net_explorer/internal/collect"
	"github.com/atotmakov/home_net_explorer/internal/collect/ports"
	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/upload"
	"gopkg.in/yaml.v3"
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

// configFile is the collector's config (feature 009); legacyConfigFile is read only when it is
// missing (configs downloaded before 0.10).
const (
	configFile       = "hne-collector.yaml"
	legacyConfigFile = "hne-collector.json"
)

// config is hne-collector.yaml, overridable by HNE_SERVER_URL, HNE_TOKEN, HNE_SUBNETS and
// HNE_EXTRA_SUBNETS.
type config struct {
	ServerURL       string        `json:"server_url" yaml:"server_url"`
	Name            string        `json:"name" yaml:"name"`
	Token           string        `json:"token" yaml:"token"`
	Subnets         []string      `json:"subnets" yaml:"subnets"`
	ExtraSubnets    []string      `json:"extra_subnets" yaml:"extra_subnets"` // scanned in addition to subnets/auto-discovery
	IntervalSeconds int           `json:"interval_seconds" yaml:"interval_seconds"`
	Routers         legacyRouters `json:"routers" yaml:"routers"` // no longer used (feature 013)

	prefixes      []netip.Prefix
	extraPrefixes []netip.Prefix
}

// environment is everything run needs from the outside world (replaced in tests).
type environment struct {
	dir            string // directory of hne-collector.yaml, spool/ and the log
	getenv         func(string) string
	stdout, stderr io.Writer
	newEngine      func(cfg config) (*collect.Engine, func() error, error)
	// routerTransport carries router traffic (nil: direct connections). Tests send it to a
	// routertest.Fake while configs keep private router addresses.
	routerTransport http.RoundTripper
	apTransport     http.RoundTripper // access point traffic (feature 012), like routerTransport
	ctx             context.Context   // parent of the command's context (nil: background)
	// newPortChecker builds the port checker (feature 015); nil: the real network.
	newPortChecker func() *ports.Checker
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
	switch {
	case err == nil:
		dec := yaml.NewDecoder(bytes.NewReader(b))
		dec.KnownFields(true) // a misspelt setting is an error, not silently ignored
		if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
			return cfg, fmt.Errorf("%s: %w", configFile, err)
		}
	case errors.Is(err, fs.ErrNotExist):
		lb, lerr := os.ReadFile(filepath.Join(dir, legacyConfigFile))
		if lerr != nil {
			return cfg, fmt.Errorf("read %s (download it from the server's Collectors page): %w", configFile, err)
		}
		if err := json.Unmarshal(contract.StripComments(lb), &cfg); err != nil { // // comments allowed (feature 008)
			return cfg, fmt.Errorf("%s: %w", legacyConfigFile, err)
		}
	default:
		return cfg, fmt.Errorf("read %s: %w", configFile, err)
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
	return cfg, nil
}

// legacyRouters is the routers section of an older config (features 002, 003). Routers are set
// up only in the web UI now (feature 013), so its entries are never used: only how many there
// were and how many carried a login are kept, never a value (FR-004).
type legacyRouters struct {
	Entries   int
	WithLogin int
}

func (l *legacyRouters) count(entries []map[string]any) {
	l.Entries = len(entries)
	for _, e := range entries {
		if e["username"] != nil || e["password"] != nil {
			l.WithLogin++
		}
	}
}

// UnmarshalYAML implements yaml.Unmarshaler. Errors never quote the section.
func (l *legacyRouters) UnmarshalYAML(n *yaml.Node) error {
	var entries []map[string]any
	if n.Decode(&entries) != nil {
		return errors.New("routers: must be a list (and is no longer used: set routers up in the web UI)")
	}
	l.count(entries)
	return nil
}

// UnmarshalJSON implements json.Unmarshaler for hne-collector.json. Errors never quote the section.
func (l *legacyRouters) UnmarshalJSON(b []byte) error {
	var entries []map[string]any
	if json.Unmarshal(b, &entries) != nil {
		return errors.New("routers: must be a list (and is no longer used: set routers up in the web UI)")
	}
	l.count(entries)
	return nil
}

// warn tells the owner, at each start, that the section is ignored (FR-003).
func (l legacyRouters) warn(log *slog.Logger) {
	if l.Entries == 0 {
		return
	}
	log.Warn("the routers section of hne-collector.yaml is no longer used: routers are set up on their " +
		"device page in the web UI and the collector gets them from the server; delete the section")
	if l.WithLogin > 0 {
		log.Warn("a router login in hne-collector.yaml is ignored: enter it on the router's device page in the web UI")
	}
}

func platformEngine(cfg config) (*collect.Engine, func() error, error) {
	prober, presence, neighbors, routes, closeFn := collect.Platform()
	return &collect.Engine{
		Prober: prober, Presence: presence, Neighbors: neighbors, Routes: routes,
		NewResolver: func(v contract.Vantage) collect.Resolver {
			r, err := collect.NewVantageResolver(netip.Addr{}, v)
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
	jsonOut, dryRun, forcePorts := false, false, false
	var cmd string
	for _, a := range args {
		switch a {
		case "--json":
			jsonOut = true
		case "--dry-run":
			dryRun = true
		case "--ports":
			forcePorts = true // feature 015: check the ports of every address found
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
		fmt.Fprintln(env.stderr, "usage: hne-collector check | scan --once [--dry-run] [--ports] | run [--ports] | version  [--json]")
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
	cfg.Routers.warn(log)
	engine, closeEngine, err := env.newEngine(cfg)
	if err != nil {
		log.Error("cannot start the scanner", "err", err)
		return exitFailure
	}
	defer closeEngine()
	engine.IntervalSeconds = cfg.IntervalSeconds
	// A router or access point whose login was rejected is skipped until its login changes in the
	// web UI; check clears the marker and tries once more (feature 002 FR-011, 003 FR-014).
	if cmd == "check" {
		os.Remove(filepath.Join(env.dir, rejectedFile))
	}

	checker := &ports.Checker{}
	if env.newPortChecker != nil {
		checker = env.newPortChecker()
	}
	c := &collector{
		cfg:        cfg,
		env:        env,
		log:        log,
		jsonOut:    jsonOut,
		engine:     engine,
		forcePorts: forcePorts,
		portJob: &collect.PortJob{Checker: checker, Clock: engine.Clock, Collector: engine.Collector,
			IntervalSeconds: cfg.IntervalSeconds},
		client: &upload.Client{BaseURL: cfg.ServerURL, Token: cfg.Token, Log: log},
		spool:  upload.Spool{Dir: filepath.Join(env.dir, "spool")},
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
		return c.scanOnce(ctx, dryRun, false)
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

	serverRouters []contract.RouterRef // routers set up in the web UI (ping, else the cache)
	markerMu      sync.Mutex           // guards the rejection marker file

	// Feature 015: port checks after each presence run.
	portJob    *collect.PortJob
	forcePorts bool        // --ports: check every address found (in run: the first job only)
	portBusy   atomic.Bool // a background port job is running
	flushMu    sync.Mutex  // one spool flush at a time (the loop and a background port job)
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
	c.ignored(ctx) // refreshes the router list; the server check below reports errors
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
				fallback = " (plus ICMP/TCP: hosts that answer count as online)"
			}
			fmt.Fprintf(w, "  %-18s router %s%s\n", p.Prefix, p.Router, fallback)
		default:
			fmt.Fprintf(w, "  %-18s on-link via %s: ARP\n", p.Prefix, p.Interface)
		}
	}
	c.checkRouters(ctx, w)
	c.checkAccessPoints(ctx, w)
	ping, err := c.client.Ping(ctx)
	if code := c.pingExit(err); code != exitOK {
		return code
	}
	fmt.Fprintf(w, "Server %s: OK (collector %q, server time %s)\n", c.cfg.ServerURL, ping.Collector, ping.ServerTime.Format(time.RFC3339))
	switch ps := ping.PortScan; {
	case ps == nil:
		fmt.Fprintln(w, "Port checks: unknown (the server is older than 0.20)")
	case ps.Enabled:
		fmt.Fprintf(w, "Port checks: on (%d due)\n", len(ps.Due))
	default:
		fmt.Fprintln(w, "Port checks: off")
	}
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
		c.serverRouters = c.cachedRouters()
		c.buildRouters()
		c.buildAccessPoints(c.cachedAPs())
		return c.lastIgnored(), err
	}
	c.serverRouters = ping.Routers
	c.saveRouterCache(ping.Routers)
	c.buildRouters()
	c.saveAPCache(ping.AccessPoints)
	c.buildAccessPoints(ping.AccessPoints)
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
// The port job (feature 015) follows the presence run: in the foreground for scan, in the
// background for run (background).
func (c *collector) scanOnce(ctx context.Context, dryRun, background bool) int {
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
	for _, s := range run.Subnets {
		if s.SkipReason == contract.SkipFakedAnswers {
			c.log.Warn("subnet skipped: answers faked on the network path (almost every address answered); "+
				"make the VPN on this computer bypass local traffic", "subnet", s.CIDR)
		}
	}
	run.SentAt = time.Now().UTC()
	if dryRun {
		enc := json.NewEncoder(c.env.stdout)
		enc.SetIndent("", "  ")
		enc.Encode(run)
		c.portCheck(ctx, run, true, false)
		return exitOK
	}
	if _, err := c.spool.Save(run); err != nil {
		c.log.Error("cannot write the spool", "dir", c.spool.Dir, "err", err)
		return exitFailure
	}
	res, err := c.flush(ctx)
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
	if code == exitOK || code == exitSpooled {
		c.portCheck(ctx, run, false, background)
	}
	return code
}

// flush uploads the spool; a background port job and the loop never flush at the same time.
func (c *collector) flush(ctx context.Context) (upload.FlushResult, error) {
	c.flushMu.Lock()
	defer c.flushMu.Unlock()
	return c.client.Flush(ctx, c.spool)
}

// portCheck checks the ports of the run's due addresses, or of all of them when forced, and
// uploads the port run (feature 015). The plan is asked for after the presence upload, so a
// device the server first learned from it is already due (research R5). In the background, a
// job still running from an earlier run means no new one.
func (c *collector) portCheck(ctx context.Context, run *contract.CollectionRun, dryRun, background bool) {
	force := c.forcePorts
	if background {
		c.forcePorts = false // run --ports forces the first job only
	}
	var plan *contract.PortScan
	if ping, err := c.client.Ping(ctx); err == nil {
		plan = ping.PortScan
	} else if !force {
		return
	}
	targets := collect.PortTargets(run, plan, force)
	if len(targets) == 0 {
		return
	}
	job := func(ctx context.Context) {
		pr := c.portJob.Run(ctx, run.Vantage, targets)
		open := 0
		for _, pc := range pr.PortChecks {
			open += len(pc.Open)
		}
		c.log.Info("port check finished", "devices", len(pr.PortChecks), "open", open,
			"duration", pr.FinishedAt.Sub(pr.StartedAt).Round(time.Millisecond))
		if dryRun {
			enc := json.NewEncoder(c.env.stdout)
			enc.SetIndent("", "  ")
			enc.Encode(pr)
			return
		}
		pr.SentAt = time.Now().UTC()
		if _, err := c.spool.Save(pr); err != nil {
			c.log.Error("cannot write the spool", "dir", c.spool.Dir, "err", err)
			return
		}
		if _, err := c.flush(ctx); err != nil {
			c.log.Warn("port run upload deferred", "err", err)
		}
	}
	if !background {
		job(ctx)
		return
	}
	if !c.portBusy.CompareAndSwap(false, true) {
		c.log.Info("port check still running; no new one started")
		return
	}
	go func() {
		defer c.portBusy.Store(false)
		job(ctx)
	}()
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
			code := c.scanOnce(ctx, false, true)
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
			_, err := c.flush(ctx)
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
