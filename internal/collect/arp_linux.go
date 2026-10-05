//go:build linux

package collect

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/mdlayher/arp"
)

// ARPProber sends raw ARP requests (needs CAP_NET_RAW) from the interface that owns the target
// subnet. One reader goroutine per interface dispatches replies to waiting probes, so many
// probes can be in flight at once.
type ARPProber struct {
	Timeout time.Duration // per attempt (default 300ms)
	Retries int           // extra attempts after the first (default 1)
	Rate    time.Duration // minimum gap between requests (default 5ms → ≤ 200/s)

	mu       sync.Mutex
	clients  map[string]*arpClient
	sendMu   sync.Mutex
	lastSend time.Time
}

type arpClient struct {
	c       *arp.Client
	mu      sync.Mutex
	waiters map[netip.Addr][]chan net.HardwareAddr
	writeMu sync.Mutex
}

// NewARPProber returns a prober with the defaults of research R2.
func NewARPProber() *ARPProber {
	return &ARPProber{Timeout: 300 * time.Millisecond, Retries: 1, Rate: 5 * time.Millisecond, clients: map[string]*arpClient{}}
}

// Probe implements Prober.
func (p *ARPProber) Probe(ctx context.Context, iface string, ip netip.Addr) (string, bool, error) {
	cl, err := p.client(iface)
	if err != nil {
		return "", false, err
	}
	for attempt := 0; attempt <= p.Retries; attempt++ {
		ch := cl.wait(ip)
		if err := p.pace(ctx); err != nil {
			cl.cancel(ip, ch)
			return "", false, err
		}
		cl.writeMu.Lock()
		err := cl.c.Request(ip)
		cl.writeMu.Unlock()
		if err != nil {
			cl.cancel(ip, ch)
			return "", false, err
		}
		select {
		case hw := <-ch:
			return strings.ToLower(hw.String()), true, nil
		case <-time.After(p.Timeout):
			cl.cancel(ip, ch)
		case <-ctx.Done():
			cl.cancel(ip, ch)
			return "", false, ctx.Err()
		}
	}
	return "", false, nil
}

// Close releases the raw sockets.
func (p *ARPProber) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for name, cl := range p.clients {
		cl.c.Close()
		delete(p.clients, name)
	}
	return nil
}

func (p *ARPProber) pace(ctx context.Context) error {
	p.sendMu.Lock()
	defer p.sendMu.Unlock()
	if wait := time.Until(p.lastSend.Add(p.Rate)); wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	p.lastSend = time.Now()
	return nil
}

func (p *ARPProber) client(iface string) (*arpClient, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if cl, ok := p.clients[iface]; ok {
		return cl, nil
	}
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		return nil, err
	}
	c, err := arp.Dial(ifi)
	if err != nil {
		return nil, err
	}
	cl := &arpClient{c: c, waiters: map[netip.Addr][]chan net.HardwareAddr{}}
	p.clients[iface] = cl
	go cl.read()
	return cl, nil
}

func (cl *arpClient) wait(ip netip.Addr) chan net.HardwareAddr {
	ch := make(chan net.HardwareAddr, 1)
	cl.mu.Lock()
	cl.waiters[ip] = append(cl.waiters[ip], ch)
	cl.mu.Unlock()
	return ch
}

func (cl *arpClient) cancel(ip netip.Addr, ch chan net.HardwareAddr) {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	ws := cl.waiters[ip]
	for i, w := range ws {
		if w == ch {
			cl.waiters[ip] = append(ws[:i], ws[i+1:]...)
			break
		}
	}
	if len(cl.waiters[ip]) == 0 {
		delete(cl.waiters, ip)
	}
}

// read dispatches ARP replies until the client is closed.
func (cl *arpClient) read() {
	for {
		pkt, _, err := cl.c.Read()
		if err != nil {
			if isClosed(err) {
				return
			}
			continue
		}
		if pkt.Operation != arp.OperationReply {
			continue
		}
		cl.mu.Lock()
		ws := cl.waiters[pkt.SenderIP]
		delete(cl.waiters, pkt.SenderIP)
		cl.mu.Unlock()
		for _, w := range ws {
			w <- pkt.SenderHardwareAddr
		}
	}
}

func isClosed(err error) bool {
	return strings.Contains(err.Error(), "closed")
}
