//go:build hwtest && linux

package collect

import (
	"context"
	"net/netip"
	"testing"
	"time"
)

// Runs only on real hardware with CAP_NET_RAW: make hwtest (T046).
func TestARPFindsDefaultGateway(t *testing.T) {
	v, err := SystemRoutes{RoutePath: "/proc/net/route"}.Vantage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, rt := range v.Routes {
		p, _ := netip.ParsePrefix(rt.Destination)
		if p.Bits() != 0 || rt.NextHop == "" {
			continue
		}
		gw := netip.MustParseAddr(rt.NextHop)
		prober := NewARPProber()
		defer prober.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		mac, ok, err := prober.Probe(ctx, rt.Interface, gw)
		if err != nil || !ok {
			t.Fatalf("ARP to gateway %s on %s: ok=%v err=%v", gw, rt.Interface, ok, err)
		}
		t.Logf("gateway %s is at %s", gw, mac)
		return
	}
	t.Skip("no default route")
}
