package ports

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/atotmakov/home_net_explorer/internal/clock"
	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// Feature 015 (research R1–R4): the checker connects to every TCP port of each target and sends
// a standard request to a few UDP services. The network is faked: no test touches a real socket.

var t0 = time.Date(2026, 10, 27, 3, 12, 0, 0, time.UTC)

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// fakeNet answers TCP connects from a table: open ports connect (and may send a banner),
// refused ports fail at once, every other port times out.
type fakeNet struct {
	mu       sync.Mutex
	open     map[netip.Addr]map[int]string // port → banner ("" = says nothing)
	refused  func(ip netip.Addr, port int) bool
	inflight map[netip.Addr]int
	peakDev  int // most dials in flight to one device
	peakIPs  int // most devices with a dial in flight
	timeouts map[int]time.Duration
	dials    int
	onDial   func(n int)
	udpReqs  map[int][]byte
	udpOpen  map[int]bool
}

func newFakeNet() *fakeNet {
	return &fakeNet{open: map[netip.Addr]map[int]string{}, inflight: map[netip.Addr]int{},
		timeouts: map[int]time.Duration{}, udpReqs: map[int][]byte{}, udpOpen: map[int]bool{},
		refused: func(netip.Addr, int) bool { return true }}
}

func (f *fakeNet) dial(ctx context.Context, addr netip.AddrPort, timeout time.Duration) (net.Conn, error) {
	ip, port := addr.Addr(), int(addr.Port())
	f.mu.Lock()
	f.dials++
	n := f.dials
	f.inflight[ip]++
	if f.inflight[ip] > f.peakDev {
		f.peakDev = f.inflight[ip]
	}
	busy := 0
	for _, c := range f.inflight {
		if c > 0 {
			busy++
		}
	}
	if busy > f.peakIPs {
		f.peakIPs = busy
	}
	if _, seen := f.timeouts[port]; !seen || timeout < f.timeouts[port] {
		f.timeouts[port] = timeout
	}
	banner, isOpen := f.open[ip][port]
	refused := f.refused(ip, port)
	hook := f.onDial
	f.mu.Unlock()
	time.Sleep(20 * time.Microsecond) // let dials overlap so the peaks mean something
	defer func() {
		f.mu.Lock()
		f.inflight[ip]--
		f.mu.Unlock()
	}()
	if hook != nil {
		hook(n)
	}
	switch {
	case isOpen:
		client, server := net.Pipe()
		go func() {
			if banner != "" {
				server.Write([]byte(banner))
			}
			buf := make([]byte, 64)
			server.Read(buf) // until the checker closes its end
			server.Close()
		}()
		return client, nil
	case refused:
		return nil, errors.New("connection refused")
	default:
		return nil, timeoutErr{}
	}
}

func (f *fakeNet) udp(ctx context.Context, addr netip.AddrPort, req []byte, timeout time.Duration) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.udpReqs[int(addr.Port())] = append([]byte(nil), req...)
	if f.udpOpen[int(addr.Port())] {
		return []byte{1}, nil
	}
	return nil, timeoutErr{}
}

func (f *fakeNet) checker(maxPort int) *Checker {
	return &Checker{Dial: f.dial, UDP: f.udp, Clock: clock.NewFake(t0), MaxPort: maxPort,
		BannerTimeout: 20 * time.Millisecond}
}

func target(last byte) Target {
	return Target{IP: netip.AddrFrom4([4]byte{192, 168, 1, last}), MAC: fmt.Sprintf("00:00:5e:00:53:%02x", last)}
}

func openList(c contract.PortCheck) []string {
	var out []string
	for _, o := range c.Open {
		out = append(out, fmt.Sprintf("%d/%s %s %s", o.Port, o.Proto, o.Service, o.ServiceSource))
	}
	return out
}

func TestCheckTCP(t *testing.T) {
	f := newFakeNet()
	var targets []Target
	for i := byte(1); i <= 6; i++ {
		tg := target(i)
		targets = append(targets, tg)
		f.open[tg.IP] = map[int]string{22: "", 80: "", 554: ""}
	}
	res := f.checker(4096).Check(context.Background(), targets)
	if len(res) != 6 {
		t.Fatalf("%d checks, want 6", len(res))
	}
	for i, c := range res {
		if c.IP != targets[i].IP.String() || c.MAC != targets[i].MAC {
			t.Errorf("check %d is for %s %s, want %s %s", i, c.IP, c.MAC, targets[i].IP, targets[i].MAC)
		}
		if !c.Complete || c.AllOpen {
			t.Errorf("%s: complete=%v all_open=%v, want complete only", c.IP, c.Complete, c.AllOpen)
		}
		want := "22/tcp ssh table,80/tcp http table,554/tcp rtsp table"
		if got := strings.Join(openList(c), ","); got != want {
			t.Errorf("%s: open = %s, want %s", c.IP, got, want)
		}
		if !c.StartedAt.Equal(t0) || !c.FinishedAt.Equal(t0) {
			t.Errorf("%s: times %v–%v, want the clock's", c.IP, c.StartedAt, c.FinishedAt)
		}
	}
	if f.peakDev > 128 || f.peakDev == 0 {
		t.Errorf("peak dials to one device = %d, want 1..128", f.peakDev)
	}
	if f.peakIPs > 4 || f.peakIPs == 0 {
		t.Errorf("peak devices at once = %d, want 1..4", f.peakIPs)
	}
	if f.timeouts[100] != time.Second {
		t.Errorf("dial timeout = %v, want 1s", f.timeouts[100])
	}
}

// A firewalled device: after its first 1,024 ports time out, the rest use 300 ms (research R2).
func TestSilentDeviceShortTimeout(t *testing.T) {
	f := newFakeNet()
	f.refused = func(netip.Addr, int) bool { return false }
	res := f.checker(65535).Check(context.Background(), []Target{target(9)})
	if len(res) != 1 || !res[0].Complete || len(res[0].Open) != 0 {
		t.Fatalf("silent device = %+v, want a complete check with nothing open", res)
	}
	for _, p := range []int{1, 500, 1024} {
		if f.timeouts[p] != time.Second {
			t.Errorf("port %d timeout = %v, want 1s", p, f.timeouts[p])
		}
	}
	for _, p := range []int{2049, 30000, 65535} {
		if f.timeouts[p] != 300*time.Millisecond {
			t.Errorf("port %d timeout = %v, want 300ms", p, f.timeouts[p])
		}
	}
	if f.dials < 65535 {
		t.Errorf("%d dials, want every port", f.dials)
	}
}

// A device that accepts every connection: the list stops at 512 and says so (research R2).
func TestAllOpen(t *testing.T) {
	f := newFakeNet()
	tg := target(7)
	f.open[tg.IP] = map[int]string{}
	for p := 1; p <= 600; p++ {
		f.open[tg.IP][p] = ""
	}
	res := f.checker(4096).Check(context.Background(), []Target{tg})
	c := res[0]
	if !c.Complete || !c.AllOpen || len(c.Open) != 512 {
		t.Fatalf("complete=%v all_open=%v open=%d, want true true 512", c.Complete, c.AllOpen, len(c.Open))
	}
	if c.Open[0].Port != 1 || c.Open[511].Port != 512 {
		t.Errorf("listed %d..%d, want the lowest 512 ports", c.Open[0].Port, c.Open[511].Port)
	}
}

// A cancelled check is incomplete and keeps what it found (it may never close a port, FR-005).
func TestCancelled(t *testing.T) {
	f := newFakeNet()
	tg := target(5)
	f.open[tg.IP] = map[int]string{22: "", 80: ""}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.onDial = func(n int) {
		if n == 2000 {
			cancel()
		}
	}
	res := f.checker(65535).Check(ctx, []Target{tg})
	c := res[0]
	if c.Complete {
		t.Error("a cancelled check is complete")
	}
	if got := strings.Join(openList(c), ","); got != "22/tcp ssh table,80/tcp http table" {
		t.Errorf("open = %s, want the ports found before the cancel", got)
	}
	if f.dials > 2000+128 {
		t.Errorf("%d dials after the cancel at 2000", f.dials)
	}
}

// UDP: only services that answer a standard request without credentials (research R3).
func TestUDPProbes(t *testing.T) {
	f := newFakeNet()
	f.udpOpen[53], f.udpOpen[1900] = true, true
	tg := target(1)
	res := f.checker(16).Check(context.Background(), []Target{tg})
	if got := strings.Join(openList(res[0]), ","); got != "53/udp dns table,1900/udp ssdp table" {
		t.Errorf("open = %s", got)
	}
	if _, asked := f.udpReqs[161]; asked {
		t.Error("SNMP was asked: every SNMP request carries a community string (a login guess)")
	}
	if len(f.udpReqs) != 5 {
		t.Errorf("UDP requests to %d ports, want 5 (53, 123, 137, 1900, 5353)", len(f.udpReqs))
	}
	question := func(port int) dnsmessage.Question {
		var p dnsmessage.Parser
		if _, err := p.Start(f.udpReqs[port]); err != nil {
			t.Fatalf("%d: not a DNS message: %v", port, err)
		}
		q, err := p.Question()
		if err != nil {
			t.Fatalf("%d: no question: %v", port, err)
		}
		return q
	}
	if q := question(53); q.Name.String() != "." || q.Type != dnsmessage.TypeNS {
		t.Errorf("DNS request = %v %v, want the root NS", q.Name, q.Type)
	}
	if q := question(5353); q.Type != dnsmessage.TypePTR || q.Name.String() != "1.1.168.192.in-addr.arpa." {
		t.Errorf("mDNS request = %v %v, want the device's reverse name", q.Name, q.Type)
	}
	if ntp := f.udpReqs[123]; len(ntp) != 48 || ntp[0] != 0x23 {
		t.Errorf("NTP request = % x, want a 48-byte client request (0x23)", ntp)
	}
	if nb := f.udpReqs[137]; len(nb) != 50 || nb[12] != 0x20 || nb[13] != 'C' || nb[14] != 'K' || nb[47] != 0x21 {
		t.Errorf("NetBIOS request = % x, want a node status query for *", nb)
	}
	if !strings.HasPrefix(string(f.udpReqs[1900]), "M-SEARCH * HTTP/1.1\r\n") {
		t.Errorf("SSDP request = %q", f.udpReqs[1900])
	}
}

// A greeting the device sends by itself names the service; its text is never kept (R4).
func TestBanner(t *testing.T) {
	f := newFakeNet()
	tg := target(3)
	f.open[tg.IP] = map[int]string{22: "SSH-2.0-OpenSSH_9.6\r\n", 2222: "SSH-2.0-dropbear\r\n", 80: ""}
	res := f.checker(4096).Check(context.Background(), []Target{tg})
	want := "22/tcp ssh banner,80/tcp http table,2222/tcp ssh banner"
	if got := strings.Join(openList(res[0]), ","); got != want {
		t.Errorf("open = %s, want %s", got, want)
	}
	if s := fmt.Sprintf("%+v", res); strings.Contains(s, "OpenSSH") || strings.Contains(s, "dropbear") {
		t.Errorf("the banner text is kept: %s", s)
	}
}
