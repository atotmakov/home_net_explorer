package store_test

import (
	"context"
	"database/sql"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/inventory"
	it "github.com/atotmakov/home_net_explorer/internal/inventory/inventorytest"
	"github.com/atotmakov/home_net_explorer/internal/store"
)

// Feature 010: the map is a view of the inventory built at request time (research R1–R7).

const routerLAN = "192.168.0.0/24"

func topo(t *testing.T, h *it.Harness, now time.Time) store.Topology {
	t.Helper()
	tp, err := h.Store.Topology(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	return tp
}

// devNode returns the device node holding ip as its first address.
func devNode(t *testing.T, tp store.Topology, ip string) store.TopoNode {
	t.Helper()
	for _, n := range tp.Nodes {
		if n.Kind == "device" && n.IP == ip {
			return n
		}
	}
	t.Fatalf("no device node with ip %s in %+v", ip, tp.Nodes)
	return store.TopoNode{}
}

func edge(tp store.Topology, kind, source, target string) (store.TopoEdge, bool) {
	for _, e := range tp.Edges {
		if e.Kind == kind && e.Source == source && e.Target == target {
			return e, true
		}
	}
	return store.TopoEdge{}, false
}

func groupOf(t *testing.T, h *it.Harness, cidr string) string {
	t.Helper()
	var id int64
	if err := h.Store.DB().QueryRow(`SELECT id FROM subnets WHERE cidr = ?`, cidr).Scan(&id); err != nil {
		t.Fatalf("subnet %s: %v", cidr, err)
	}
	return "subnet-" + itoa(id)
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func listedAt(ip, mac, via string) contract.Observation {
	return contract.Observation{IP: ip, MAC: mac, Via: via, Method: contract.ObsRouterTable}
}

func routerTable() contract.SubnetScan {
	return contract.SubnetScan{CIDR: routerLAN, Method: contract.MethodRouterTable, Complete: true, HostsProbed: 29}
}

// routerRead ingests a desktop run that read the router at 192.168.0.1 (outcome ok).
func routerRead(h *it.Harness, at time.Time, obs ...contract.Observation) {
	h.T.Helper()
	run := h.Build(it.Run{Collector: "desktop", Start: at, Subnets: []contract.SubnetScan{routerTable()}, Obs: obs})
	run.Sources = []contract.RunSource{{Type: "router", Model: "huawei-hg8145v5", Address: "192.168.0.1",
		Subnet: routerLAN, Outcome: contract.OutcomeOK, Online: len(obs), Offline: 0}}
	h.IngestRun(run, run.FinishedAt.Add(time.Second))
}

func TestTopologyAllDevices(t *testing.T) {
	h := it.New(t)
	ctx := context.Background()
	h.Ingest(it.Run{Subnets: []contract.SubnetScan{it.Scan("192.168.1.0/24")}, Obs: []contract.Observation{
		it.ARP("192.168.1.20", "00:11:32:aa:bb:cc", "nas.lan"),
		it.ARP("192.168.1.30", "00:00:5e:10:00:30", ""),
	}})
	h.Ingest(it.Run{Collector: "desktop", Start: it.T0.Add(time.Minute),
		Subnets: []contract.SubnetScan{it.Scan("192.168.8.0/24")},
		Obs:     []contract.Observation{it.ARP("192.168.8.23", "3c:22:fb:44:55:66", "printer.local")}})
	routerRead(h, it.T0.Add(time.Minute), listedAt("192.168.0.4", "00:00:5e:10:00:04", "LAN1"))
	// .30 goes offline: the next complete scan of its subnet misses it.
	h.Ingest(it.Run{Start: it.T0.Add(20 * time.Minute), Subnets: []contract.SubnetScan{it.Scan("192.168.1.0/24")},
		Obs: []contract.Observation{it.ARP("192.168.1.20", "00:11:32:aa:bb:cc", "nas.lan")}})

	now := it.T0.Add(45 * time.Minute) // desktop devices last seen 44 min ago (> 2 × 15 min): stale
	tp := topo(t, h, now)
	rows, err := h.Store.ListDevices(ctx, store.DeviceFilter{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	var devs []store.TopoNode
	for _, n := range tp.Nodes {
		if n.Kind == "device" {
			devs = append(devs, n)
		}
	}
	if len(devs) != len(rows) {
		t.Fatalf("map has %d devices, Devices list %d", len(devs), len(rows))
	}
	seen := map[int64]bool{}
	for _, n := range devs {
		if seen[n.DeviceID] {
			t.Errorf("device %d appears twice", n.DeviceID)
		}
		seen[n.DeviceID] = true
	}
	want := map[string]string{"192.168.1.20": "online", "192.168.1.30": "offline", "192.168.8.23": "stale", "192.168.0.4": "stale"}
	for _, r := range rows {
		if !seen[r.ID] {
			t.Errorf("device %d (%s) missing from the map", r.ID, r.PrimaryIP())
			continue
		}
		n := devNode(t, tp, r.PrimaryIP())
		if n.State != r.State || n.State != want[r.PrimaryIP()] {
			t.Errorf("%s state = %s, Devices list %s, want %s", r.PrimaryIP(), n.State, r.State, want[r.PrimaryIP()])
		}
		if n.LastSeenAgo != store.Ago(r.Age) || n.Label != r.DisplayName || n.MAC != r.MAC || n.ID != "dev-"+itoa(r.ID) {
			t.Errorf("node %+v does not match row %+v", n, r)
		}
		if !slices.Equal(n.IPs, []string{r.PrimaryIP()}) {
			t.Errorf("%s ips = %v", r.PrimaryIP(), n.IPs)
		}
	}
	if n := devNode(t, tp, "192.168.1.20"); n.Vendor == "" || n.LastSeenAgo != "25 mins" {
		t.Errorf("nas node = %+v, want a vendor and last seen 25 mins", n)
	}
}

func TestTopologyGroups(t *testing.T) {
	h := it.New(t)
	ctx := context.Background()
	// The nas scanned 192.168.2.0/24 once (no devices), then only 192.168.1.0/24.
	h.Ingest(it.Run{Subnets: []contract.SubnetScan{it.Scan("192.168.1.0/24"), it.Scan("192.168.2.0/24")},
		Obs: []contract.Observation{it.ARP("192.168.1.20", "00:11:32:aa:bb:cc", ""), it.ARP("192.168.1.21", "00:00:5e:10:00:21", "")}})
	h.Ingest(it.Run{Start: it.T0.Add(5 * time.Minute), Subnets: []contract.SubnetScan{it.Scan("192.168.1.0/24")},
		Obs: []contract.Observation{it.ARP("192.168.1.20", "00:11:32:aa:bb:cc", ""), it.ARP("192.168.1.21", "00:00:5e:10:00:21", "")}})
	// The desktop's latest run scanned 192.168.9.0/24 and found nothing: still shown.
	h.Ingest(it.Run{Collector: "desktop", Start: it.T0.Add(5 * time.Minute),
		Subnets: []contract.SubnetScan{it.Scan("192.168.9.0/24")}, Obs: []contract.Observation{}})
	h.Fact(func(tx *sql.Tx) error {
		return inventory.SetSubnetAttr(ctx, tx, "192.168.1.0/24", "name", "home", it.T0)
	})

	now := it.T0.Add(10 * time.Minute)
	tp := topo(t, h, now)
	g1, g9 := groupOf(t, h, "192.168.1.0/24"), groupOf(t, h, "192.168.9.0/24")
	var ids []string
	for _, g := range tp.Groups {
		ids = append(ids, g.ID)
		switch g.ID {
		case g1:
			if g.Label != "home (192.168.1.0/24)" || g.CIDR != "192.168.1.0/24" || g.Stale {
				t.Errorf("named group = %+v", g)
			}
		case g9:
			if g.Label != "192.168.9.0/24" {
				t.Errorf("unnamed group label = %q, want the CIDR", g.Label)
			}
		}
	}
	if !slices.Equal(ids, []string{g1, g9}) {
		t.Errorf("groups = %v, want [%s %s] (192.168.2.0/24 has no devices and is not in a latest run; no Other group)", ids, g1, g9)
	}
	if n := devNode(t, tp, "192.168.1.20"); n.Parent != g1 {
		t.Errorf("parent = %s, want %s", n.Parent, g1)
	}

	// A device left with no current address goes to the Other group (simulated in the projection).
	if _, err := h.Store.DB().Exec(`UPDATE device_addresses SET current = 0 WHERE ip = '192.168.1.21'`); err != nil {
		t.Fatal(err)
	}
	tp = topo(t, h, now)
	var other *store.TopoGroup
	for i := range tp.Groups {
		if tp.Groups[i].ID == "other" {
			other = &tp.Groups[i]
		}
	}
	if other == nil || other.Label != "Other" {
		t.Fatalf("no Other group: %+v", tp.Groups)
	}
	found := false
	for _, n := range tp.Nodes {
		if n.Kind == "device" && n.MAC == "00:00:5e:10:00:21" {
			found = n.Parent == "other"
		}
	}
	if !found {
		t.Errorf("device without a current address is not in the Other group: %+v", tp.Nodes)
	}
}

func TestTopologyGateways(t *testing.T) {
	h := it.New(t)
	// (b) a route next hop: the nas's default route goes through 192.168.1.1.
	h.Ingest(it.Run{Subnets: []contract.SubnetScan{it.Scan("192.168.1.0/24")},
		Obs: []contract.Observation{it.ARP("192.168.1.1", "a0:b1:c2:d3:e4:01", ""), it.ARP("192.168.1.20", "00:11:32:aa:bb:cc", "")},
		Vantage: &contract.Vantage{Interfaces: []contract.Interface{{Name: "eth0", IP: "192.168.1.20", PrefixLen: 24, MAC: "00:11:32:aa:bb:cc"}},
			Routes: []contract.Route{{Destination: "0.0.0.0/0", NextHop: "192.168.1.1", Interface: "eth0"},
				{Destination: "10.9.0.0/16", NextHop: "192.168.1.254", Interface: "eth0"}}}})
	// (a) a router read of 192.168.0.0/24 at 192.168.0.1.
	routerRead(h, it.T0.Add(time.Minute), listedAt("192.168.0.1", "e0:00:5e:10:00:01", ""), listedAt("192.168.0.4", "00:00:5e:10:00:04", "LAN1"))

	tp := topo(t, h, it.T0.Add(5*time.Minute))
	g0, g1 := groupOf(t, h, routerLAN), groupOf(t, h, "192.168.1.0/24")
	r := devNode(t, tp, "192.168.0.1")
	if !slices.Equal(r.GatewayOf, []string{g0}) {
		t.Errorf("router gateway_of = %v, want [%s]", r.GatewayOf, g0)
	}
	if _, ok := edge(tp, "gateway", r.ID, g0); !ok {
		t.Errorf("no gateway edge router → %s: %+v", g0, tp.Edges)
	}
	gw := devNode(t, tp, "192.168.1.1")
	if !slices.Equal(gw.GatewayOf, []string{g1}) {
		t.Errorf("next hop gateway_of = %v, want [%s]", gw.GatewayOf, g1)
	}
	if n := devNode(t, tp, "192.168.1.20"); len(n.GatewayOf) != 0 {
		t.Errorf("nas gateway_of = %v, want none", n.GatewayOf)
	}
	for _, e := range tp.Edges {
		if e.Kind == "gateway" && e.Source != r.ID && e.Source != gw.ID {
			t.Errorf("unexpected gateway edge %+v (a next hop no device holds adds nothing)", e)
		}
	}
}

// No "two subnets" link: a device is drawn in the subnet of its first address only, and a
// collector with network cards in two subnets is a device per card (one MAC each).
func TestTopologyNoTwoSubnetLinks(t *testing.T) {
	h := it.New(t)
	// DUNE: one MAC in two subnets.
	h.Ingest(it.Run{Subnets: []contract.SubnetScan{it.Scan("192.168.1.0/24")}, Obs: []contract.Observation{
		it.ARP("192.168.1.43", "00:00:5e:10:00:43", "dune"), it.ARP("192.168.1.10", "10:7b:44:01:02:03", "karlo2")}})
	routerRead(h, it.T0.Add(time.Minute), listedAt("192.168.0.28", "00:00:5e:10:00:43", "LAN2"))
	// The desktop collector has interfaces in 192.168.1.0/24 and 192.168.8.0/24.
	h.Ingest(it.Run{Collector: "desktop", Start: it.T0.Add(2 * time.Minute),
		Subnets: []contract.SubnetScan{it.Scan("192.168.8.0/24")},
		Obs:     []contract.Observation{it.ARP("192.168.8.1", "b4:0f:3b:10:20:30", "")},
		Vantage: &contract.Vantage{Interfaces: []contract.Interface{
			{Name: "home", IP: "192.168.1.10", PrefixLen: 24, MAC: "10:7b:44:01:02:03"},
			{Name: "internet", IP: "192.168.8.176", PrefixLen: 24, MAC: "10:7b:44:01:02:04"}}, Routes: []contract.Route{}}})

	tp := topo(t, h, it.T0.Add(5*time.Minute))
	g0, g1, g8 := groupOf(t, h, routerLAN), groupOf(t, h, "192.168.1.0/24"), groupOf(t, h, "192.168.8.0/24")
	dune := devNode(t, tp, "192.168.0.28")
	if dune.Parent != g0 || !slices.Equal(dune.IPs, []string{"192.168.0.28", "192.168.1.43"}) {
		t.Errorf("dune = %+v, want parent %s with both addresses", dune, g0)
	}
	desk := devNode(t, tp, "192.168.1.10")
	if desk.Parent != g1 {
		t.Errorf("desktop parent = %s, want %s", desk.Parent, g1)
	}
	for _, e := range tp.Edges {
		if e.Kind == "bridge" {
			t.Errorf("two-subnet link %+v", e)
		}
		if e.Source == desk.ID && e.Target == g8 {
			t.Errorf("desktop linked to %s: %+v", g8, e)
		}
	}
}

func TestTopologyPorts(t *testing.T) {
	h := it.New(t)
	router := listedAt("192.168.0.1", "e0:00:5e:10:00:01", "")
	routerRead(h, it.T0, router,
		listedAt("192.168.0.4", "00:00:5e:10:00:04", "LAN1"),
		listedAt("192.168.0.5", "00:00:5e:10:00:05", "LAN1"),
		listedAt("192.168.0.7", "00:00:5e:10:00:07", "SSID1"))

	tp := topo(t, h, it.T0.Add(5*time.Minute))
	r := devNode(t, tp, "192.168.0.1")
	lan1, ssid1 := "port-"+itoa(r.DeviceID)+"-LAN1", "port-"+itoa(r.DeviceID)+"-SSID1"
	ports := map[string]store.TopoNode{}
	for _, n := range tp.Nodes {
		if n.Kind == "port" {
			ports[n.ID] = n
		}
	}
	if len(ports) != 2 || ports[lan1].Label != "LAN1" || ports[ssid1].Label != "SSID1" || ports[lan1].Parent != r.Parent {
		t.Fatalf("port nodes = %+v, want LAN1 and SSID1 in the router's group %s", ports, r.Parent)
	}
	for _, p := range []string{lan1, ssid1} {
		if e, ok := edge(tp, "port", r.ID, p); !ok || !e.Current {
			t.Errorf("router → %s edge = %+v, %v", p, e, ok)
		}
	}
	for ip, p := range map[string]string{"192.168.0.4": lan1, "192.168.0.5": lan1, "192.168.0.7": ssid1} {
		if e, ok := edge(tp, "port", p, devNode(t, tp, ip).ID); !ok || !e.Current || e.Label == "" {
			t.Errorf("%s → %s edge = %+v, %v", p, ip, e, ok)
		}
	}

	// .5 is missing from the next complete read (offline); .4 moved to LAN2.
	routerRead(h, it.T0.Add(15*time.Minute), router,
		listedAt("192.168.0.4", "00:00:5e:10:00:04", "LAN2"),
		listedAt("192.168.0.7", "00:00:5e:10:00:07", "SSID1"))
	tp = topo(t, h, it.T0.Add(20*time.Minute))
	lan2 := "port-" + itoa(r.DeviceID) + "-LAN2"
	if e, ok := edge(tp, "port", lan1, devNode(t, tp, "192.168.0.5").ID); !ok || e.Current {
		t.Errorf("offline device's port edge = %+v, %v, want last known (current false)", e, ok)
	}
	if _, ok := edge(tp, "port", lan2, devNode(t, tp, "192.168.0.4").ID); !ok {
		t.Errorf("moved device is not on LAN2: %+v", tp.Edges)
	}
	if _, ok := edge(tp, "port", lan1, devNode(t, tp, "192.168.0.4").ID); ok {
		t.Error("moved device still on LAN1")
	}

	// Without the router as a device there are no port nodes.
	h2 := it.New(t)
	routerRead(h2, it.T0, listedAt("192.168.0.4", "00:00:5e:10:00:04", "LAN1"))
	for _, n := range topo(t, h2, it.T0.Add(5*time.Minute)).Nodes {
		if n.Kind == "port" {
			t.Errorf("port node %+v without a router device", n)
		}
	}
}

func TestTopologyManualLinks(t *testing.T) {
	h := it.New(t)
	ctx := context.Background()
	router := listedAt("192.168.0.1", "e0:00:5e:10:00:01", "")
	read := func(m int) {
		routerRead(h, it.T0.Add(time.Duration(m)*time.Minute), router,
			listedAt("192.168.0.4", "00:00:5e:10:00:04", "LAN1"),
			listedAt("192.168.0.9", "00:00:5e:10:00:09", "LAN2"))
	}
	read(0)
	key := func(ip string) string {
		var k string
		if err := h.Store.DB().QueryRow(`SELECT d.identity_key FROM devices d JOIN device_addresses a ON a.device_id = d.id
			WHERE a.ip = ? AND a.current = 1`, ip).Scan(&k); err != nil {
			t.Fatalf("identity of %s: %v", ip, err)
		}
		return k
	}
	phone, ap := key("192.168.0.4"), key("192.168.0.9")
	h.Fact(func(tx *sql.Tx) error { return store.SetUserLink(ctx, tx, phone, ap, it.T0.Add(time.Minute)) })

	check := func(when string, now time.Time, want bool) {
		t.Helper()
		tp := topo(t, h, now)
		p, a := devNode(t, tp, "192.168.0.4"), devNode(t, tp, "192.168.0.9")
		e, ok := edge(tp, "manual", p.ID, a.ID)
		if ok != want || (ok && (e.ID != "mu-"+itoa(p.DeviceID) || !e.Current)) {
			t.Errorf("%s: manual edge = %+v, %v; want %v", when, e, ok, want)
		}
		for _, e := range tp.Edges {
			if want && e.Kind == "port" && e.Target == p.ID {
				t.Errorf("%s: the phone keeps its port edge %+v next to the manual link", when, e)
			}
		}
	}
	check("after setting", it.T0.Add(2*time.Minute), true)
	for m := 15; m <= 45; m += 15 {
		read(m)
	}
	check("after 3 more reads", it.T0.Add(46*time.Minute), true)
	if err := inventory.Rebuild(ctx, h.Store, h.Applier); err != nil {
		t.Fatal(err)
	}
	check("after a rebuild", it.T0.Add(46*time.Minute), true)

	opts, err := h.Store.DeviceLinkOptions(ctx, devNode(t, topo(t, h, it.T0.Add(46*time.Minute)), "192.168.0.4").DeviceID, it.T0.Add(46*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	var selected []string
	for _, o := range opts {
		if o.Label == "" {
			t.Errorf("option without a label: %+v", o)
		}
		if o.Selected {
			selected = append(selected, o.Label)
		}
	}
	if len(opts) != 2 || len(selected) != 1 {
		t.Errorf("link options = %+v, want the 2 other devices with the ap selected", opts)
	}

	h.Fact(func(tx *sql.Tx) error { return store.ClearUserLink(ctx, tx, phone, it.T0.Add(50*time.Minute)) })
	check("after clearing", it.T0.Add(51*time.Minute), false)
	if n := scalarQ(t, h, `SELECT count(*) FROM user_links`); n != 2 {
		t.Errorf("user_links rows = %d, want 2 (append-only)", n)
	}

	// A link to a device that is not on the map is dropped.
	h.Fact(func(tx *sql.Tx) error {
		return store.SetUserLink(ctx, tx, phone, "mac:00:00:5e:99:99:99", it.T0.Add(52*time.Minute))
	})
	tp := topo(t, h, it.T0.Add(53*time.Minute))
	for _, e := range tp.Edges {
		if e.Kind == "manual" {
			t.Errorf("manual edge to an unknown device: %+v", e)
		}
	}
}

// A link set on a weak (IP-only) device follows it when it is merged into the MAC device.
func TestTopologyManualLinkFollowsMerge(t *testing.T) {
	h := it.New(t)
	ctx := context.Background()
	routerRead(h, it.T0, listedAt("192.168.0.1", "e0:00:5e:10:00:01", ""), listedAt("192.168.0.9", "00:00:5e:10:00:09", "LAN2"))
	h.Ingest(it.Run{Collector: "desktop", Start: it.T0.Add(time.Minute),
		Subnets: []contract.SubnetScan{{CIDR: routerLAN, Method: contract.MethodICMPTCP, Complete: true, HostsProbed: 254}},
		Obs:     []contract.Observation{{IP: "192.168.0.4", Method: contract.ObsICMP}}})
	h.Fact(func(tx *sql.Tx) error {
		return store.SetUserLink(ctx, tx, "ip:192.168.0.0/24:192.168.0.4", "mac:00:00:5e:10:00:09", it.T0.Add(2*time.Minute))
	})
	routerRead(h, it.T0.Add(15*time.Minute), listedAt("192.168.0.1", "e0:00:5e:10:00:01", ""),
		listedAt("192.168.0.9", "00:00:5e:10:00:09", "LAN2"), listedAt("192.168.0.4", "00:00:5e:10:00:04", "LAN1"))
	tp := topo(t, h, it.T0.Add(16*time.Minute))
	if _, ok := edge(tp, "manual", devNode(t, tp, "192.168.0.4").ID, devNode(t, tp, "192.168.0.9").ID); !ok {
		t.Errorf("manual link did not follow the merge: %+v", tp.Edges)
	}
}

// Feature 012: an access point read with outcome ok is not a router read; the access point is
// never made the gateway of its subnet.
func TestAccessPointNotGateway(t *testing.T) {
	h := it.New(t)
	routerRead(h, it.T0, listedAt("192.168.0.1", "e0:00:5e:10:00:01", ""), listedAt("192.168.0.5", "00:00:5e:10:00:05", "LAN1"))
	run := h.Build(it.Run{Collector: "desktop", Start: it.T0.Add(time.Minute), Subnets: []contract.SubnetScan{routerTable()},
		Obs: []contract.Observation{listedAt("192.168.0.5", "00:00:5e:10:00:05", "LAN1")}})
	run.Sources = []contract.RunSource{{Type: "access_point", Model: "tplink-eap225", Address: "192.168.0.5",
		Subnet: routerLAN, Outcome: contract.OutcomeOK, Online: 0, Offline: 0}}
	h.IngestRun(run, run.FinishedAt.Add(time.Second))

	tp := topo(t, h, it.T0.Add(5*time.Minute))
	g0 := groupOf(t, h, routerLAN)
	if r := devNode(t, tp, "192.168.0.1"); !slices.Equal(r.GatewayOf, []string{g0}) {
		t.Errorf("router gateway_of = %v, want [%s]", r.GatewayOf, g0)
	}
	if ap := devNode(t, tp, "192.168.0.5"); len(ap.GatewayOf) != 0 {
		t.Errorf("access point gateway_of = %v, want none", ap.GatewayOf)
	}
}
