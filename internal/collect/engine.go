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

	"github.com/atotmakov/home_net_explorer/internal/clock"
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
}

type target struct {
	prefix netip.Prefix
	iface  string // on-link interface; "" for a routed subnet
	skip   string // contract.SkipTooLarge / SkipIgnored, or "" to scan
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

	targets := planTargets(v, opts)
	var arpScanned []netip.Prefix
	for _, t := range targets {
		if t.skip != "" {
			run.Subnets = append(run.Subnets, contract.SubnetScan{CIDR: t.prefix.String(), Method: contract.MethodSkipped, SkipReason: t.skip})
			continue
		}
		scan := e.probeSubnet(ctx, clk, t, record)
		run.Subnets = append(run.Subnets, scan)
		if scan.Method == contract.MethodARP {
			arpScanned = append(arpScanned, t.prefix)
		}
	}

	e.mergeNeighbors(ctx, clk, arpScanned, found, record)
	e.addSelf(clk, v, arpScanned, found, record)

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

// planTargets decides what to scan (research R14).
func planTargets(v contract.Vantage, opts ScanOptions) []target {
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
			out = append(out, t)
		}
	}
	slices.SortFunc(out, func(a, b target) int { return a.prefix.Addr().Compare(b.prefix.Addr()) })
	if len(out) > contract.MaxSubnets {
		out = out[:contract.MaxSubnets]
	}
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
	Method     string // contract.MethodARP, MethodICMPTCP or MethodSkipped
	SkipReason string
}

// Plan reports which subnets a scan with these options would cover, and how.
func Plan(v contract.Vantage, opts ScanOptions) []PlannedSubnet {
	var out []PlannedSubnet
	for _, t := range planTargets(v, opts) {
		p := PlannedSubnet{Prefix: t.prefix, Interface: t.iface, Method: contract.MethodARP}
		switch {
		case t.skip != "":
			p.Method, p.SkipReason = contract.MethodSkipped, t.skip
		case t.iface == "":
			p.Method = contract.MethodICMPTCP
		}
		out = append(out, p)
	}
	return out
}
