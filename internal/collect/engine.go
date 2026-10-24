package collect

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"iter"
	"net"
	"net/netip"
	"slices"
	"sync"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/clock"
	"github.com/atotmakov/home_net_explorer/internal/collect/accesspoint"
	"github.com/atotmakov/home_net_explorer/internal/collect/router"
	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// DefaultConcurrency is the maximum number of probes in flight.
const DefaultConcurrency = 64

// ScanOptions select what to scan.
type ScanOptions struct {
	// Targets restricts scanning to these private subnets. Empty means auto-discovery: every
	// on-link private subnet of /22 or narrower (research R14).
	Targets []netip.Prefix
	// Ignored subnets are reported as skipped/ignored and never probed.
	Ignored []netip.Prefix
	// Extra subnets are scanned in addition to Targets or auto-discovery (feature 002,
	// FR-015): ARP when on-link, otherwise ICMP/TCP.
	Extra []netip.Prefix
}

// Engine runs one collection: it plans subnets from the vantage report, probes them in
// parallel, merges the neighbor cache, resolves names and returns a contract.CollectionRun.
type Engine struct {
	Prober    Prober         // ARP for on-link subnets
	Presence  PresenceProber // ICMP/TCP for routed subnets (optional)
	Neighbors NeighborTable  // optional
	Routes    RouteReader
	Resolver  Resolver // optional; if nil, NewResolver is used
	// NewResolver builds a resolver for this vantage (e.g. pinned to the LAN gateway as DNS).
	NewResolver     func(v contract.Vantage) Resolver
	Clock           clock.Clock
	Collector       contract.CollectorInfo
	IntervalSeconds int
	Concurrency     int
	// Routers are the configured router sources (feature 002). Each is read once per scan,
	// before probing, bounded by RouterTimeout (default router.DefaultTimeout).
	Routers       []router.Source
	RouterTimeout time.Duration
	// AccessPoints are the configured access points (feature 012). Each is read once per scan,
	// in parallel with the routers and the probes, bounded by APTimeout (default
	// accesspoint.DefaultTimeout).
	AccessPoints []accesspoint.Source
	APTimeout    time.Duration
}

type target struct {
	prefix netip.Prefix
	iface  string        // on-link interface; "" for a routed subnet
	skip   string        // contract.SkipTooLarge / SkipIgnored, or "" to scan
	router router.Source // the router whose device list covers this subnet, if any
	// probe marks a routed subnet the collector probes itself (listed in Targets or Extra).
	// With a router, that probe replaces a failed read (research R4) and adds hosts that answer
	// beside a good one (the router's online flag lags).
	probe bool
}

// Scan performs one collection. A cancelled context ends probing early and the affected
// subnets are reported with complete=false; the partial run is still returned.
func (e *Engine) Scan(ctx context.Context, opts ScanOptions) (*contract.CollectionRun, error) {
	clk := e.Clock
	if clk == nil {
		clk = clock.Real{}
	}
	started := clk.Now()
	v, err := e.Routes.Vantage(ctx)
	if err != nil {
		return nil, fmt.Errorf("collect: read interfaces and routes: %w", err)
	}
	if v.Interfaces == nil {
		v.Interfaces = []contract.Interface{}
	}
	if v.Routes == nil {
		v.Routes = []contract.Route{}
	}

	run := &contract.CollectionRun{
		SchemaVersion:   contract.SchemaVersion,
		CollectionID:    NewUUID(),
		Collector:       e.Collector,
		StartedAt:       started,
		IntervalSeconds: e.IntervalSeconds,
		Vantage:         v,
		Subnets:         []contract.SubnetScan{},
		Observations:    []contract.Observation{},
	}

	var mu sync.Mutex
	found := map[netip.Addr]contract.Observation{}
	record := func(o contract.Observation) {
		mu.Lock()
		found[netip.MustParseAddr(o.IP)] = o
		mu.Unlock()
	}

	targets := planTargets(v, opts, e.Routers)
	apReads := e.readAccessPoints(ctx)
	reads := e.readRouters(ctx, targets)
	var arpScanned []netip.Prefix
	var routerObs []contract.Observation // recorded last: one observation per IP, the router's wins
	for i, t := range targets {
		if t.skip != "" {
			run.Subnets = append(run.Subnets, contract.SubnetScan{CIDR: t.prefix.String(), Method: contract.MethodSkipped, SkipReason: t.skip})
			continue
		}
		res, read := reads[i]
		if read {
			run.Sources = append(run.Sources, contract.RunSource{Type: contract.SourceTypeRouter, Model: t.router.Model(),
				Address: t.router.Address().String(), Subnet: t.prefix.String(), Outcome: res.Outcome, Online: res.Online, Offline: res.Offline})
		}
		ok := read && res.Outcome == contract.OutcomeOK
		if ok {
			routerObs = append(routerObs, res.Observations...)
			if res.Self != nil && t.prefix.Contains(netip.MustParseAddr(res.Self.IP)) {
				routerObs = append(routerObs, *res.Self)
			}
		}
		switch {
		case t.iface == "" && ok:
			run.Subnets = append(run.Subnets, contract.SubnetScan{CIDR: t.prefix.String(), Method: contract.MethodRouterTable,
				Complete: true, HostsProbed: res.Online + res.Offline})
			if t.probe {
				e.probeBesideRouter(ctx, clk, t, res, record)
			}
		case t.iface == "" && t.router != nil && t.probe:
			// ICMP/TCP fallback: presence only, never a completed scan of the router's subnet
			// (FR-010), so devices only the router can see are not marked offline.
			scan := e.probeRouted(ctx, clk, t, record)
			if scan.Method != contract.MethodSkipped {
				scan.Complete = false
			}
			run.Subnets = append(run.Subnets, scan)
		case t.iface == "" && t.router != nil:
			run.Subnets = append(run.Subnets, contract.SubnetScan{CIDR: t.prefix.String(), Method: contract.MethodRouterTable})
		case t.iface == "":
			run.Subnets = append(run.Subnets, e.probeRouted(ctx, clk, t, record))
		default:
			scan := e.probeSubnet(ctx, clk, t, record)
			run.Subnets = append(run.Subnets, scan)
			if scan.Method == contract.MethodARP {
				arpScanned = append(arpScanned, t.prefix)
			}
		}
	}

	e.mergeNeighbors(ctx, clk, arpScanned, found, record)
	e.addSelf(clk, v, arpScanned, found, record)
	now := clk.Now()
	for _, o := range routerObs {
		o.ObservedAt = now
		record(o)
	}
	run.Sources = append(run.Sources, e.addAccessPointClients(apReads(), run.Subnets, now, found)...)

	obs := make([]contract.Observation, 0, len(found))
	for _, o := range found {
		obs = append(obs, o)
	}
	slices.SortFunc(obs, func(a, b contract.Observation) int {
		return netip.MustParseAddr(a.IP).Compare(netip.MustParseAddr(b.IP))
	})
	e.resolveNames(ctx, v, obs)

	run.Observations = obs
	run.FinishedAt = clk.Now()
	run.SentAt = run.FinishedAt
	return run, nil
}

// readRouters reads, concurrently, the router of every scanned target that has one, keyed by
// target index. Failed reads carry no data.
func (e *Engine) readRouters(ctx context.Context, targets []target) map[int]router.Result {
	timeout := e.RouterTimeout
	if timeout <= 0 {
		timeout = router.DefaultTimeout
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	out := map[int]router.Result{}
	for i, t := range targets {
		if t.router == nil || t.skip != "" {
			continue
		}
		wg.Add(1)
		go func(i int, src router.Source) {
			defer wg.Done()
			rctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			res := src.Read(rctx)
			if res.Outcome != contract.OutcomeOK {
				res = router.Result{Outcome: res.Outcome}
			}
			mu.Lock()
			out[i] = res
			mu.Unlock()
		}(i, t.router)
	}
	wg.Wait()
	return out
}

// readAccessPoints starts reading every access point and returns a function that waits for the
// results, sorted by access point address (research R7: the higher address wins a client listed
// by two access points).
func (e *Engine) readAccessPoints(ctx context.Context) func() []apRead {
	timeout := e.APTimeout
	if timeout <= 0 {
		timeout = accesspoint.DefaultTimeout
	}
	out := make([]apRead, len(e.AccessPoints))
	var wg sync.WaitGroup
	for i, src := range e.AccessPoints {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			res := src.Read(rctx)
			if res.Outcome != contract.OutcomeOK {
				res = accesspoint.Result{Outcome: res.Outcome}
			}
			out[i] = apRead{src: src, res: res}
		}()
	}
	return func() []apRead {
		wg.Wait()
		slices.SortStableFunc(out, func(a, b apRead) int { return a.src.Address().Compare(b.src.Address()) })
		return out
	}
}

type apRead struct {
	src accesspoint.Source
	res accesspoint.Result
}

// addAccessPointClients turns the clients of successful reads into ap_client observations
// (research R6, R7). A client needs a valid MAC and an IP inside a non-skipped subnet of the
// run; its observation replaces whatever else was recorded for that IP, keeping a hostname the
// AP doesn't know. It returns one RunSource per access point.
func (e *Engine) addAccessPointClients(reads []apRead, subnets []contract.SubnetScan, now time.Time, found map[netip.Addr]contract.Observation) []contract.RunSource {
	var scanned []netip.Prefix
	for _, s := range subnets {
		if s.Method != contract.MethodSkipped {
			scanned = append(scanned, netip.MustParsePrefix(s.CIDR))
		}
	}
	inScanned := func(ip netip.Addr) bool {
		return slices.ContainsFunc(scanned, func(p netip.Prefix) bool { return p.Contains(ip) })
	}
	byMAC := map[string]netip.Addr{} // a client listed by two access points keeps the later one
	var sources []contract.RunSource
	for _, rd := range reads {
		src := contract.RunSource{Type: contract.SourceTypeAccessPoint, Model: rd.src.Model(),
			Address: rd.src.Address().String(), Subnet: rd.src.Prefix().String(), Outcome: rd.res.Outcome}
		for _, c := range rd.res.Clients {
			if c.MAC == "" || !c.IP.Is4() || !inScanned(c.IP) {
				src.Offline++
				continue
			}
			src.Online++
			if prev, ok := byMAC[c.MAC]; ok && prev != c.IP {
				delete(found, prev)
			}
			byMAC[c.MAC] = c.IP
			o := contract.Observation{ObservedAt: now, IP: c.IP.String(), MAC: c.MAC, Method: contract.ObsAPClient,
				Via: accesspoint.Via(c), AccessPoint: src.Address}
			if c.Hostname != "" {
				o.Hostname, o.HostnameSource = c.Hostname, contract.HostnameSourceRouter
			} else if old, ok := found[c.IP]; ok && old.MAC == c.MAC {
				o.Hostname, o.HostnameSource = old.Hostname, old.HostnameSource
			}
			found[c.IP] = o
		}
		sources = append(sources, src)
	}
	return sources
}

// Faked answers (feature 005): a routed subnet where at least SaturationMinProbed addresses were
// probed and SaturationPercent or more answered is treated as faked by the network path.
const (
	SaturationMinProbed = 16
	SaturationPercent   = 90
)

// probeRouted probes a routed subnet with ICMP/TCP. If the subnet is saturated (the network
// path answers for every address, as a VPN tunnel does), the subnet is reported as skipped with
// reason faked_answers and nothing is recorded. A probe of a reserved public address is no
// test: proxies that answer for public addresses often route private subnets directly.
func (e *Engine) probeRouted(ctx context.Context, clk clock.Clock, t target, record func(contract.Observation)) contract.SubnetScan {
	faked := contract.SubnetScan{CIDR: t.prefix.String(), Method: contract.MethodSkipped, SkipReason: contract.SkipFakedAnswers}
	var mu sync.Mutex
	var found []contract.Observation
	scan := e.probeSubnet(ctx, clk, t, func(o contract.Observation) {
		mu.Lock()
		found = append(found, o)
		mu.Unlock()
	})
	if scan.HostsProbed >= SaturationMinProbed && len(found)*100 >= scan.HostsProbed*SaturationPercent {
		return faked
	}
	for _, o := range found {
		record(o)
	}
	return scan
}

// probeBesideRouter probes a router's subnet that the collector can reach, after a successful
// read: the router's online flag lags (a Huawei HG8145V5 lists devices that just reconnected as
// offline), so a host that answers is online too. It keeps the MAC and name the router lists for
// its address. The router's online entries are recorded later and win for their addresses.
// Saturated (faked) answers add nothing.
func (e *Engine) probeBesideRouter(ctx context.Context, clk clock.Clock, t target, res router.Result, record func(contract.Observation)) {
	listed := map[string]contract.Observation{}
	for _, o := range res.Listed {
		listed[o.IP] = o
	}
	e.probeRouted(ctx, clk, t, func(o contract.Observation) {
		if l, ok := listed[o.IP]; ok {
			o.MAC, o.Hostname, o.HostnameSource = l.MAC, l.Hostname, l.HostnameSource
		}
		record(o)
	})
}

func (e *Engine) probeSubnet(ctx context.Context, clk clock.Clock, t target, record func(contract.Observation)) contract.SubnetScan {
	scan := contract.SubnetScan{CIDR: t.prefix.String(), Method: contract.MethodARP, Complete: true}
	if t.iface == "" {
		scan.Method = contract.MethodICMPTCP
	}
	n := e.Concurrency
	if n <= 0 {
		n = DefaultConcurrency
	}
	sem := make(chan struct{}, n)
	var wg sync.WaitGroup
	for ip := range hosts(t.prefix) {
		if ctx.Err() != nil {
			scan.Complete = false
			break
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			scan.Complete = false
		}
		if !scan.Complete {
			break
		}
		scan.HostsProbed++
		wg.Add(1)
		go func(ip netip.Addr) {
			defer wg.Done()
			defer func() { <-sem }()
			if o, ok := e.probe(ctx, clk, t, ip); ok {
				record(o)
			}
		}(ip)
	}
	wg.Wait()
	if ctx.Err() != nil {
		scan.Complete = false
	}
	return scan
}

func (e *Engine) probe(ctx context.Context, clk clock.Clock, t target, ip netip.Addr) (contract.Observation, bool) {
	if t.iface == "" {
		if e.Presence == nil {
			return contract.Observation{}, false
		}
		method, ok := e.Presence.Present(ctx, ip)
		if !ok {
			return contract.Observation{}, false
		}
		return contract.Observation{ObservedAt: clk.Now(), IP: ip.String(), Method: method}, true
	}
	mac, ok, err := e.Prober.Probe(ctx, t.iface, ip)
	if err != nil || !ok {
		return contract.Observation{}, false
	}
	mac, ok = NormalizeMAC(mac)
	if !ok {
		return contract.Observation{}, false
	}
	return contract.Observation{ObservedAt: clk.Now(), IP: ip.String(), MAC: mac, Method: contract.ObsARP}, true
}

// mergeNeighbors adds neighbor-cache entries inside ARP-scanned subnets that didn't answer.
func (e *Engine) mergeNeighbors(ctx context.Context, clk clock.Clock, scanned []netip.Prefix, found map[netip.Addr]contract.Observation, record func(contract.Observation)) {
	if e.Neighbors == nil || len(scanned) == 0 {
		return
	}
	entries, err := e.Neighbors.Entries(ctx)
	if err != nil {
		return
	}
	now := clk.Now()
	for _, n := range entries {
		if _, ok := found[n.IP]; ok || !containedIn(n.IP, scanned) {
			continue
		}
		mac, ok := NormalizeMAC(n.MAC)
		if !ok {
			continue
		}
		record(contract.Observation{ObservedAt: now, IP: n.IP.String(), MAC: mac, Method: contract.ObsNeighborCache})
	}
}

// addSelf records the collector's own addresses (it never answers its own ARP requests).
func (e *Engine) addSelf(clk clock.Clock, v contract.Vantage, scanned []netip.Prefix, found map[netip.Addr]contract.Observation, record func(contract.Observation)) {
	now := clk.Now()
	for _, ifc := range v.Interfaces {
		ip, err := netip.ParseAddr(ifc.IP)
		if err != nil || !containedIn(ip, scanned) {
			continue
		}
		if _, ok := found[ip]; ok {
			continue
		}
		mac, ok := NormalizeMAC(ifc.MAC)
		if !ok {
			continue
		}
		record(contract.Observation{ObservedAt: now, IP: ip.String(), MAC: mac, Method: contract.ObsARP})
	}
}

func (e *Engine) resolveNames(ctx context.Context, v contract.Vantage, obs []contract.Observation) {
	r := e.Resolver
	if r == nil && e.NewResolver != nil {
		r = e.NewResolver(v)
	}
	if r == nil || ctx.Err() != nil {
		return
	}
	sem := make(chan struct{}, 32)
	var wg sync.WaitGroup
	for i := range obs {
		if obs[i].Hostname != "" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(o *contract.Observation) {
			defer wg.Done()
			defer func() { <-sem }()
			if host, src, ok := r.Lookup(ctx, netip.MustParseAddr(o.IP)); ok && len(host) <= 253 {
				o.Hostname, o.HostnameSource = host, src
			}
		}(&obs[i])
	}
	wg.Wait()
}

// planTargets decides what to scan (research R14): the on-link (or listed) subnets, plus the
// subnet of each router source (feature 002, research R4/R5).
func planTargets(v contract.Vantage, opts ScanOptions, routers []router.Source) []target {
	onlink := map[netip.Prefix]string{}
	for _, ifc := range v.Interfaces {
		a, err := netip.ParseAddr(ifc.IP)
		if err != nil || !a.Is4() || ifc.PrefixLen < 0 || ifc.PrefixLen > 32 {
			continue
		}
		p := netip.PrefixFrom(a, ifc.PrefixLen).Masked()
		if !contract.IsPrivate(p) {
			continue // never scan non-private networks
		}
		if _, ok := onlink[p]; !ok {
			onlink[p] = ifc.Name
		}
	}
	ignored := map[netip.Prefix]bool{}
	for _, p := range opts.Ignored {
		ignored[p.Masked()] = true
	}

	var out []target
	if len(opts.Targets) == 0 {
		for p, iface := range onlink {
			if p.Bits() < contract.MinPrefixBits || p.Bits() > contract.MaxPrefixBits {
				continue // cannot be expressed in a run (e.g. a /8 or a point-to-point /31)
			}
			t := target{prefix: p, iface: iface}
			switch {
			case ignored[p]:
				t.skip = contract.SkipIgnored
			case !contract.AutoScannable(p):
				t.skip = contract.SkipTooLarge
			}
			out = append(out, t)
		}
	} else {
		seen := map[netip.Prefix]bool{}
		for _, p := range opts.Targets {
			p = p.Masked()
			if seen[p] || !contract.IsPrivate(p) || p.Bits() < contract.MinPrefixBits || p.Bits() > contract.MaxPrefixBits {
				continue
			}
			seen[p] = true
			t := target{prefix: p}
			for op, iface := range onlink {
				if op.Bits() <= p.Bits() && op.Contains(p.Addr()) {
					t.iface = iface
					break
				}
			}
			if ignored[p] {
				t.skip = contract.SkipIgnored
			}
			t.probe = t.iface == ""
			out = append(out, t)
		}
	}
	for _, p := range opts.Extra {
		p = p.Masked()
		if !contract.IsPrivate(p) || p.Bits() < contract.MinPrefixBits || p.Bits() > contract.MaxPrefixBits {
			continue
		}
		if i := slices.IndexFunc(out, func(t target) bool { return t.prefix == p }); i >= 0 {
			if out[i].skip == contract.SkipTooLarge {
				out[i].skip = "" // listed explicitly by the owner
			}
			continue // already planned: scanned once, with the on-link method if attached (FR-016)
		}
		t := target{prefix: p}
		for op, iface := range onlink {
			if op.Bits() <= p.Bits() && op.Contains(p.Addr()) {
				t.iface = iface
				break
			}
		}
		if ignored[p] {
			t.skip = contract.SkipIgnored
		}
		t.probe = t.iface == ""
		out = append(out, t)
	}
	for _, r := range routers {
		p := r.Prefix().Masked()
		i := slices.IndexFunc(out, func(t target) bool { return t.prefix == p })
		switch {
		case i >= 0 && out[i].router == nil:
			out[i].router = r
		case i < 0 && contract.IsPrivate(p) && p.Bits() >= contract.MinPrefixBits && p.Bits() <= contract.MaxPrefixBits:
			t := target{prefix: p, router: r}
			if ignored[p] {
				t.skip = contract.SkipIgnored
			}
			out = append(out, t)
		}
	}
	if len(out) > contract.MaxSubnets {
		// Keep on-link subnets first, then routed ones, each by address.
		slices.SortStableFunc(out, func(a, b target) int {
			if (a.iface == "") != (b.iface == "") {
				if a.iface != "" {
					return -1
				}
				return 1
			}
			return a.prefix.Addr().Compare(b.prefix.Addr())
		})
		out = out[:contract.MaxSubnets]
	}
	slices.SortFunc(out, func(a, b target) int { return a.prefix.Addr().Compare(b.prefix.Addr()) })
	return out
}

// hosts yields every host address of p (excluding the network and broadcast addresses).
func hosts(p netip.Prefix) iter.Seq[netip.Addr] {
	return func(yield func(netip.Addr) bool) {
		p = p.Masked()
		a4 := p.Addr().As4()
		base := binary.BigEndian.Uint32(a4[:])
		size := uint32(1) << uint(32-p.Bits())
		for i := uint32(1); i+1 < size; i++ {
			var b [4]byte
			binary.BigEndian.PutUint32(b[:], base+i)
			if !yield(netip.AddrFrom4(b)) {
				return
			}
		}
	}
}

func containedIn(ip netip.Addr, ps []netip.Prefix) bool {
	for _, p := range ps {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// NormalizeMAC returns mac in lower-case aa:bb:cc:dd:ee:ff form; all-zero and malformed
// addresses are rejected.
func NormalizeMAC(mac string) (string, bool) {
	hw, err := net.ParseMAC(mac)
	if err != nil || len(hw) != 6 {
		return "", false
	}
	zero := true
	for _, b := range hw {
		if b != 0 {
			zero = false
		}
	}
	if zero {
		return "", false
	}
	return hw.String(), true
}

// NewUUID returns a random (version 4) UUID.
func NewUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// PlannedSubnet is how a scan will treat one subnet (shown by `hne-collector check`).
type PlannedSubnet struct {
	Prefix     netip.Prefix
	Interface  string // on-link interface; "" for a routed subnet
	Method     string // contract.MethodARP, MethodICMPTCP, MethodRouterTable or MethodSkipped
	SkipReason string
	Router     string // "<model> at <address>" when a router's device list covers the subnet
	Fallback   bool   // a routed router subnet the collector also probes (beside a good read, instead of a failed one)
}

// Plan reports which subnets a scan with these options and routers would cover, and how.
func Plan(v contract.Vantage, opts ScanOptions, routers []router.Source) []PlannedSubnet {
	var out []PlannedSubnet
	for _, t := range planTargets(v, opts, routers) {
		p := PlannedSubnet{Prefix: t.prefix, Interface: t.iface, Method: contract.MethodARP}
		if t.router != nil {
			p.Router = t.router.Model() + " at " + t.router.Address().String()
		}
		switch {
		case t.skip != "":
			p.Method, p.SkipReason = contract.MethodSkipped, t.skip
		case t.iface == "" && t.router != nil:
			p.Method, p.Fallback = contract.MethodRouterTable, t.probe
		case t.iface == "":
			p.Method = contract.MethodICMPTCP
		}
		out = append(out, p)
	}
	return out
}
