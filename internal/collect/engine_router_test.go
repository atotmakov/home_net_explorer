package collect_test

import (
	"context"
	"fmt"
	"net/netip"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/collect"
	"github.com/atotmakov/home_net_explorer/internal/collect/collecttest"
	"github.com/atotmakov/home_net_explorer/internal/collect/router"
	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// routerResult is a successful read of 192.168.0.0/24: two online devices, one offline.
func routerResult() router.Result {
	return router.Result{Outcome: contract.OutcomeOK, Online: 2, Offline: 1, Observations: []contract.Observation{
		{IP: "192.168.0.4", MAC: "00:00:5e:10:00:02", Hostname: "host-01", HostnameSource: contract.HostnameSourceRouter, Via: "LAN1", Method: contract.ObsRouterTable},
		{IP: "192.168.0.9", MAC: "02:00:5e:10:00:07", Method: contract.ObsRouterTable},
	}}
}

// desktop has two on-link subnets; the ISP router's 192.168.0.0/24 is one hop away.
func desktopEngine(net *collecttest.FakeNetwork, routers ...router.Source) *collect.Engine {
	e := newEngine(net, collecttest.NewFakeRoutes("192.168.8.1", "internet=192.168.8.176/24", "home=192.168.1.10/24"))
	e.Routers = routers
	return e
}

func observationsByIP(run *contract.CollectionRun) map[string][]contract.Observation {
	m := map[string][]contract.Observation{}
	for _, o := range run.Observations {
		m[o.IP] = append(m[o.IP], o)
	}
	return m
}

func TestRouterSubnetOK(t *testing.T) {
	net := collecttest.NewFakeNetwork().
		Add("192.168.1.20", collecttest.Host{MAC: "00:11:32:aa:bb:cc"}).
		Add("192.168.0.4", collecttest.Host{Hostname: "from-dns"}). // would answer pings and DNS
		Add("192.168.0.9", collecttest.Host{Hostname: "phone-dns"})
	fr := collecttest.NewFakeRouter("192.168.0.1", "192.168.0.0/24", routerResult())
	run, err := desktopEngine(net, fr).Scan(context.Background(), collect.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if fr.Calls() != 1 {
		t.Errorf("router read %d times", fr.Calls())
	}
	s, ok := subnet(run, "192.168.0.0/24")
	if !ok || s.Method != contract.MethodRouterTable || !s.Complete || s.HostsProbed != 3 {
		t.Fatalf("router subnet = %+v, want router_table, complete, hosts_probed 3", s)
	}
	if net.Probes != 2*254 {
		t.Errorf("probes = %d, want %d: the router's subnet must not be ICMP/TCP-probed when the read succeeds", net.Probes, 2*254)
	}
	got := observationsByIP(run)
	if o := got["192.168.0.4"]; len(o) != 1 || o[0].Method != contract.ObsRouterTable || o[0].MAC != "00:00:5e:10:00:02" ||
		o[0].Hostname != "host-01" || o[0].HostnameSource != contract.HostnameSourceRouter || o[0].Via != "LAN1" || !o[0].ObservedAt.Equal(t0) {
		t.Errorf("192.168.0.4 = %+v (router hostname must survive name resolution)", o)
	}
	if o := got["192.168.0.9"]; len(o) != 1 || o[0].Hostname != "phone-dns" || o[0].HostnameSource != contract.HostnameSourceDNS {
		t.Errorf("192.168.0.9 = %+v (DNS fills in a missing router hostname)", o)
	}
	want := contract.RunSource{Type: "router", Model: "huawei-hg8145v5", Address: "192.168.0.1", Subnet: "192.168.0.0/24", Outcome: "ok", Online: 2, Offline: 1}
	if len(run.Sources) != 1 || run.Sources[0] != want {
		t.Errorf("sources = %+v, want [%+v]", run.Sources, want)
	}
	if err := contract.Validate(run); err != nil {
		t.Errorf("run fails validation: %v", err)
	}
}

func TestRouterSubnetFailed(t *testing.T) {
	for _, outcome := range []string{contract.OutcomeUnreachable, contract.OutcomeLoginRejected, contract.OutcomeLocked,
		contract.OutcomeSessionBusy, contract.OutcomePageNotUnderstood, contract.OutcomeSkippedAfterRejection} {
		net := collecttest.NewFakeNetwork().
			Add("192.168.1.20", collecttest.Host{MAC: "00:11:32:aa:bb:cc"}).
			Add("192.168.0.4", collecttest.Host{})
		fr := collecttest.NewFakeRouter("192.168.0.1", "192.168.0.0/24", router.Result{Outcome: outcome})
		run, err := desktopEngine(net, fr).Scan(context.Background(), collect.ScanOptions{})
		if err != nil {
			t.Fatalf("%s: a failed router read must not fail the scan (FR-010): %v", outcome, err)
		}
		if s, ok := subnet(run, "192.168.0.0/24"); !ok || s.Method != contract.MethodRouterTable || s.Complete || s.HostsProbed != 0 {
			t.Errorf("%s: router subnet = %+v, want router_table, incomplete, 0 hosts", outcome, s)
		}
		if s, ok := subnet(run, "192.168.1.0/24"); !ok || s.Method != contract.MethodARP || !s.Complete {
			t.Errorf("%s: the rest of the scan must be unaffected: %+v", outcome, s)
		}
		if o := observationsByIP(run)["192.168.0.4"]; len(o) != 0 {
			t.Errorf("%s: observations in the router subnet without a router read: %+v", outcome, o)
		}
		if len(run.Sources) != 1 || run.Sources[0].Outcome != outcome || run.Sources[0].Online != 0 || run.Sources[0].Offline != 0 {
			t.Errorf("%s: sources = %+v", outcome, run.Sources)
		}
		if err := contract.Validate(run); err != nil {
			t.Errorf("%s: run fails validation: %v", outcome, err)
		}
	}
}

// research R4: a router serving an on-link subnet adds its devices to that subnet's ARP entry,
// one observation per IP (the router_table one wins, it carries hostname and via).
func TestRouterOnLinkSubnet(t *testing.T) {
	net := collecttest.NewFakeNetwork().
		Add("192.168.1.50", collecttest.Host{MAC: "3c:22:fb:44:55:66", Hostname: "tv-dns"}).
		Add("192.168.1.20", collecttest.Host{MAC: "00:11:32:aa:bb:cc"})
	fr := collecttest.NewFakeRouter("192.168.1.1", "192.168.1.0/24", router.Result{Outcome: contract.OutcomeOK, Online: 2, Offline: 0,
		Observations: []contract.Observation{
			{IP: "192.168.1.50", MAC: "3c:22:fb:44:55:66", Hostname: "tv", HostnameSource: contract.HostnameSourceRouter, Via: "LAN2", Method: contract.ObsRouterTable},
			{IP: "192.168.1.60", MAC: "da:a1:19:01:02:03", Hostname: "sleeping-phone", HostnameSource: contract.HostnameSourceRouter, Via: "SSID1", Method: contract.ObsRouterTable},
		}})
	run, err := desktopEngine(net, fr).Scan(context.Background(), collect.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, s := range run.Subnets {
		if s.CIDR == "192.168.1.0/24" {
			n++
			if s.Method != contract.MethodARP || !s.Complete {
				t.Errorf("on-link router subnet = %+v, want arp", s)
			}
		}
	}
	if n != 1 {
		t.Errorf("192.168.1.0/24 listed %d times", n)
	}
	got := observationsByIP(run)
	if o := got["192.168.1.50"]; len(o) != 1 || o[0].Method != contract.ObsRouterTable || o[0].Via != "LAN2" || o[0].Hostname != "tv" {
		t.Errorf("192.168.1.50 = %+v, want one router_table observation", o)
	}
	if o := got["192.168.1.60"]; len(o) != 1 || o[0].Method != contract.ObsRouterTable {
		t.Errorf("non-pinging device = %+v (US1 AS-3)", o)
	}
	if o := got["192.168.1.20"]; len(o) != 1 || o[0].Method != contract.ObsARP {
		t.Errorf("ARP-only device = %+v", o)
	}
	if len(run.Sources) != 1 || run.Sources[0].Subnet != "192.168.1.0/24" {
		t.Errorf("sources = %+v", run.Sources)
	}
	if err := contract.Validate(run); err != nil {
		t.Errorf("run fails validation: %v", err)
	}
}

func TestRouterReadTimeout(t *testing.T) {
	net := collecttest.NewFakeNetwork()
	fr := collecttest.NewFakeRouter("192.168.0.1", "192.168.0.0/24", routerResult())
	fr.Delay = time.Hour
	e := desktopEngine(net, fr)
	e.RouterTimeout = 100 * time.Millisecond
	start := time.Now()
	run, err := e.Scan(context.Background(), collect.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("a hanging router delayed the scan by %v", d)
	}
	if len(run.Sources) != 1 || run.Sources[0].Outcome != contract.OutcomeUnreachable {
		t.Errorf("sources = %+v, want unreachable", run.Sources)
	}
}

// An ignored router subnet is reported as skipped and the router is not contacted.
func TestRouterSubnetIgnored(t *testing.T) {
	fr := collecttest.NewFakeRouter("192.168.0.1", "192.168.0.0/24", routerResult())
	run, err := desktopEngine(collecttest.NewFakeNetwork(), fr).Scan(context.Background(),
		collect.ScanOptions{Ignored: []netip.Prefix{netip.MustParsePrefix("192.168.0.0/24")}})
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := subnet(run, "192.168.0.0/24"); !ok || s.Method != contract.MethodSkipped || s.SkipReason != contract.SkipIgnored {
		t.Errorf("ignored router subnet = %+v", s)
	}
	if fr.Calls() != 0 || len(run.Sources) != 0 {
		t.Errorf("router contacted for an ignored subnet (calls %d, sources %+v)", fr.Calls(), run.Sources)
	}
	if err := contract.Validate(run); err != nil {
		t.Errorf("run fails validation: %v", err)
	}
}

// Router subnets count toward contract.MaxSubnets; on-link subnets are kept first.
func TestRouterSubnetCountsTowardLimit(t *testing.T) {
	var ifaces []string
	for i := 0; i < contract.MaxSubnets; i++ {
		ifaces = append(ifaces, fmt.Sprintf("eth%d=10.1.%d.10/24", i, i))
	}
	e := newEngine(collecttest.NewFakeNetwork(), collecttest.NewFakeRoutes("10.1.0.1", ifaces...))
	fr := collecttest.NewFakeRouter("192.168.0.1", "192.168.0.0/24", routerResult())
	e.Routers = []router.Source{fr}
	run, err := e.Scan(context.Background(), collect.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Subnets) != contract.MaxSubnets {
		t.Errorf("subnets = %d, want %d", len(run.Subnets), contract.MaxSubnets)
	}
	if _, ok := subnet(run, "192.168.0.0/24"); ok {
		t.Error("the router subnet displaced an on-link subnet")
	}
	if err := contract.Validate(run); err != nil {
		t.Errorf("run fails validation: %v", err)
	}
}
