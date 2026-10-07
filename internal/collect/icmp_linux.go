//go:build linux

package collect

import (
	"context"
	"math/rand/v2"
	"net"
	"net/netip"
	"os"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// icmpEcho sends one ICMP echo with a raw socket (needs CAP_NET_RAW, which the NAS container
// has for ARP anyway) and waits for the matching reply.
func icmpEcho(ctx context.Context, ip netip.Addr) bool {
	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return false
	}
	defer conn.Close()
	deadline := time.Now().Add(time.Second)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	conn.SetDeadline(deadline)

	id, seq := os.Getpid()&0xffff, rand.IntN(0xffff)
	msg := icmp.Message{Type: ipv4.ICMPTypeEcho, Body: &icmp.Echo{ID: id, Seq: seq, Data: []byte("hne")}}
	b, err := msg.Marshal(nil)
	if err != nil {
		return false
	}
	dst := &net.IPAddr{IP: net.IP(ip.AsSlice())}
	if _, err := conn.WriteTo(b, dst); err != nil {
		return false
	}
	buf := make([]byte, 1500)
	for {
		n, peer, err := conn.ReadFrom(buf)
		if err != nil {
			return false
		}
		if a, ok := peer.(*net.IPAddr); !ok || !a.IP.Equal(dst.IP) {
			continue
		}
		reply, err := icmp.ParseMessage(1, buf[:n])
		if err != nil || reply.Type != ipv4.ICMPTypeEchoReply {
			continue
		}
		if e, ok := reply.Body.(*icmp.Echo); ok && e.Seq == seq {
			return true
		}
	}
}
