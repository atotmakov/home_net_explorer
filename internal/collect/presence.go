package collect

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// PresencePorts are tried with TCP connect when a routed host doesn't answer ICMP (research R3):
// web, HTTPS, SMB, SSH and the iPhone sync port.
var PresencePorts = []int{80, 443, 445, 22, 62078}

// PresenceTimeout bounds each TCP connect probe.
const PresenceTimeout = 500 * time.Millisecond

// presenceProber checks routed hosts: ICMP echo first, then TCP connects. A refused
// connection (RST) also proves the host is there.
type presenceProber struct {
	echo func(ctx context.Context, ip netip.Addr) bool // platform ICMP echo; nil = TCP only
}

// NewPresenceProber returns the platform's routed-subnet prober.
func NewPresenceProber() PresenceProber { return presenceProber{echo: icmpEcho} }

// Present implements PresenceProber.
func (p presenceProber) Present(ctx context.Context, ip netip.Addr) (string, bool) {
	if p.echo != nil && p.echo(ctx, ip) {
		return contract.ObsICMP, true
	}
	if tcpPresent(ctx, ip) {
		return contract.ObsTCP, true
	}
	return "", false
}

func tcpPresent(ctx context.Context, ip netip.Addr) bool {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	found := make(chan bool, len(PresencePorts))
	d := net.Dialer{Timeout: PresenceTimeout}
	for _, port := range PresencePorts {
		go func(port int) {
			conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), strconv.Itoa(port)))
			if err == nil {
				conn.Close()
				found <- true
				return
			}
			// A refused connection (RST) means a host answered. Windows reports it as WSAECONNREFUSED,
			// which does not match syscall.ECONNREFUSED, hence the message check.
			found <- errors.Is(err, syscall.ECONNREFUSED) || strings.Contains(err.Error(), "refused")
		}(port)
	}
	for range PresencePorts {
		if <-found {
			return true
		}
	}
	return false
}
