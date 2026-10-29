// Package ports checks which ports of a device accept connections (feature 015): every TCP port
// with a plain connect, and a few UDP services that answer a standard request. Checks are
// non-intrusive (001 FR-007): a connection attempt closed at once, or a credential-free query.
package ports

import (
	"cmp"
	"context"
	"errors"
	"net"
	"net/netip"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/clock"
	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// Defaults (research R2).
const (
	DefaultPerDevice     = 128 // connection attempts in flight per device
	DefaultDevices       = 4   // devices checked at once
	DefaultTimeout       = time.Second
	DefaultSilentTimeout = 300 * time.Millisecond // after the first 1,024 ports all time out
	DefaultBannerTimeout = 500 * time.Millisecond
	DefaultUDPTimeout    = time.Second
	silentWindow         = 1024
)

// Target is an address to check, with the MAC its presence scan found.
type Target struct {
	IP  netip.Addr
	MAC string
}

// DialFunc opens a TCP connection; a timeout error (net.Error.Timeout) means no answer.
type DialFunc func(ctx context.Context, addr netip.AddrPort, timeout time.Duration) (net.Conn, error)

// UDPFunc sends one request and returns the first reply.
type UDPFunc func(ctx context.Context, addr netip.AddrPort, req []byte, timeout time.Duration) ([]byte, error)

// Checker checks the ports of targets. Zero fields take the defaults; Dial and UDP default to
// the real network.
type Checker struct {
	Dial          DialFunc
	UDP           UDPFunc
	Clock         clock.Clock
	PerDevice     int
	Devices       int
	Timeout       time.Duration
	SilentTimeout time.Duration
	BannerTimeout time.Duration
	UDPTimeout    time.Duration
	MaxPort       int // highest TCP port checked; 65535 by default (tests use fewer)
	Done          func() // called after each target's check (progress); optional
}

func (c *Checker) defaults() Checker {
	d := *c
	if d.Dial == nil {
		d.Dial = dialTCP
	}
	if d.UDP == nil {
		d.UDP = exchangeUDP
	}
	if d.Clock == nil {
		d.Clock = clock.Real{}
	}
	if d.PerDevice <= 0 {
		d.PerDevice = DefaultPerDevice
	}
	if d.Devices <= 0 {
		d.Devices = DefaultDevices
	}
	if d.Timeout <= 0 {
		d.Timeout = DefaultTimeout
	}
	if d.SilentTimeout <= 0 {
		d.SilentTimeout = DefaultSilentTimeout
	}
	if d.BannerTimeout <= 0 {
		d.BannerTimeout = DefaultBannerTimeout
	}
	if d.UDPTimeout <= 0 {
		d.UDPTimeout = DefaultUDPTimeout
	}
	if d.MaxPort <= 0 || d.MaxPort > 65535 {
		d.MaxPort = 65535
	}
	return d
}

// Check checks every target, at most Devices at once, and returns one PortCheck per target in
// the same order. A cancelled context ends the checks early; those are not complete.
func (c *Checker) Check(ctx context.Context, targets []Target) []contract.PortCheck {
	d := c.defaults()
	out := make([]contract.PortCheck, len(targets))
	sem := make(chan struct{}, d.Devices)
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = d.checkOne(ctx, t)
			if d.Done != nil {
				d.Done()
			}
		}()
	}
	wg.Wait()
	return out
}

func (c *Checker) checkOne(ctx context.Context, t Target) contract.PortCheck {
	res := contract.PortCheck{IP: t.IP.String(), MAC: t.MAC, StartedAt: c.Clock.Now(), Open: []contract.OpenPort{}}
	var mu sync.Mutex
	add := func(o contract.OpenPort) {
		mu.Lock()
		res.Open = append(res.Open, o)
		mu.Unlock()
	}

	// A firewalled device times out on every port: the first 1,024 ports are checked as a batch,
	// and when they all timed out the rest are tried with the short timeout (research R2).
	slots := make(chan struct{}, c.PerDevice)
	var silent atomic.Int32
	sweep := func(from, to int, timeout time.Duration) bool {
		var wg sync.WaitGroup
		defer wg.Wait()
		for port := from; port <= to; port++ {
			select {
			case <-ctx.Done():
				return false
			case slots <- struct{}{}:
			}
			if ctx.Err() != nil {
				<-slots
				return false
			}
			wg.Add(1)
			go func(port int) {
				defer wg.Done()
				defer func() { <-slots }()
				o, open, timedOut := c.tcp(ctx, t.IP, port, timeout)
				if open {
					add(o)
				}
				if timedOut {
					silent.Add(1)
				}
			}(port)
		}
		return true
	}
	first := min(silentWindow, c.MaxPort)
	complete := sweep(1, first, c.Timeout)
	if complete && c.MaxPort > first {
		timeout := c.Timeout
		if silent.Load() == int32(first) {
			timeout = c.SilentTimeout
		}
		complete = sweep(first+1, c.MaxPort, timeout)
	}

	if complete && ctx.Err() == nil {
		var uwg sync.WaitGroup
		for _, p := range udpProbes {
			uwg.Add(1)
			go func() {
				defer uwg.Done()
				reply, err := c.UDP(ctx, netip.AddrPortFrom(t.IP, uint16(p.port)), p.request(t.IP), c.UDPTimeout)
				if err == nil && len(reply) > 0 {
					add(contract.OpenPort{Port: p.port, Proto: contract.ProtoUDP, Service: Service(contract.ProtoUDP, p.port),
						ServiceSource: contract.ServiceSourceTable})
				}
			}()
		}
		uwg.Wait()
	}
	if ctx.Err() != nil {
		complete = false
	}

	slices.SortFunc(res.Open, func(a, b contract.OpenPort) int {
		return cmp.Or(cmp.Compare(a.Port, b.Port), cmp.Compare(a.Proto, b.Proto))
	})
	if len(res.Open) > contract.MaxOpenPorts {
		res.Open = res.Open[:contract.MaxOpenPorts]
		res.AllOpen = complete
	}
	res.Complete = complete
	res.FinishedAt = c.Clock.Now()
	return res
}

// tcp tries one port: open (with its service), or not; timedOut tells silence from a refusal.
func (c *Checker) tcp(ctx context.Context, ip netip.Addr, port int, timeout time.Duration) (contract.OpenPort, bool, bool) {
	conn, err := c.Dial(ctx, netip.AddrPortFrom(ip, uint16(port)), timeout)
	if err != nil {
		var ne net.Error
		return contract.OpenPort{}, false, errors.As(err, &ne) && ne.Timeout()
	}
	defer conn.Close()
	o := contract.OpenPort{Port: port, Proto: contract.ProtoTCP, Service: Service(contract.ProtoTCP, port),
		ServiceSource: contract.ServiceSourceTable}
	if name := readBanner(conn, c.BannerTimeout); name != "" {
		o.Service, o.ServiceSource = name, contract.ServiceSourceBanner
	}
	return o, true, false
}

func dialTCP(ctx context.Context, addr netip.AddrPort, timeout time.Duration) (net.Conn, error) {
	d := net.Dialer{Timeout: timeout}
	return d.DialContext(ctx, "tcp4", addr.String())
}
