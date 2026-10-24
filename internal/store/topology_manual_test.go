package store_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/inventory"
	it "github.com/atotmakov/home_net_explorer/internal/inventory/inventorytest"
	"github.com/atotmakov/home_net_explorer/internal/store"
)

// Feature 011, US2: a manual device on the Map, in the group of the device it connects via
// (research R7).

func manualKey(t *testing.T, h *it.Harness, name string, at time.Time) string {
	t.Helper()
	var key string
	h.Fact(func(tx *sql.Tx) error {
		var err error
		key, err = inventory.AddManualDevice(context.Background(), tx, inventory.ManualDevice{Name: name, Type: "switch"}, at)
		return err
	})
	return key
}

func manualNode(t *testing.T, tp store.Topology, label string) store.TopoNode {
	t.Helper()
	for _, n := range tp.Nodes {
		if n.Kind == "device" && n.Label == label {
			return n
		}
	}
	t.Fatalf("no node %q in %+v", label, tp.Nodes)
	return store.TopoNode{}
}

func TestTopologyManualDevices(t *testing.T) {
	h := it.New(t)
	ctx := context.Background()
	router := listedAt("192.168.0.1", "e0:00:5e:10:00:01", "")
	routerRead(h, it.T0, router,
		listedAt("192.168.0.4", "00:00:5e:10:00:04", "LAN1"),
		listedAt("192.168.0.5", "00:00:5e:10:00:05", "LAN1"))
	at := it.T0.Add(2 * time.Minute)
	sw := manualKey(t, h, "TL-SL1218P", at)
	hub := manualKey(t, h, "hub", at) // connects via the switch
	manualKey(t, h, "spare", at)       // connects to nothing
	key := func(mac string) string { return inventory.MACKey(mac) }
	link := func(from, to string) {
		h.Fact(func(tx *sql.Tx) error { return store.SetUserLink(ctx, tx, from, to, at.Add(time.Minute)) })
	}
	link(sw, key("e0:00:5e:10:00:01"))
	link(hub, sw)
	link(key("00:00:5e:10:00:04"), sw)
	link(key("00:00:5e:10:00:05"), hub)

	tp := topo(t, h, it.T0.Add(5*time.Minute))
	g0 := groupOf(t, h, routerLAN)
	r := devNode(t, tp, "192.168.0.1")
	s, hb, sp := manualNode(t, tp, "TL-SL1218P"), manualNode(t, tp, "hub"), manualNode(t, tp, "spare")
	for _, n := range []store.TopoNode{s, hb, sp} {
		if n.State != store.StateManual || n.LastSeenAgo != "" || len(n.IPs) != 0 || n.IP != "" {
			t.Errorf("manual node %s = %+v; want state manual, no last seen, no address", n.Label, n)
		}
	}
	if s.Parent != g0 || hb.Parent != g0 {
		t.Errorf("parents: switch %s, hub %s; want both in the router's group %s", s.Parent, hb.Parent, g0)
	}
	if sp.Parent != "other" {
		t.Errorf("unconnected manual device parent = %s, want other", sp.Parent)
	}
	pc, tv := devNode(t, tp, "192.168.0.4"), devNode(t, tp, "192.168.0.5")
	for _, c := range []struct{ from, to store.TopoNode }{{s, r}, {hb, s}, {pc, s}, {tv, hb}} {
		if e, ok := edge(tp, "manual", c.from.ID, c.to.ID); !ok || e.ID != "mu-"+itoa(c.from.DeviceID) {
			t.Errorf("manual edge %s → %s = %+v, %v", c.from.Label, c.to.Label, e, ok)
		}
	}
	for _, e := range tp.Edges {
		if e.Kind == "port" && (e.Target == pc.ID || e.Target == tv.ID) {
			t.Errorf("a device wired through the switch keeps its router port edge %+v", e)
		}
	}
}

// FR-004: manual devices are link targets; routers, switches and access points come first. FR-009:
// a link that closes a chain is a loop.
func TestLinkOptionsAndLoops(t *testing.T) {
	h := it.New(t)
	ctx := context.Background()
	routerRead(h, it.T0, listedAt("192.168.0.1", "e0:00:5e:10:00:01", ""),
		listedAt("192.168.0.4", "00:00:5e:10:00:04", "LAN1"),
		listedAt("192.168.0.9", "00:00:5e:10:00:09", "LAN1"),
		listedAt("192.168.0.7", "00:00:5e:10:00:07", "LAN1"))
	at := it.T0.Add(2 * time.Minute)
	sw := manualKey(t, h, "zz switch", at)
	setType := func(mac, typ string) {
		h.Fact(func(tx *sql.Tx) error {
			return inventory.SetDeviceAttr(ctx, tx, inventory.MACKey(mac), "type", typ, at)
		})
	}
	setType("e0:00:5e:10:00:01", "router")
	setType("00:00:5e:10:00:09", "access_point")

	now := it.T0.Add(5 * time.Minute)
	tp := topo(t, h, now)
	pc := devNode(t, tp, "192.168.0.4")
	opts, err := h.Store.DeviceLinkOptions(ctx, pc.DeviceID, now)
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, o := range opts {
		types = append(types, o.Type)
	}
	if len(types) < 4 || types[0] != "router" || types[1] != "switch" || types[2] != "access_point" {
		t.Errorf("option types = %v; want router, switch (the manual device), access_point first", types)
	}

	id := func(ip string) int64 { return devNode(t, tp, ip).DeviceID }
	swID := manualNode(t, tp, "zz switch").DeviceID
	h.Fact(func(tx *sql.Tx) error {
		return store.SetUserLink(ctx, tx, sw, inventory.MACKey("e0:00:5e:10:00:01"), at)
	})
	h.Fact(func(tx *sql.Tx) error {
		return store.SetUserLink(ctx, tx, inventory.MACKey("00:00:5e:10:00:04"), sw, at)
	})
	for _, c := range []struct {
		from, to int64
		loop     bool
	}{
		{swID, id("192.168.0.4"), true},              // pc → switch already: switch → pc closes it
		{id("192.168.0.1"), id("192.168.0.4"), true}, // router → pc → switch → router
		{id("192.168.0.7"), id("192.168.0.4"), false},
		{id("192.168.0.4"), id("192.168.0.9"), false},
	} {
		got, err := h.Store.LinkLoop(ctx, c.from, c.to)
		if err != nil || got != c.loop {
			t.Errorf("LinkLoop(%d, %d) = %v, %v; want %v", c.from, c.to, got, err, c.loop)
		}
	}
}
