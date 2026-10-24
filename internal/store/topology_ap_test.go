package store_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	it "github.com/atotmakov/home_net_explorer/internal/inventory/inventorytest"
	"github.com/atotmakov/home_net_explorer/internal/store"
)

// Feature 012, US3: Wi-Fi devices hang under the access point that reported them.

func apClient(ip, mac, via, ap string) contract.Observation {
	return contract.Observation{IP: ip, MAC: mac, Via: via, AccessPoint: ap, Method: contract.ObsAPClient}
}

func named(o contract.Observation, host string) contract.Observation {
	o.Hostname = host
	return o
}

// apRead ingests a desktop run that read the router (wired) and the access points at aps
// (outcome ok), as the engine sends it: Wi-Fi clients as ap_client observations.
func apRead(h *it.Harness, at time.Time, aps []string, obs ...contract.Observation) {
	h.T.Helper()
	run := h.Build(it.Run{Collector: "desktop", Start: at, Subnets: []contract.SubnetScan{routerTable()}, Obs: obs})
	run.Sources = []contract.RunSource{{Type: "router", Model: "huawei-hg8145v5", Address: "192.168.0.1",
		Subnet: routerLAN, Outcome: contract.OutcomeOK, Online: len(obs)}}
	for _, ap := range aps {
		run.Sources = append(run.Sources, contract.RunSource{Type: "access_point", Model: "tplink-eap225", Address: ap,
			Subnet: routerLAN, Outcome: contract.OutcomeOK})
	}
	h.IngestRun(run, run.FinishedAt.Add(time.Second))
}

func TestTopologyAccessPoints(t *testing.T) {
	h := it.New(t)
	const home5, home24 = "Home (5 GHz)", "Home (2.4 GHz)"
	wired := []contract.Observation{
		listedAt("192.168.0.1", "e0:00:5e:10:00:01", ""),
		named(listedAt("192.168.0.5", "00:00:5e:10:00:05", "LAN1"), "eap-hall"),
		named(listedAt("192.168.0.10", "00:00:5e:10:00:10", "LAN1"), "eap-office"),
		listedAt("192.168.0.4", "00:00:5e:10:00:04", "LAN1"),
	}
	aps := []string{"192.168.0.5", "192.168.0.10", "192.168.0.99"} // .99: no device holds it
	apRead(h, it.T0, aps, append(wired[:4:4],
		apClient("192.168.0.21", "00:00:5e:10:00:21", home5, "192.168.0.5"),
		apClient("192.168.0.22", "00:00:5e:10:00:22", home5, "192.168.0.5"),
		apClient("192.168.0.23", "00:00:5e:10:00:23", home24, "192.168.0.10"),
		apClient("192.168.0.24", "00:00:5e:10:00:24", home5, "192.168.0.99"))...)

	tp := topo(t, h, it.T0.Add(5*time.Minute))
	r, a, b := devNode(t, tp, "192.168.0.1"), devNode(t, tp, "192.168.0.5"), devNode(t, tp, "192.168.0.10")
	portA, portB := "port-"+itoa(a.DeviceID)+"-"+home5, "port-"+itoa(b.DeviceID)+"-"+home24
	ports := map[string]store.TopoNode{}
	for _, n := range tp.Nodes {
		if n.Kind == "port" {
			ports[n.ID] = n
		}
	}
	if p, ok := ports[portA]; !ok || p.Parent != a.Parent || p.Label != home5 {
		t.Errorf("port node %s = %+v, %v; want label %q in the AP's group %s", portA, p, ok, home5, a.Parent)
	}
	if p, ok := ports[portB]; !ok || p.Label != home24 {
		t.Errorf("port node %s = %+v, %v", portB, p, ok)
	}
	if e, ok := edge(tp, "port", a.ID, portA); !ok || !e.Current || e.ID != "rp-"+itoa(a.DeviceID)+"-"+home5 {
		t.Errorf("AP → port edge = %+v, %v", e, ok)
	}
	for ip, p := range map[string]string{"192.168.0.21": portA, "192.168.0.22": portA, "192.168.0.23": portB} {
		d := devNode(t, tp, ip)
		if e, ok := edge(tp, "port", p, d.ID); !ok || !e.Current || e.ID != "pd-"+itoa(d.DeviceID) {
			t.Errorf("%s → %s edge = %+v, %v", p, ip, e, ok)
		}
	}
	lan1 := "port-" + itoa(r.DeviceID) + "-LAN1"
	if _, ok := edge(tp, "port", lan1, devNode(t, tp, "192.168.0.4").ID); !ok {
		t.Error("the wired device is not under the router's LAN1")
	}
	for _, ip := range []string{"192.168.0.21", "192.168.0.22", "192.168.0.23"} {
		if _, ok := edge(tp, "port", lan1, devNode(t, tp, ip).ID); ok {
			t.Errorf("%s is under the router's LAN1", ip)
		}
	}
	for _, e := range tp.Edges {
		if e.Target == devNode(t, tp, "192.168.0.24").ID && e.Kind == "port" {
			t.Errorf("a client of an access point no device holds has a port edge %+v", e)
		}
	}
	if len(a.GatewayOf) != 0 || len(b.GatewayOf) != 0 {
		t.Errorf("access points are gateways: %v, %v", a.GatewayOf, b.GatewayOf)
	}

	// .22 is missing from the next read (offline, last known AP); .21 moved to the other AP.
	apRead(h, it.T0.Add(15*time.Minute), aps, append(wired[:4:4],
		apClient("192.168.0.21", "00:00:5e:10:00:21", home24, "192.168.0.10"),
		apClient("192.168.0.23", "00:00:5e:10:00:23", home24, "192.168.0.10"))...)
	tp = topo(t, h, it.T0.Add(20*time.Minute))
	if e, ok := edge(tp, "port", portA, devNode(t, tp, "192.168.0.22").ID); !ok || e.Current {
		t.Errorf("offline client's edge = %+v, %v; want its last AP with current false", e, ok)
	}
	if _, ok := edge(tp, "port", portB, devNode(t, tp, "192.168.0.21").ID); !ok {
		t.Errorf("the client that moved is not under the other AP: %+v", tp.Edges)
	}
	if _, ok := edge(tp, "port", portA, devNode(t, tp, "192.168.0.21").ID); ok {
		t.Error("the client that moved is still under the first AP")
	}

	// A manual link still replaces the AP port edge.
	ctx := context.Background()
	key := func(ip string) string {
		var k string
		if err := h.Store.DB().QueryRow(`SELECT d.identity_key FROM devices d JOIN device_addresses a ON a.device_id = d.id
			WHERE a.ip = ? AND a.current = 1`, ip).Scan(&k); err != nil {
			t.Fatalf("identity of %s: %v", ip, err)
		}
		return k
	}
	h.Fact(func(tx *sql.Tx) error {
		return store.SetUserLink(ctx, tx, key("192.168.0.23"), key("192.168.0.4"), it.T0.Add(16*time.Minute))
	})
	tp = topo(t, h, it.T0.Add(20*time.Minute))
	c := devNode(t, tp, "192.168.0.23")
	if _, ok := edge(tp, "manual", c.ID, devNode(t, tp, "192.168.0.4").ID); !ok {
		t.Error("no manual edge")
	}
	for _, e := range tp.Edges {
		if e.Kind == "port" && e.Target == c.ID {
			t.Errorf("the linked client keeps its AP port edge %+v", e)
		}
	}
}

// FR-013: the device page's address row names the access point and the Wi-Fi it used.
func TestAddressViaAccessPoint(t *testing.T) {
	h := it.New(t)
	ctx := context.Background()
	apRead(h, it.T0, []string{"192.168.0.5", "192.168.0.99"},
		listedAt("192.168.0.1", "e0:00:5e:10:00:01", ""),
		named(listedAt("192.168.0.5", "00:00:5e:10:00:05", "LAN1"), "eap-hall"),
		listedAt("192.168.0.4", "00:00:5e:10:00:04", "LAN1"),
		apClient("192.168.0.21", "00:00:5e:10:00:21", "Home (5 GHz)", "192.168.0.5"),
		apClient("192.168.0.24", "00:00:5e:10:00:24", "Home (5 GHz)", "192.168.0.99"))
	now := it.T0.Add(5 * time.Minute)
	tp := topo(t, h, now)
	via := func(ip string) string {
		t.Helper()
		d, err := h.Store.GetDevice(ctx, devNode(t, tp, ip).DeviceID, now)
		if err != nil || len(d.Addresses) != 1 {
			t.Fatalf("device %s: %+v, %v", ip, d.Addresses, err)
		}
		return d.Addresses[0].ViaText()
	}
	ap := devNode(t, tp, "192.168.0.5").Label
	for ip, want := range map[string]string{
		"192.168.0.21": ap + " · Home (5 GHz)",
		"192.168.0.24": "192.168.0.99 · Home (5 GHz)", // no device holds the AP's address
		"192.168.0.4":  "LAN1",
	} {
		if got := via(ip); got != want {
			t.Errorf("%s via = %q, want %q", ip, got, want)
		}
	}
	if ap == "" || ap == "192.168.0.5" {
		t.Errorf("AP label = %q, want its name", ap)
	}
}
