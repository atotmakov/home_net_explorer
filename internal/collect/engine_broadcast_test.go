package collect_test

import (
	"context"
	"net/netip"
	"testing"

	"github.com/atotmakov/home_net_explorer/internal/collect"
	"github.com/atotmakov/home_net_explorer/internal/collect/collecttest"
)

// Windows keeps the broadcast address (ff-ff-ff-ff-ff-ff) and multicast groups in its ARP cache.
// They are not devices, and 192.168.1.255 once showed on the Map as one.
func TestNeighborCacheSkipsBroadcastAndMulticast(t *testing.T) {
	net := collecttest.NewFakeNetwork().
		Add("192.168.1.100", collecttest.Host{MAC: "a0:b1:c2:d3:e4:f5"}).
		Add("192.168.1.77", collecttest.Host{MAC: "3c:22:fb:44:55:66", Silent: true})
	net.Neighbors = []collect.Neighbor{
		{IP: netip.MustParseAddr("192.168.1.77"), MAC: "3c:22:fb:44:55:66"},
		{IP: netip.MustParseAddr("192.168.1.255"), MAC: "ff:ff:ff:ff:ff:ff"},
		{IP: netip.MustParseAddr("192.168.1.0"), MAC: "3c:22:fb:44:55:00"},   // the network address
		{IP: netip.MustParseAddr("192.168.1.251"), MAC: "01:00:5e:00:00:fb"}, // a group MAC
	}
	e := newEngine(net, collecttest.NewFakeRoutes("192.168.1.100", "eth0=192.168.1.20/24"))
	run, err := e.Scan(context.Background(), collect.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, o := range run.Observations {
		got[o.IP] = true
	}
	if !got["192.168.1.77"] {
		t.Error("a real neighbor-cache entry is missing")
	}
	for _, ip := range []string{"192.168.1.255", "192.168.1.0", "192.168.1.251"} {
		if got[ip] {
			t.Errorf("%s is reported as a device", ip)
		}
	}
}

func TestNormalizeMACRejectsGroupAddresses(t *testing.T) {
	for _, mac := range []string{"ff:ff:ff:ff:ff:ff", "FF-FF-FF-FF-FF-FF", "01:00:5e:00:00:fb", "33:33:00:00:00:01"} {
		if m, ok := collect.NormalizeMAC(mac); ok {
			t.Errorf("NormalizeMAC(%q) = %q, ok; want rejected", mac, m)
		}
	}
	if m, ok := collect.NormalizeMAC("3C-22-FB-44-55-66"); !ok || m != "3c:22:fb:44:55:66" {
		t.Errorf("unicast MAC = %q, %v", m, ok)
	}
}
