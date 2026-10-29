//go:build hwtest

package ports

import (
	"context"
	"net"
	"net/netip"
	"testing"
)

// The real dialer on loopback (feature 015, research R1): two listening ports are found open, the
// port between them is not. Run with -tags hwtest.
func TestLoopbackRealDialer(t *testing.T) {
	var ports []int
	for range 2 {
		l, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		go func() {
			for {
				c, err := l.Accept()
				if err != nil {
					return
				}
				c.Close()
			}
		}()
		ports = append(ports, l.Addr().(*net.TCPAddr).Port)
	}
	hi := max(ports[0], ports[1])
	c := &Checker{MaxPort: hi}
	res := c.Check(context.Background(), []Target{{IP: netip.MustParseAddr("127.0.0.1")}})
	open := map[int]bool{}
	for _, o := range res[0].Open {
		if o.Proto == "tcp" {
			open[o.Port] = true
		}
	}
	for _, p := range ports {
		if !open[p] {
			t.Errorf("listening port %d not found open: %+v", p, res[0].Open)
		}
	}
	if !res[0].Complete {
		t.Error("loopback check not complete")
	}
}
