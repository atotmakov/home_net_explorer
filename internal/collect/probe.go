package collect

import (
	"context"
	"net/netip"

	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// Neighbor is an entry of the operating system's ARP/neighbor cache.
type Neighbor struct {
	IP  netip.Addr
	MAC string // lower-case aa:bb:cc:dd:ee:ff
}

// Prober checks whether an on-link address is present and returns its MAC (ARP).
type Prober interface {
	Probe(ctx context.Context, iface string, ip netip.Addr) (mac string, ok bool, err error)
}

// PresenceProber checks a routed (not on-link) address without learning its MAC
// (ICMP echo, then TCP connect). method is contract.ObsICMP or contract.ObsTCP.
type PresenceProber interface {
	Present(ctx context.Context, ip netip.Addr) (method string, ok bool)
}

// NeighborTable reads the operating system's neighbor cache.
type NeighborTable interface {
	Entries(ctx context.Context) ([]Neighbor, error)
}

// RouteReader reports this machine's interfaces and routes.
type RouteReader interface {
	Vantage(ctx context.Context) (contract.Vantage, error)
}

// Resolver finds a hostname for an address. source is contract.HostnameSourceDNS or
// contract.HostnameSourceMDNS.
type Resolver interface {
	Lookup(ctx context.Context, ip netip.Addr) (host, source string, ok bool)
}
