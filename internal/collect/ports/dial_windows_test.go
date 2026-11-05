package ports

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"
)

// Windows answers a refused connect only after retrying it for about 2 s, so with the 1 s timeout
// every closed port looked silent and a full sweep took 34 minutes (gate G1). Loopback only: this
// is the one behaviour a fake dialer cannot show.
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
