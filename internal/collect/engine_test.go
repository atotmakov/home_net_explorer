package collect_test

import (
	"context"
	"net/netip"
	"regexp"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/clock"
	"github.com/atotmakov/home_net_explorer/internal/collect"
	"github.com/atotmakov/home_net_explorer/internal/collect/collecttest"
	"github.com/atotmakov/home_net_explorer/internal/contract"
)

var t0 = time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)

func newEngine(net *collecttest.FakeNetwork, routes *collecttest.FakeRoutes) *collect.Engine {
	return &collect.Engine{
		Prober:          net,
		Presence:        net,
		Neighbors:       net,
		Routes:          routes,
		Resolver:        net,
		Clock:           clock.NewFake(t0),
		Collector:       contract.CollectorInfo{Name: "nas", Version: "test", OS: "linux"},
		IntervalSeconds: 900,
	}
}

func subnet(run *contract.CollectionRun, cidr string) (contract.SubnetScan, bool) {
	for _, s := range run.Subnets {
		if s.CIDR == cidr {
			return s, true
		}
	}
	return contract.SubnetScan{}, false
}

func TestScanHomeSubnet(t *testing.T) {
	net := collecttest.NewFakeNetwork().
		Add("192.168.1.100", collecttest.Host{MAC: "a0:b1:c2:d3:e4:f5", Hostname: "router.lan"}).
		Add("192.168.1.50", collecttest.Host{MAC: "da:a1:19:01:02:03", Hostname: "phone.local", HostnameSource: contract.HostnameSourceMDNS}).
		Add("192.168.1.77", collecttest.Host{MAC: "3c:22:fb:44:55:66", Silent: true})
	net.Neighbors = []collect.Neighbor{{IP: netip.MustParseAddr("192.168.1.77"), MAC: "3c:22:fb:44:55:66"}}
	net.ProbeDelay = time.Millisecond
	e := newEngine(net, collecttest.NewFakeRoutes("192.168.1.100", "eth0=192.168.1.20/24"))

	run, err := e.Scan(context.Background(), collect.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(run.CollectionID) {
		t.Errorf("collection_id %q is not a UUIDv4", run.CollectionID)
	}
	s, ok := subnet(run, "192.168.1.0/24")
	if !ok || s.Method != contract.MethodARP || !s.Complete || s.HostsProbed != 254 {
		t.Errorf("subnet scan = %+v, want arp/complete/254", s)
	}
	if net.MaxInflight > 64 {
		t.Errorf("max concurrent probes = %d, want <= 64", net.MaxInflight)
	}
	if net.MaxInflight < 2 {
		t.Errorf("probes ran sequentially (max inflight %d)", net.MaxInflight)
	}

	byIP := map[string]contract.Observation{}
	for _, o := range run.Observations {
		byIP[o.IP] = o
	}
	if o := byIP["192.168.1.100"]; o.MAC != "a0:b1:c2:d3:e4:f5" || o.Method != contract.ObsARP || o.Hostname != "router.lan" || o.HostnameSource != contract.HostnameSourceDNS {
		t.Errorf("router observation = %+v", o)
	}
	if o := byIP["192.168.1.50"]; o.HostnameSource != contract.HostnameSourceMDNS {
		t.Errorf("mDNS hostname source lost: %+v", o)
	}
	if o, ok := byIP["192.168.1.77"]; !ok || o.Method != contract.ObsNeighborCache {
		t.Errorf("silent host from the neighbor cache = %+v (found %v), want method neighbor_cache", o, ok)
	}
	for _, o := range run.Observations {
		if o.ObservedAt.Before(run.StartedAt) || o.ObservedAt.After(run.FinishedAt) {
			t.Errorf("observation %s outside the run window", o.IP)
		}
	}
	if err := contract.Validate(run); err != nil {
		t.Errorf("engine output fails validation: %v", err)
	}
}

func TestAutoTargets(t *testing.T) {
	net := collecttest.NewFakeNetwork().
		Add("192.168.1.100", collecttest.Host{MAC: "a0:b1:c2:d3:e4:f5"}).
		Add("10.20.30.1", collecttest.Host{MAC: "e4:8d:8c:01:02:03"})
	routes := collecttest.NewFakeRoutes("192.168.1.100",
		"eth0=192.168.1.20/24", "usb=10.20.30.15/24", "lab=10.0.0.5/16", "wan=203.0.113.9/24")
	e := newEngine(net, routes)

	run, err := e.Scan(context.Background(), collect.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, cidr := range []string{"192.168.1.0/24", "10.20.30.0/24"} {
		if s, ok := subnet(run, cidr); !ok || s.Method != contract.MethodARP {
			t.Errorf("%s not scanned: %+v", cidr, s)
		}
	}
	if s, ok := subnet(run, "10.0.0.0/16"); !ok || s.Method != contract.MethodSkipped || s.SkipReason != contract.SkipTooLarge {
		t.Errorf("/16 should be reported skipped/too_large, got %+v (found %v)", s, ok)
	}
	if _, ok := subnet(run, "203.0.113.0/24"); ok {
		t.Error("a non-private subnet must never appear in a run")
	}
	if err := contract.Validate(run); err != nil {
		t.Errorf("engine output fails validation: %v", err)
	}

	// A new adapter appears: the next scan picks it up with no configuration change.
	routes.Set("192.168.1.100", "eth0=192.168.1.20/24", "new=10.99.0.2/24")
	run2, err := e.Scan(context.Background(), collect.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := subnet(run2, "10.99.0.0/24"); !ok {
		t.Error("newly attached subnet not scanned")
	}
	if _, ok := subnet(run2, "10.20.30.0/24"); ok {
		t.Error("detached subnet still scanned")
	}
}

func TestIgnoredSubnetsAreSkipped(t *testing.T) {
	net := collecttest.NewFakeNetwork().Add("10.20.30.1", collecttest.Host{MAC: "e4:8d:8c:01:02:03"})
	e := newEngine(net, collecttest.NewFakeRoutes("", "eth0=192.168.1.20/24", "usb=10.20.30.15/24"))
	run, err := e.Scan(context.Background(), collect.ScanOptions{Ignored: []netip.Prefix{netip.MustParsePrefix("10.20.30.0/24")}})
	if err != nil {
		t.Fatal(err)
	}
	s, ok := subnet(run, "10.20.30.0/24")
	if !ok || s.Method != contract.MethodSkipped || s.SkipReason != contract.SkipIgnored {
		t.Errorf("ignored subnet = %+v, want skipped/ignored", s)
	}
	for _, o := range run.Observations {
		if netip.MustParsePrefix("10.20.30.0/24").Contains(netip.MustParseAddr(o.IP)) {
			t.Errorf("ignored subnet was probed: %+v", o)
		}
	}
}

func TestCancelledScanIsIncomplete(t *testing.T) {
	net := collecttest.NewFakeNetwork().Add("192.168.1.100", collecttest.Host{MAC: "a0:b1:c2:d3:e4:f5"})
	net.ProbeDelay = 20 * time.Millisecond
	e := newEngine(net, collecttest.NewFakeRoutes("", "eth0=192.168.1.20/24"))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	run, err := e.Scan(ctx, collect.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := subnet(run, "192.168.1.0/24"); s.Complete {
		t.Error("cancelled scan reported complete")
	}
	if err := contract.Validate(run); err != nil {
		t.Errorf("partial run fails validation: %v", err)
	}
}
