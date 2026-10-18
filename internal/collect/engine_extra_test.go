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

// Feature 002, US3: extra_subnets are scanned in addition to auto-discovery (FR-015, FR-016,
// SC-007), and give the ICMP/TCP fallback for a router's subnet (research R4).

func prefixes(ss ...string) []netip.Prefix {
	var out []netip.Prefix
	for _, s := range ss {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}

func methods(run *contract.CollectionRun) map[string]contract.SubnetScan {
	m := map[string]contract.SubnetScan{}
	for _, s := range run.Subnets {
		if _, dup := m[s.CIDR]; dup {
			s.Method = "DUPLICATE"
		}
		m[s.CIDR] = s
	}
	return m
}

func TestExtraSubnetsAddToAutoDiscovery(t *testing.T) {
	net := collecttest.NewFakeNetwork().Add("192.168.0.4", collecttest.Host{})
	run, err := desktopEngine(net).Scan(context.Background(), collect.ScanOptions{Extra: prefixes("192.168.0.0/24")})
	if err != nil {
		t.Fatal(err)
	}
	m := methods(run)
	if len(m) != 3 || m["192.168.1.0/24"].Method != contract.MethodARP || m["192.168.8.0/24"].Method != contract.MethodARP ||
		m["192.168.0.0/24"].Method != contract.MethodICMPTCP || !m["192.168.0.0/24"].Complete {
		t.Errorf("subnets = %+v, want both on-link subnets (arp) plus the extra (icmp_tcp)", run.Subnets)
	}
	if o := observationsByIP(run)["192.168.0.4"]; len(o) != 1 || o[0].Method != contract.ObsICMP {
		t.Errorf("extra-subnet host = %+v", o)
	}
	if err := contract.Validate(run); err != nil {
		t.Errorf("run fails validation: %v", err)
	}
}

func TestExtraOnLinkScannedOnce(t *testing.T) {
	run, err := desktopEngine(collecttest.NewFakeNetwork()).Scan(context.Background(), collect.ScanOptions{Extra: prefixes("192.168.1.0/24")})
	if err != nil {
		t.Fatal(err)
	}
	if m := methods(run); len(run.Subnets) != 2 || m["192.168.1.0/24"].Method != contract.MethodARP {
		t.Errorf("subnets = %+v, want the on-link extra once, with ARP", run.Subnets)
	}
}

func TestExtraWithFixedTargets(t *testing.T) {
	run, err := desktopEngine(collecttest.NewFakeNetwork()).Scan(context.Background(), collect.ScanOptions{
		Targets: prefixes("192.168.50.0/24"), Extra: prefixes("192.168.0.0/24", "192.168.50.0/24")})
	if err != nil {
		t.Fatal(err)
	}
	m := methods(run)
	if len(run.Subnets) != 2 || m["192.168.50.0/24"].Method != contract.MethodICMPTCP || m["192.168.0.0/24"].Method != contract.MethodICMPTCP {
		t.Errorf("subnets = %+v, want the union of subnets and extra_subnets, each once", run.Subnets)
	}
}

// SC-007: extras never stop auto-discovery of a newly attached adapter.
func TestExtraKeepsDiscoveringNewAdapters(t *testing.T) {
	routes := collecttest.NewFakeRoutes("192.168.8.1", "internet=192.168.8.176/24")
	e := newEngine(collecttest.NewFakeNetwork(), routes)
	opts := collect.ScanOptions{Extra: prefixes("192.168.0.0/24")}
	if run, _ := e.Scan(context.Background(), opts); len(run.Subnets) != 2 {
		t.Fatalf("first scan = %+v", run.Subnets)
	}
	routes.Set("192.168.8.1", "internet=192.168.8.176/24", "lab=10.20.30.5/24")
	run, err := e.Scan(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if m := methods(run); len(run.Subnets) != 3 || m["10.20.30.0/24"].Method != contract.MethodARP || m["192.168.0.0/24"].Method != contract.MethodICMPTCP {
		t.Errorf("after a new adapter: %+v", run.Subnets)
	}
}

func TestExtraIgnored(t *testing.T) {
	run, err := desktopEngine(collecttest.NewFakeNetwork()).Scan(context.Background(), collect.ScanOptions{
		Extra: prefixes("192.168.0.0/24"), Ignored: prefixes("192.168.0.0/24")})
	if err != nil {
		t.Fatal(err)
	}
	if s := methods(run)["192.168.0.0/24"]; s.Method != contract.MethodSkipped || s.SkipReason != contract.SkipIgnored {
		t.Errorf("ignored extra = %+v", s)
	}
}

func TestExtraCountsTowardLimit(t *testing.T) {
	var ifaces []string
	for i := 0; i < contract.MaxSubnets; i++ {
		ifaces = append(ifaces, fmt.Sprintf("eth%d=10.1.%d.10/24", i, i))
	}
	e := newEngine(collecttest.NewFakeNetwork(), collecttest.NewFakeRoutes("10.1.0.1", ifaces...))
	run, err := e.Scan(context.Background(), collect.ScanOptions{Extra: prefixes("10.0.0.0/24")})
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Subnets) != contract.MaxSubnets {
		t.Errorf("subnets = %d, want %d", len(run.Subnets), contract.MaxSubnets)
	}
	if _, ok := methods(run)["10.0.0.0/24"]; ok {
		t.Error("an extra subnet displaced an on-link subnet (on-link subnets are kept first)")
	}
}

func TestExtraIsRouterFallback(t *testing.T) {
	opts := collect.ScanOptions{Extra: prefixes("192.168.0.0/24")}

	net := collecttest.NewFakeNetwork().Add("192.168.0.4", collecttest.Host{})
	ok := collecttest.NewFakeRouter("192.168.0.1", "192.168.0.0/24", routerResult())
	run, err := desktopEngine(net, ok).Scan(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if s := methods(run)["192.168.0.0/24"]; s.Method != contract.MethodRouterTable || !s.Complete {
		t.Errorf("router read ok: %+v, want router_table", s)
	}
	if net.Probes != 3*254 {
		t.Errorf("probes = %d: an extra subnet is also probed after a successful router read", net.Probes)
	}
	if o := observationsByIP(run)["192.168.0.4"]; len(o) != 1 || o[0].Method != contract.ObsRouterTable {
		t.Errorf("192.168.0.4 = %+v: the router's online entry wins over the probe", o)
	}

	net = collecttest.NewFakeNetwork().Add("192.168.0.4", collecttest.Host{})
	bad := collecttest.NewFakeRouter("192.168.0.1", "192.168.0.0/24", router.Result{Outcome: contract.OutcomeSessionBusy})
	run, err = desktopEngine(net, bad).Scan(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if s := methods(run)["192.168.0.0/24"]; s.Method != contract.MethodICMPTCP || s.Complete || s.HostsProbed != 254 {
		t.Errorf("router read failed: %+v, want an incomplete icmp_tcp fallback (FR-010)", s)
	}
	if o := observationsByIP(run)["192.168.0.4"]; len(o) != 1 || o[0].Method != contract.ObsICMP {
		t.Errorf("fallback presence = %+v", o)
	}
	if len(run.Sources) != 1 || run.Sources[0].Outcome != contract.OutcomeSessionBusy {
		t.Errorf("sources = %+v", run.Sources)
	}
	if err := contract.Validate(run); err != nil {
		t.Errorf("run fails validation: %v", err)
	}
}

func TestPlanExtra(t *testing.T) {
	routes := collecttest.NewFakeRoutes("192.168.8.1", "internet=192.168.8.176/24")
	v, _ := routes.Vantage(context.Background())
	opts := collect.ScanOptions{Extra: prefixes("192.168.0.0/24", "10.20.30.0/24")}
	fr := collecttest.NewFakeRouter("192.168.0.1", "192.168.0.0/24", routerResult())
	plan := map[string]collect.PlannedSubnet{}
	for _, p := range collect.Plan(v, opts, []router.Source{fr}) {
		plan[p.Prefix.String()] = p
	}
	if p := plan["192.168.0.0/24"]; p.Method != contract.MethodRouterTable || p.Router != "huawei-hg8145v5 at 192.168.0.1" || !p.Fallback {
		t.Errorf("router extra = %+v", p)
	}
	if p := plan["10.20.30.0/24"]; p.Method != contract.MethodICMPTCP || p.Router != "" {
		t.Errorf("plain extra = %+v", p)
	}
	if p := plan["192.168.8.0/24"]; p.Method != contract.MethodARP {
		t.Errorf("on-link = %+v", p)
	}
}

// The HG8145V5 lists devices that just reconnected as offline: a reachable router subnet is
// probed too, and a host that answers is online with the MAC and name the router lists for it.
func TestRouterOfflineFlagCorrectedByProbe(t *testing.T) {
	res := routerResult()
	res.Offline, res.Listed = 2, []contract.Observation{
		{IP: "192.168.0.7", MAC: "58:20:b1:4d:af:10", Hostname: "DEV4DAF10", HostnameSource: contract.HostnameSourceRouter, Via: "LAN1", Method: contract.ObsRouterTable},
		{IP: "192.168.0.8", MAC: "8c:85:90:cf:7f:0b", Method: contract.ObsRouterTable},
	}
	net := collecttest.NewFakeNetwork().
		Add("192.168.0.7", collecttest.Host{}).              // listed offline, answers
		Add("192.168.0.50", collecttest.Host{TCPOnly: true}) // not listed, answers
	fr := collecttest.NewFakeRouter("192.168.0.1", "192.168.0.0/24", res)
	run, err := desktopEngine(net, fr).Scan(context.Background(), collect.ScanOptions{Extra: prefixes("192.168.0.0/24")})
	if err != nil {
		t.Fatal(err)
	}
	got := observationsByIP(run)
	if o := got["192.168.0.7"]; len(o) != 1 || o[0].MAC != "58:20:b1:4d:af:10" || o[0].Hostname != "DEV4DAF10" ||
		o[0].HostnameSource != contract.HostnameSourceRouter || o[0].Method != contract.ObsICMP || o[0].Via != "" {
		t.Errorf("192.168.0.7 = %+v, want online with the router's MAC and name", o)
	}
	if o := got["192.168.0.50"]; len(o) != 1 || o[0].MAC != "" || o[0].Method != contract.ObsTCP {
		t.Errorf("192.168.0.50 = %+v, want presence only", o)
	}
	if _, ok := got["192.168.0.8"]; ok {
		t.Error("192.168.0.8 is listed offline and does not answer: it must not be recorded")
	}
	for _, ip := range []string{"192.168.0.4", "192.168.0.9"} {
		if len(got[ip]) != 1 {
			t.Errorf("router online entry %s missing", ip)
		}
	}
	if s := methods(run)["192.168.0.0/24"]; s.Method != contract.MethodRouterTable || !s.Complete {
		t.Errorf("subnet = %+v, want router_table complete", s)
	}
	if err := contract.Validate(run); err != nil {
		t.Errorf("run fails validation: %v", err)
	}

	// A path that answers for every address adds nothing beside the router.
	net = collecttest.NewFakeNetwork()
	net.AnswerAll = true
	run, err = desktopEngine(net, collecttest.NewFakeRouter("192.168.0.1", "192.168.0.0/24", res)).Scan(context.Background(), collect.ScanOptions{Extra: prefixes("192.168.0.0/24")})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, o := range run.Observations {
		if prefixes("192.168.0.0/24")[0].Contains(netip.MustParseAddr(o.IP)) {
			n++
		}
	}
	if n != 2 {
		t.Errorf("%d observations in the router subnet with a faking path, want the router's 2", n)
	}
}
