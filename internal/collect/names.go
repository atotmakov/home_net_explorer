package collect

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// mdnsGroup is the IPv4 mDNS multicast group and port (RFC 6762).
var mdnsGroup = netip.AddrPortFrom(netip.AddrFrom4([4]byte{224, 0, 0, 251}), 5353)

// NameResolver finds hostnames with reverse DNS (PTR) against a LAN DNS server, then mDNS.
// It never uses the system resolver: the host may be configured with a public DNS server, and
// sending PTR queries for LAN addresses there would leak the inventory (Principle I).
type NameResolver struct {
	server  netip.Addr // zero: DNS disabled, mDNS only
	timeout time.Duration
	dial    func(ctx context.Context, network, address string) (net.Conn, error)
	dns     func(ctx context.Context, ip netip.Addr) (string, bool)
	mdns    func(ctx context.Context, ip netip.Addr) (string, bool)
}

// NewNameResolver returns a resolver that sends PTR queries only to server, which must be a
// private (RFC 1918) address. A zero server disables DNS and keeps mDNS.
func NewNameResolver(server netip.Addr) (*NameResolver, error) {
	if server.IsValid() && !contract.IsPrivateAddr(server) {
		return nil, fmt.Errorf("collect: DNS server %s is not a private address; PTR lookups must stay on the LAN", server)
	}
	d := &net.Dialer{}
	r := &NameResolver{server: server, timeout: time.Second, dial: d.DialContext, mdns: lookupMDNS}
	r.dns = r.lookupDNS
	return r, nil
}

// Lookup tries DNS first, then mDNS; each gets its own time budget.
func (r *NameResolver) Lookup(ctx context.Context, ip netip.Addr) (string, string, bool) {
	try := func(f func(context.Context, netip.Addr) (string, bool)) (string, bool) {
		c, cancel := context.WithTimeout(ctx, r.timeout)
		defer cancel()
		return f(c, ip)
	}
	if h, ok := try(r.dns); ok {
		return h, contract.HostnameSourceDNS, true
	}
	if h, ok := try(r.mdns); ok {
		return h, contract.HostnameSourceMDNS, true
	}
	return "", "", false
}

func (r *NameResolver) lookupDNS(ctx context.Context, ip netip.Addr) (string, bool) {
	if !r.server.IsValid() {
		return "", false
	}
	server := netip.AddrPortFrom(r.server, 53).String()
	res := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return r.dial(ctx, network, server) // pinned: ignore resolv.conf servers
		},
	}
	names, err := res.LookupAddr(ctx, ip.String())
	if err != nil || len(names) == 0 {
		return "", false
	}
	return strings.TrimSuffix(names[0], "."), true
}

// PickDNSServer chooses the PTR server: the configured one, else the private next hop of a
// default route (normally the home router, which knows DHCP names).
func PickDNSServer(configured netip.Addr, v contract.Vantage) (netip.Addr, bool) {
	if configured.IsValid() {
		return configured, true
	}
	for _, rt := range v.Routes {
		p, err := netip.ParsePrefix(rt.Destination)
		if err != nil || p.Bits() != 0 || rt.NextHop == "" {
			continue
		}
		if gw, err := netip.ParseAddr(rt.NextHop); err == nil && contract.IsPrivateAddr(gw) {
			return gw, true
		}
	}
	return netip.Addr{}, false
}

// lookupMDNS sends a "legacy unicast" PTR query to the mDNS group from an ephemeral port;
// responders answer by unicast to that port (RFC 6762 §6.7).
func lookupMDNS(ctx context.Context, ip netip.Addr) (string, bool) {
	q, id, err := buildPTRQuery(ip)
	if err != nil {
		return "", false
	}
	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return "", false
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl)
	}
	if _, err := conn.WriteToUDPAddrPort(q, mdnsGroup); err != nil {
		return "", false
	}
	buf := make([]byte, 9000)
	for {
		n, _, err := conn.ReadFromUDPAddrPort(buf)
		if err != nil {
			return "", false
		}
		if host, ok := parsePTRAnswer(buf[:n], id); ok {
			return host, true
		}
	}
}

func reverseName(ip netip.Addr) string {
	a := ip.As4()
	return fmt.Sprintf("%d.%d.%d.%d.in-addr.arpa.", a[3], a[2], a[1], a[0])
}

func buildPTRQuery(ip netip.Addr) ([]byte, uint16, error) {
	if !ip.Is4() {
		return nil, 0, errors.New("collect: PTR query needs an IPv4 address")
	}
	var idb [2]byte
	if _, err := rand.Read(idb[:]); err != nil {
		return nil, 0, err
	}
	id := binary.BigEndian.Uint16(idb[:])
	name, err := dnsmessage.NewName(reverseName(ip))
	if err != nil {
		return nil, 0, err
	}
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: id})
	if err := b.StartQuestions(); err != nil {
		return nil, 0, err
	}
	if err := b.Question(dnsmessage.Question{Name: name, Type: dnsmessage.TypePTR, Class: dnsmessage.ClassINET}); err != nil {
		return nil, 0, err
	}
	msg, err := b.Finish()
	return msg, id, err
}

func parsePTRAnswer(msg []byte, id uint16) (string, bool) {
	var p dnsmessage.Parser
	h, err := p.Start(msg)
	if err != nil || h.ID != id || !h.Response {
		return "", false
	}
	if err := p.SkipAllQuestions(); err != nil {
		return "", false
	}
	for {
		ah, err := p.AnswerHeader()
		if err != nil {
			return "", false
		}
		if ah.Type != dnsmessage.TypePTR {
			if err := p.SkipAnswer(); err != nil {
				return "", false
			}
			continue
		}
		ptr, err := p.PTRResource()
		if err != nil {
			return "", false
		}
		return strings.TrimSuffix(ptr.PTR.String(), "."), true
	}
}
