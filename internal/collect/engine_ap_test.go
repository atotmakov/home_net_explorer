package collect_test

import (
	"context"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/collect"
	"github.com/atotmakov/home_net_explorer/internal/collect/accesspoint"
	"github.com/atotmakov/home_net_explorer/internal/collect/collecttest"
	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// Feature 012: access point reads in a scan (research R6, R7).

func apOK(clients ...accesspoint.Client) accesspoint.Result {
	return accesspoint.Result{Outcome: contract.OutcomeOK, Clients: clients, Path: "status.client.user"}
}

func client(mac, ip, ssid, band, host string) accesspoint.Client {
	c := accesspoint.Client{MAC: mac, SSID: ssid, Band: band, Hostname: host}
	if ip != "" {
		c.IP = netip.MustParseAddr(ip)
	}
	return c
}

func TestAccessPointClientsInRun(t *testing.T) {
	net := collecttest.NewFakeNetwork().Add("192.168.1.20", collecttest.Host{MAC: "00:11:32:aa:bb:cc"})
	fr := collecttest.NewFakeRouter("192.168.0.1", "192.168.0.0/24", routerResult()) // 192.168.0.4 host-01 and .9 on LAN1
	long := strings.Repeat("s", 40)
	apA := collecttest.NewFakeAccessPoint("192.168.0.5", "192.168.0.0/24", apOK(
		client("00:00:5e:10:00:02", "192.168.0.4", "Home", "5 GHz", ""),          // replaces the router's entry, keeps host-01
		client("02:00:5e:10:00:07", "192.168.0.9", "Home", "2.4 GHz", "phone-9"), // replaces the router's entry
		client("00:00:5e:10:00:13", "192.168.0.13", long, "5 GHz", ""),           // new; via cut to 32
		client("00:00:5e:10:00:40", "", "Guest", "5 GHz", ""),                    // no IP: left out
		client("", "192.168.0.41", "Guest", "5 GHz", ""),                         // no valid MAC: left out
		client("00:00:5e:10:00:42", "192.168.5.42", "Home", "5 GHz", ""),         // outside the scanned subnets: left out
		client("00:00:5e:10:00:31", "192.168.0.31", "Home", "5 GHz", ""),         // also on B: B (higher address) wins
	))
	apB := collecttest.NewFakeAccessPoint("192.168.0.10", "192.168.0.0/24", apOK(
		client("00:00:5e:10:00:31", "192.168.0.31", "Home", "2.4 GHz", "laptop"),
	))
	failedAP := collecttest.NewFakeAccessPoint("192.168.0.11", "192.168.0.0/24", accesspoint.Result{Outcome: contract.OutcomeLoginRejected})
	e := desktopEngine(net, fr)
	e.AccessPoints = []accesspoint.Source{apB, failedAP, apA} // reported in address order whatever the config order
	run, err := e.Scan(context.Background(), collect.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := contract.Validate(run); err != nil {
		t.Fatalf("run fails validation: %v", err)
	}
	got := observationsByIP(run)
	check := func(ip, mac, via, ap, host string) {
		t.Helper()
		o := got[ip]
		if len(o) != 1 || o[0].Method != contract.ObsAPClient || o[0].MAC != mac || o[0].Via != via || o[0].AccessPoint != ap ||
			o[0].Hostname != host || !o[0].ObservedAt.Equal(t0) {
			t.Errorf("%s = %+v, want ap_client %s via %q from %s, hostname %q", ip, o, mac, via, ap, host)
		}
	}
	check("192.168.0.4", "00:00:5e:10:00:02", "Home (5 GHz)", "192.168.0.5", "host-01")
	check("192.168.0.9", "02:00:5e:10:00:07", "Home (2.4 GHz)", "192.168.0.5", "phone-9")
	check("192.168.0.13", "00:00:5e:10:00:13", strings.Repeat("s", 24)+" (5 GHz)", "192.168.0.5", "")
	check("192.168.0.31", "00:00:5e:10:00:31", "Home (2.4 GHz)", "192.168.0.10", "laptop")
	for _, ip := range []string{"192.168.0.41", "192.168.5.42"} {
		if len(got[ip]) != 0 {
			t.Errorf("%s was reported: %+v", ip, got[ip])
		}
	}
	if o := got["192.168.0.4"]; len(o) == 1 && o[0].HostnameSource != contract.HostnameSourceRouter {
		t.Errorf("the router's hostname lost its source: %+v", o[0])
	}

	want := []contract.RunSource{
		{Type: "router", Model: "huawei-hg8145v5", Address: "192.168.0.1", Subnet: "192.168.0.0/24", Outcome: "ok", Online: 2, Offline: 1},
		{Type: "access_point", Model: "tplink-eap225", Address: "192.168.0.5", Subnet: "192.168.0.0/24", Outcome: "ok", Online: 4, Offline: 3},
		{Type: "access_point", Model: "tplink-eap225", Address: "192.168.0.10", Subnet: "192.168.0.0/24", Outcome: "ok", Online: 1, Offline: 0},
		{Type: "access_point", Model: "tplink-eap225", Address: "192.168.0.11", Subnet: "192.168.0.0/24", Outcome: "login_rejected"},
	}
	if len(run.Sources) != len(want) {
		t.Fatalf("sources = %+v\nwant %+v", run.Sources, want)
	}
	for i := range want {
		if run.Sources[i] != want[i] {
			t.Errorf("sources[%d] = %+v, want %+v", i, run.Sources[i], want[i])
		}
	}
	// An access point adds no subnet entry: the router's subnet is the only 192.168.0.0/24 one.
	n := 0
	for _, s := range run.Subnets {
		if s.CIDR == "192.168.0.0/24" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("192.168.0.0/24 listed %d times in subnets", n)
	}
}

// A failed access point read changes nothing else in the run.
func TestAccessPointFailureLeavesRun(t *testing.T) {
	net := collecttest.NewFakeNetwork().Add("192.168.1.20", collecttest.Host{MAC: "00:11:32:aa:bb:cc"})
	plain, err := desktopEngine(net, collecttest.NewFakeRouter("192.168.0.1", "192.168.0.0/24", routerResult())).
		Scan(context.Background(), collect.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	e := desktopEngine(net, collecttest.NewFakeRouter("192.168.0.1", "192.168.0.0/24", routerResult()))
	e.AccessPoints = []accesspoint.Source{collecttest.NewFakeAccessPoint("192.168.0.5", "192.168.0.0/24",
		accesspoint.Result{Outcome: contract.OutcomeUnreachable})}
	run, err := e.Scan(context.Background(), collect.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Observations) != len(plain.Observations) || len(run.Subnets) != len(plain.Subnets) {
		t.Errorf("a failed access point read changed the run: %d/%d observations, %d/%d subnets",
			len(run.Observations), len(plain.Observations), len(run.Subnets), len(plain.Subnets))
	}
	if len(run.Sources) != 2 || run.Sources[1].Outcome != contract.OutcomeUnreachable || run.Sources[1].Online != 0 {
		t.Errorf("sources = %+v", run.Sources)
	}
}

// Router and access point reads overlap (SC-003): the scan waits for the slowest, not the sum.
func TestAccessPointReadsInParallel(t *testing.T) {
	net := collecttest.NewFakeNetwork()
	fr := collecttest.NewFakeRouter("192.168.0.1", "192.168.0.0/24", routerResult())
	fr.Delay = 300 * time.Millisecond
	a := collecttest.NewFakeAccessPoint("192.168.0.5", "192.168.0.0/24", apOK())
	b := collecttest.NewFakeAccessPoint("192.168.0.10", "192.168.0.0/24", apOK())
	a.Delay, b.Delay = 300*time.Millisecond, 300*time.Millisecond
	e := desktopEngine(net, fr)
	e.AccessPoints = []accesspoint.Source{a, b}
	start := time.Now()
	if _, err := e.Scan(context.Background(), collect.ScanOptions{}); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d >= 600*time.Millisecond {
		t.Errorf("scan took %v: router and access point reads did not overlap", d)
	}
	if a.Calls() != 1 || b.Calls() != 1 {
		t.Errorf("access points read %d and %d times", a.Calls(), b.Calls())
	}
}

// A slow access point is cut off at APTimeout and reported unreachable.
func TestAccessPointTimeout(t *testing.T) {
	a := collecttest.NewFakeAccessPoint("192.168.0.5", "192.168.0.0/24", apOK())
	a.Delay = time.Hour
	e := desktopEngine(collecttest.NewFakeNetwork())
	e.AccessPoints = []accesspoint.Source{a}
	e.APTimeout = 50 * time.Millisecond
	run, err := e.Scan(context.Background(), collect.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Sources) != 1 || run.Sources[0].Outcome != contract.OutcomeUnreachable {
		t.Errorf("sources = %+v, want one unreachable access point", run.Sources)
	}
}
