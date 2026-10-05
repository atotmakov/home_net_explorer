package collect

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/atotmakov/home_net_explorer/internal/contract"
)

func TestNameResolverRejectsPublicDNS(t *testing.T) {
	if _, err := NewNameResolver(netip.MustParseAddr("8.8.8.8")); err == nil {
		t.Error("a public DNS server must be rejected (Principle I)")
	}
	if _, err := NewNameResolver(netip.MustParseAddr("192.168.1.100")); err != nil {
		t.Errorf("private DNS server rejected: %v", err)
	}
}

func TestPickDNSServer(t *testing.T) {
	v := contract.Vantage{Routes: []contract.Route{
		{Destination: "192.168.1.0/24", Interface: "eth0"},
		{Destination: "0.0.0.0/0", NextHop: "192.168.1.100", Interface: "eth0"},
	}}
	if got, ok := PickDNSServer(netip.Addr{}, v); !ok || got.String() != "192.168.1.100" {
		t.Errorf("default DNS server = %v, %v; want the private gateway", got, ok)
	}
	if got, _ := PickDNSServer(netip.MustParseAddr("10.0.0.53"), v); got.String() != "10.0.0.53" {
		t.Errorf("configured DNS server ignored: %v", got)
	}
	public := contract.Vantage{Routes: []contract.Route{{Destination: "0.0.0.0/0", NextHop: "203.0.113.1"}}}
	if _, ok := PickDNSServer(netip.Addr{}, public); ok {
		t.Error("a public gateway must never be used as the DNS server")
	}
}

func TestPTRGoesOnlyToConfiguredServer(t *testing.T) {
	r, err := NewNameResolver(netip.MustParseAddr("192.168.1.100"))
	if err != nil {
		t.Fatal(err)
	}
	var dialed []string
	r.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		dialed = append(dialed, address)
		return nil, errors.New("test: no network")
	}
	r.mdns = func(ctx context.Context, ip netip.Addr) (string, bool) { return "", false }
	if _, _, ok := r.Lookup(context.Background(), netip.MustParseAddr("192.168.1.50")); ok {
		t.Error("lookup succeeded without a network")
	}
	if len(dialed) == 0 {
		t.Fatal("DNS server never dialed")
	}
	for _, a := range dialed {
		if a != "192.168.1.100:53" {
			t.Errorf("dialed %s; PTR queries must go only to the configured LAN DNS server", a)
		}
	}
}

func TestDNSPreferredOverMDNS(t *testing.T) {
	r, _ := NewNameResolver(netip.MustParseAddr("192.168.1.100"))
	r.dns = func(ctx context.Context, ip netip.Addr) (string, bool) { return "nas.lan", true }
	r.mdns = func(ctx context.Context, ip netip.Addr) (string, bool) { return "nas.local", true }
	host, src, ok := r.Lookup(context.Background(), netip.MustParseAddr("192.168.1.20"))
	if !ok || host != "nas.lan" || src != contract.HostnameSourceDNS {
		t.Errorf("Lookup = %q %q %v, want DNS answer", host, src, ok)
	}
	r.dns = func(ctx context.Context, ip netip.Addr) (string, bool) { return "", false }
	host, src, ok = r.Lookup(context.Background(), netip.MustParseAddr("192.168.1.20"))
	if !ok || host != "nas.local" || src != contract.HostnameSourceMDNS {
		t.Errorf("Lookup fallback = %q %q %v, want mDNS answer", host, src, ok)
	}
}

func TestLookupTimeout(t *testing.T) {
	r, _ := NewNameResolver(netip.MustParseAddr("192.168.1.100"))
	r.timeout = 50 * time.Millisecond
	r.dns = func(ctx context.Context, ip netip.Addr) (string, bool) { <-ctx.Done(); return "", false }
	r.mdns = func(ctx context.Context, ip netip.Addr) (string, bool) { <-ctx.Done(); return "", false }
	start := time.Now()
	r.Lookup(context.Background(), netip.MustParseAddr("192.168.1.20"))
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Errorf("lookup took %v; each lookup has a bounded budget", d)
	}
}

func TestMDNSQueryAndAnswer(t *testing.T) {
	ip := netip.MustParseAddr("192.168.1.50")
	q, id, err := buildPTRQuery(ip)
	if err != nil {
		t.Fatal(err)
	}
	var p dnsmessage.Parser
	h, err := p.Start(q)
	if err != nil {
		t.Fatal(err)
	}
	question, err := p.Question()
	if err != nil {
		t.Fatal(err)
	}
	if h.ID != id || question.Name.String() != "50.1.168.192.in-addr.arpa." || question.Type != dnsmessage.TypePTR {
		t.Errorf("query = id %d %s %v", h.ID, question.Name, question.Type)
	}

	// A recorded-style answer, as an mDNS responder sends it.
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: id, Response: true, Authoritative: true})
	b.StartAnswers()
	b.PTRResource(dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName("50.1.168.192.in-addr.arpa."), Class: dnsmessage.ClassINET, TTL: 120},
		dnsmessage.PTRResource{PTR: dnsmessage.MustNewName("phone.local.")})
	answer, err := b.Finish()
	if err != nil {
		t.Fatal(err)
	}
	host, ok := parsePTRAnswer(answer, id)
	if !ok || host != "phone.local" {
		t.Errorf("parsePTRAnswer = %q, %v", host, ok)
	}
	if _, ok := parsePTRAnswer(answer, id+1); ok {
		t.Error("answer with a different id accepted")
	}
}
