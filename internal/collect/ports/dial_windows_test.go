package ports

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"syscall"
	"testing"
	"time"
)

// The owner's Windows 11 desktop answers a refused connect only after retrying it for about 2 s,
// so with the 1 s timeout every closed port looked silent and a full sweep took 34 minutes (gate
// G1). The CI runner refuses fast even without the fix, so TestDialRefusedFast cannot catch that
// slowdown there; this test guards the wiring, and the desktop run is the evidence for the speed.
func TestDialerTurnsOffSynRetransmit(t *testing.T) {
	if dialControl == nil {
		t.Fatal("the dialer leaves SYN retransmission on")
	}
}

// Windows must accept the setting: dialControl ignores a refusal, so only this test would see one.
func TestWindowsAcceptsNoSynRetransmit(t *testing.T) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	d := net.Dialer{Timeout: DefaultTimeout, Control: func(_, _ string, c syscall.RawConn) error {
		return setNoSynRetransmit(c)
	}}
	conn, err := d.Dial("tcp4", l.Addr().String())
	if err != nil {
		t.Fatalf("dial with the setting: %v", err)
	}
	conn.Close()
}

func TestDialRefusedFast(t *testing.T) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := netip.MustParseAddrPort(l.Addr().String())
	l.Close()

	start := time.Now()
	conn, err := dialTCP(context.Background(), addr, DefaultTimeout)
	elapsed := time.Since(start)
	if err == nil {
		conn.Close()
		t.Fatal("connected to a closed port")
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		t.Errorf("closed port timed out after %v, want refused: %v", elapsed, err)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("refused after %v, want under 500ms", elapsed)
	}
}

func TestDialOpenPort(t *testing.T) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	conn, err := dialTCP(context.Background(), netip.MustParseAddrPort(l.Addr().String()), DefaultTimeout)
	if err != nil {
		t.Fatalf("open port: %v", err)
	}
	conn.Close()
}
