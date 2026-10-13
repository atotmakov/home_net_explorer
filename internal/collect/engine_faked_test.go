package collect_test

import (
	"context"
	"fmt"
	"net/netip"
	"testing"

	"github.com/atotmakov/home_net_explorer/internal/collect"
	"github.com/atotmakov/home_net_explorer/internal/collect/collecttest"
	"github.com/atotmakov/home_net_explorer/internal/collect/router"
	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// Feature 005: a network path that answers for every address (e.g. a VPN tunnel holding the
// default route) must not turn a routed subnet into hundreds of devices.

func scanRouted(t *testing.T, e *collect.Engine, opts collect.ScanOptions) *contract.CollectionRun {
	t.Helper()
	run, err := e.Scan(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := contract.Validate(run); err != nil {
		t.Errorf("run fails validation: %v", err)
	}
	return run
}

func routedObservations(run *contract.CollectionRun, prefix string) int {
	p := prefixes(prefix)[0]
	n := 0
	for _, o := range run.Observations {
		if p.Contains(netip.MustParseAddr(o.IP)) {
			n++
		}
	}
	return n
}

func wantFaked(t *testing.T, run *contract.CollectionRun, cidr string) {
	t.Helper()
	s, ok := subnet(run, cidr)
	if !ok || s.Method != contract.MethodSkipped || s.SkipReason != contract.SkipFakedAnswers || s.Complete || s.HostsProbed != 0 {
		t.Errorf("%s = %+v, want skipped/faked_answers", cidr, s)
	}
	if n := routedObservations(run, cidr); n != 0 {
		t.Errorf("%s: %d observations recorded from faked answers", cidr, n)
	}
}

func TestCanaryAnswerSkipsDefaultRouteSubnet(t *testing.T) {
	net := collecttest.NewFakeNetwork().Add("192.168.1.20", collecttest.Host{MAC: "00:11:32:aa:bb:cc"})
	net.AnswerAll = true
	e := newEngine(net, collecttest.NewFakeRoutes("172.18.0.2", "happ-tun=172.18.0.1/30", "home=192.168.1.10/24"))
	run := scanRouted(t, e, collect.ScanOptions{Targets: prefixes("192.168.0.0/24", "192.168.1.0/24")})
	wantFaked(t, run, "192.168.0.0/24")
	if net.Probes > 1+254 { // the canary and the on-link ARP scan; the routed subnet is not probed
		t.Errorf("probes = %d: the routed subnet was probed although the canary answered", net.Probes)
	}
	if s, _ := subnet(run, "192.168.1.0/24"); s.Method != contract.MethodARP || !s.Complete {
		t.Errorf("on-link subnet = %+v, want an unaffected ARP scan", s)
	}
	if o := observationsByIP(run)["192.168.1.20"]; len(o) != 1 {
		t.Error("the on-link host was lost")
	}
}

func TestSaturatedRoutedSubnetIsFaked(t *testing.T) {
	net := collecttest.NewFakeNetwork()
	for i := 1; i <= 240; i++ { // 240 of 254 answer; the canary does not
		net.Add(fmt.Sprintf("192.168.50.%d", i), collecttest.Host{})
	}
	e := newEngine(net, collecttest.NewFakeRoutes("192.168.1.100", "eth0=192.168.1.20/24"))
	run := scanRouted(t, e, collect.ScanOptions{Targets: prefixes("192.168.50.0/24")})
	wantFaked(t, run, "192.168.50.0/24")
}

func TestOwnRouteSubnetIgnoresCanary(t *testing.T) {
	net := collecttest.NewFakeNetwork().Add("192.0.2.1", collecttest.Host{}).Add("10.20.30.5", collecttest.Host{})
	routes := collecttest.NewFakeRoutes("172.18.0.2", "happ-tun=172.18.0.1/30", "home=192.168.1.10/24")
	routes.AddRoute("10.20.30.0/24", "192.168.1.1", "home") // its own route, not the tunnel
	e := newEngine(net, routes)
	run := scanRouted(t, e, collect.ScanOptions{Targets: prefixes("10.20.30.0/24")})
	if s, _ := subnet(run, "10.20.30.0/24"); s.Method != contract.MethodICMPTCP || !s.Complete {
		t.Errorf("own-route subnet = %+v, want a normal icmp_tcp scan", s)
	}
	if o := observationsByIP(run)["10.20.30.5"]; len(o) != 1 {
		t.Error("the host on the own-route subnet was not recorded")
	}
}

func TestNormalRoutedScanUnchanged(t *testing.T) {
	net := collecttest.NewFakeNetwork().Add("192.168.50.1", collecttest.Host{}).Add("192.168.50.7", collecttest.Host{TCPOnly: true})
	e := newEngine(net, collecttest.NewFakeRoutes("192.168.1.100", "eth0=192.168.1.20/24"))
	run := scanRouted(t, e, collect.ScanOptions{Targets: prefixes("192.168.50.0/24")})
	if s, _ := subnet(run, "192.168.50.0/24"); s.Method != contract.MethodICMPTCP || !s.Complete || s.HostsProbed != 254 {
		t.Errorf("routed subnet = %+v", s)
	}
	if n := routedObservations(run, "192.168.50.0/24"); n != 2 {
		t.Errorf("observations = %d, want 2", n)
	}
	if e.PathFakesAnswers(context.Background()) {
		t.Error("PathFakesAnswers on a normal path = true")
	}
}

func TestRouterFallbackFaked(t *testing.T) {
	net := collecttest.NewFakeNetwork()
	net.AnswerAll = true
	bad := collecttest.NewFakeRouter("192.168.0.1", "192.168.0.0/24", router.Result{Outcome: contract.OutcomeUnreachable})
	e := desktopEngine(net, bad)
	run := scanRouted(t, e, collect.ScanOptions{Extra: prefixes("192.168.0.0/24")})
	wantFaked(t, run, "192.168.0.0/24")
	if len(run.Sources) != 1 || run.Sources[0].Outcome != contract.OutcomeUnreachable {
		t.Errorf("sources = %+v: the router read is still reported", run.Sources)
	}
	if !e.PathFakesAnswers(context.Background()) {
		t.Error("PathFakesAnswers with a faking path = false")
	}
}
