// Package collecttest provides scriptable fakes of the collect interfaces, so no test ever
// touches the real network (Constitution II).
package collecttest

import (
	"context"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/collect"
	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// Host is a device on the fake network.
type Host struct {
	MAC            string
	Hostname       string
	HostnameSource string // contract.HostnameSourceDNS (default when Hostname is set) or MDNS
	Silent         bool   // present in the neighbor cache but does not answer probes
	TCPOnly        bool   // routed presence answers only to TCP, not ICMP
}

// FakeNetwork implements collect.Prober, collect.PresenceProber, collect.NeighborTable and
// collect.Resolver over a map of hosts. It records the peak number of concurrent probes.
type FakeNetwork struct {
	mu          sync.Mutex
	Hosts       map[netip.Addr]Host
	Neighbors   []collect.Neighbor
	ProbeDelay  time.Duration
	inflight    int
	MaxInflight int
	Probes      int
}

// NewFakeNetwork returns an empty fake network.
func NewFakeNetwork() *FakeNetwork {
	return &FakeNetwork{Hosts: map[netip.Addr]Host{}}
}

// Add puts a host on the network.
func (n *FakeNetwork) Add(ip string, h Host) *FakeNetwork {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.Hosts[netip.MustParseAddr(ip)] = h
	return n
}

// Remove takes a host off the network.
func (n *FakeNetwork) Remove(ip string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.Hosts, netip.MustParseAddr(ip))
}

func (n *FakeNetwork) enter() {
	n.mu.Lock()
	n.inflight++
	n.Probes++
	if n.inflight > n.MaxInflight {
		n.MaxInflight = n.inflight
	}
	n.mu.Unlock()
}

func (n *FakeNetwork) leave() {
	n.mu.Lock()
	n.inflight--
	n.mu.Unlock()
}

func (n *FakeNetwork) host(ip netip.Addr) (Host, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	h, ok := n.Hosts[ip]
	return h, ok
}

// Probe implements collect.Prober.
func (n *FakeNetwork) Probe(ctx context.Context, iface string, ip netip.Addr) (string, bool, error) {
	n.enter()
	defer n.leave()
	if n.ProbeDelay > 0 {
		select {
		case <-time.After(n.ProbeDelay):
		case <-ctx.Done():
			return "", false, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	h, ok := n.host(ip)
	if !ok || h.Silent || h.MAC == "" {
		return "", false, nil
	}
	return h.MAC, true, nil
}

// Present implements collect.PresenceProber.
func (n *FakeNetwork) Present(ctx context.Context, ip netip.Addr) (string, bool) {
	n.enter()
	defer n.leave()
	h, ok := n.host(ip)
	if !ok || h.Silent {
		return "", false
	}
	if h.TCPOnly {
		return contract.ObsTCP, true
	}
	return contract.ObsICMP, true
}

// Entries implements collect.NeighborTable.
func (n *FakeNetwork) Entries(ctx context.Context) ([]collect.Neighbor, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]collect.Neighbor(nil), n.Neighbors...), nil
}

// Lookup implements collect.Resolver.
func (n *FakeNetwork) Lookup(ctx context.Context, ip netip.Addr) (string, string, bool) {
	h, ok := n.host(ip)
	if !ok || h.Hostname == "" {
		return "", "", false
	}
	src := h.HostnameSource
	if src == "" {
		src = contract.HostnameSourceDNS
	}
	return h.Hostname, src, true
}

// FakeRoutes implements collect.RouteReader with a settable vantage.
type FakeRoutes struct {
	mu sync.Mutex
	V  contract.Vantage
}

// NewFakeRoutes builds a vantage from "name=ip/prefix" interfaces; the first interface's
// gateway (if non-empty) becomes the default route.
func NewFakeRoutes(gateway string, ifaces ...string) *FakeRoutes {
	r := &FakeRoutes{}
	r.Set(gateway, ifaces...)
	return r
}

// Set replaces the vantage (e.g. to simulate a new adapter appearing).
func (r *FakeRoutes) Set(gateway string, ifaces ...string) {
	v := contract.Vantage{Interfaces: []contract.Interface{}, Routes: []contract.Route{}}
	for _, spec := range ifaces {
		name, cidr, _ := strings.Cut(spec, "=")
		p := netip.MustParsePrefix(cidr)
		v.Interfaces = append(v.Interfaces, contract.Interface{Name: name, IP: p.Addr().String(), PrefixLen: p.Bits()})
		v.Routes = append(v.Routes, contract.Route{Destination: p.Masked().String(), Interface: name})
	}
	if gateway != "" && len(v.Interfaces) > 0 {
		def := netip.PrefixFrom(netip.IPv4Unspecified(), 0)
		v.Routes = append(v.Routes, contract.Route{Destination: def.String(), NextHop: gateway, Interface: v.Interfaces[0].Name})
	}
	r.mu.Lock()
	r.V = v
	r.mu.Unlock()
}

// Vantage implements collect.RouteReader.
func (r *FakeRoutes) Vantage(ctx context.Context) (contract.Vantage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.V, nil
}
