package ports

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// udpProbe is a UDP service that answers a standard, credential-free request (research R3).
// SNMP is deliberately absent: every SNMP request carries a community string, a login guess.
type udpProbe struct {
	port    int
	request func(ip netip.Addr) []byte
}

var udpProbes = []udpProbe{
	{53, func(netip.Addr) []byte { return dnsQuery(".", dnsmessage.TypeNS) }},
	{123, func(netip.Addr) []byte { return ntpRequest() }},
	{137, func(netip.Addr) []byte { return netbiosNodeStatus() }},
	{1900, ssdpSearch},
	{5353, func(ip netip.Addr) []byte {
		a := ip.As4()
		return dnsQuery(fmt.Sprintf("%d.%d.%d.%d.in-addr.arpa.", a[3], a[2], a[1], a[0]), dnsmessage.TypePTR)
	}},
}

func dnsQuery(name string, typ dnsmessage.Type) []byte {
	n, err := dnsmessage.NewName(name)
	if err != nil {
		return nil
	}
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: 0x4e45})
	b.StartQuestions()
	b.Question(dnsmessage.Question{Name: n, Type: typ, Class: dnsmessage.ClassINET})
	msg, _ := b.Finish()
	return msg
}

// ntpRequest is an NTP v4 client request (LI 0, VN 4, mode 3).
func ntpRequest() []byte {
	b := make([]byte, 48)
	b[0] = 0x23
	return b
}

// netbiosNodeStatus is an NBSTAT query for the wildcard name "*" (RFC 1002 4.2.17).
func netbiosNodeStatus() []byte {
	b := []byte{0x4e, 0x45, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0x20}
	name := [16]byte{'*'}
	for _, c := range name {
		b = append(b, 'A'+c>>4, 'A'+c&0x0f)
	}
	return append(b, 0, 0, 0x21, 0, 1)
}

func ssdpSearch(ip netip.Addr) []byte {
	return []byte("M-SEARCH * HTTP/1.1\r\nHOST: " + ip.String() + ":1900\r\nMAN: \"ssdp:discover\"\r\nMX: 1\r\nST: ssdp:all\r\n\r\n")
}

func exchangeUDP(ctx context.Context, addr netip.AddrPort, req []byte, timeout time.Duration) ([]byte, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp4", addr.String())
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	deadline := time.Now().Add(timeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	conn.SetDeadline(deadline)
	if _, err := conn.Write(req); err != nil {
		return nil, err
	}
	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}
