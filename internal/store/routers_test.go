package store_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/inventory"
	it "github.com/atotmakov/home_net_explorer/internal/inventory/inventorytest"
	"github.com/atotmakov/home_net_explorer/internal/store"
)

// Feature 003: router settings configured in the web UI.

const routerKey = "mac:00:00:5e:10:00:01"

func save(t *testing.T, h *it.Harness, key string, r store.RouterSettings) error {
	t.Helper()
	return h.Store.Tx(context.Background(), func(tx *sql.Tx) error {
		return store.SaveRouter(context.Background(), tx, key, r, it.T0.Add(time.Hour))
	})
}

func routerScan(cidr string) contract.SubnetScan {
	return contract.SubnetScan{CIDR: cidr, Method: contract.MethodARP, Complete: true, HostsProbed: 254}
}

func seen(ip, mac string) contract.Observation {
	return contract.Observation{IP: ip, MAC: mac, Method: contract.ObsARP}
}

func TestSaveRouterReplaceAndDelete(t *testing.T) {
	ctx := context.Background()
	h := it.New(t)
	r := store.RouterSettings{Model: "huawei-hg8145v5", Username: "root", Password: "pw-1"}
	if err := save(t, h, routerKey, r); err != nil {
		t.Fatal(err)
	}
	v, ok, err := h.Store.GetRouterSettings(ctx, routerKey)
	if err != nil || !ok || v.Model != "huawei-hg8145v5" || v.Username != "root" || !v.PasswordSet || v.ID == 0 {
		t.Fatalf("settings = %+v, %v, %v", v, ok, err)
	}
	id := v.ID

	// An empty password keeps the stored one; the id stays the same.
	if err := save(t, h, routerKey, store.RouterSettings{Model: "huawei-hg8145v5", Username: "admin"}); err != nil {
		t.Fatal(err)
	}
	v, _, _ = h.Store.GetRouterSettings(ctx, routerKey)
	if v.ID != id || v.Username != "admin" {
		t.Errorf("after replace: %+v (id must stay %d)", v, id)
	}
	if l, err := h.Store.RouterLogin(ctx, id); err != nil || l.Username != "admin" || l.Password != "pw-1" {
		t.Errorf("login = %+v, %v; an empty password keeps the stored one", l, err)
	}
	if err := save(t, h, routerKey, store.RouterSettings{Model: "huawei-hg8145v5", Username: "admin", Password: "pw-2"}); err != nil {
		t.Fatal(err)
	}
	if l, _ := h.Store.RouterLogin(ctx, id); l.Password != "pw-2" {
		t.Errorf("password not replaced: %+v", l)
	}

	if err := save(t, h, "mac:00:00:5e:10:00:99", store.RouterSettings{Model: "huawei-hg8145v5", Username: "root"}); err == nil {
		t.Error("saved a router without any password")
	}

	if err := h.Store.Tx(ctx, func(tx *sql.Tx) error { return store.DeleteRouter(ctx, tx, routerKey) }); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := h.Store.GetRouterSettings(ctx, routerKey); ok {
		t.Error("router still configured after delete")
	}
	if _, err := h.Store.RouterLogin(ctx, id); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("login of a deleted router: %v, want ErrNotFound", err)
	}
}

// The view never carries the password (FR-004).
func TestRouterViewHasNoPassword(t *testing.T) {
	typ := reflect.TypeOf(store.RouterView{})
	for i := 0; i < typ.NumField(); i++ {
		if f := typ.Field(i).Name; strings.Contains(f, "Password") && f != "PasswordSet" {
			t.Errorf("RouterView has field %s", f)
		}
	}
}

// The address follows the device: preferred subnet, else the subnet where it was last seen
// (research R3), not the most recently changed address.
func TestListRoutersResolvesAddress(t *testing.T) {
	ctx := context.Background()
	h := it.New(t)
	const mac = "00:00:5e:10:00:01"
	lan, isp := "192.168.1.0/24", "192.168.0.0/24"
	h.Ingest(it.Run{Collector: "desktop", Subnets: []contract.SubnetScan{routerScan(lan), routerScan(isp)},
		Obs: []contract.Observation{seen("192.168.1.1", mac), seen("192.168.0.1", mac)}})
	// The LAN address changes later (newer as_of) …
	h.Ingest(it.Run{Collector: "desktop", Start: it.T0.Add(15 * time.Minute), Subnets: []contract.SubnetScan{routerScan(lan)},
		Obs: []contract.Observation{seen("192.168.1.2", mac)}})
	// … but the device is seen last on the ISP subnet.
	h.Ingest(it.Run{Collector: "desktop", Start: it.T0.Add(30 * time.Minute), Subnets: []contract.SubnetScan{routerScan(isp)},
		Obs: []contract.Observation{seen("192.168.0.1", mac)}})

	if err := save(t, h, routerKey, store.RouterSettings{Model: "huawei-hg8145v5", Username: "root", Password: "pw"}); err != nil {
		t.Fatal(err)
	}
	rs, err := h.Store.ListRouters(ctx)
	if err != nil || len(rs) != 1 {
		t.Fatalf("routers = %+v, %v", rs, err)
	}
	want := contract.RouterRef{ID: rs[0].ID, Model: "huawei-hg8145v5", Address: "192.168.0.1", Subnet: isp}
	if rs[0] != want {
		t.Errorf("router = %+v, want %+v (last seen, not last changed)", rs[0], want)
	}

	if err := save(t, h, routerKey, store.RouterSettings{Model: "huawei-hg8145v5", Subnet: lan, Username: "root"}); err != nil {
		t.Fatal(err)
	}
	if rs, _ := h.Store.ListRouters(ctx); len(rs) != 1 || rs[0].Address != "192.168.1.2" || rs[0].Subnet != lan {
		t.Errorf("preferred subnet: %+v", rs)
	}

	// A router whose device has no current address is left out.
	if err := save(t, h, "mac:00:00:5e:10:00:77", store.RouterSettings{Model: "huawei-hg8145v5", Username: "root", Password: "pw"}); err != nil {
		t.Fatal(err)
	}
	if rs, _ := h.Store.ListRouters(ctx); len(rs) != 1 {
		t.Errorf("routers = %+v, want only the one with an address", rs)
	}

	// Rebuild leaves router settings alone.
	if err := inventory.Rebuild(ctx, h.Store, h.Applier); err != nil {
		t.Fatal(err)
	}
	if rs, _ := h.Store.ListRouters(ctx); len(rs) != 1 {
		t.Errorf("after rebuild: %+v", rs)
	}
}

// A router configured on a weak (IP-only) device follows it when it is merged into the MAC device.
func TestListRoutersFollowsMerge(t *testing.T) {
	ctx := context.Background()
	h := it.New(t)
	isp := "192.168.0.0/24"
	h.Ingest(it.Run{Collector: "desktop", Subnets: []contract.SubnetScan{{CIDR: isp, Method: contract.MethodICMPTCP, Complete: true, HostsProbed: 254}},
		Obs: []contract.Observation{{IP: "192.168.0.1", Method: contract.ObsICMP}}})
	if err := save(t, h, "ip:192.168.0.0/24:192.168.0.1", store.RouterSettings{Model: "huawei-hg8145v5", Username: "root", Password: "pw"}); err != nil {
		t.Fatal(err)
	}
	h.Ingest(it.Run{Collector: "desktop", Start: it.T0.Add(15 * time.Minute), Subnets: []contract.SubnetScan{routerScan(isp)},
		Obs: []contract.Observation{seen("192.168.0.1", "00:00:5e:10:00:01")}})
	rs, err := h.Store.ListRouters(ctx)
	if err != nil || len(rs) != 1 || rs[0].Address != "192.168.0.1" {
		t.Errorf("routers after merge = %+v, %v", rs, err)
	}
}

func TestRouterStatusLatestPerCollector(t *testing.T) {
	ctx := context.Background()
	h := it.New(t)
	isp := "192.168.0.0/24"
	src := func(outcome string, online, offline int) []contract.RunSource {
		return []contract.RunSource{{Type: "router", Model: "huawei-hg8145v5", Address: "192.168.0.1", Subnet: isp,
			Outcome: outcome, Online: online, Offline: offline}}
	}
	ingest := func(collector string, m int, sources []contract.RunSource) {
		run := h.Build(it.Run{Collector: collector, Start: it.T0.Add(time.Duration(m) * time.Minute),
			Subnets: []contract.SubnetScan{{CIDR: isp, Method: contract.MethodRouterTable}}})
		run.Sources = sources
		h.IngestRun(run, run.FinishedAt)
	}
	ingest("desktop", 0, src("ok", 14, 16))
	ingest("desktop", 15, src("login_rejected", 0, 0))
	ingest("nas", 5, src("unreachable", 0, 0))

	got, err := h.Store.RouterStatus(ctx, "192.168.0.1")
	if err != nil || len(got) != 2 {
		t.Fatalf("status = %+v, %v", got, err)
	}
	if got[0].Collector != "desktop" || got[0].Outcome != "login_rejected" || got[1].Collector != "nas" || got[1].Outcome != "unreachable" {
		t.Errorf("status = %+v, want the latest read per collector, by name", got)
	}
	if got[0].At.IsZero() {
		t.Error("status lacks the read time")
	}
	if none, _ := h.Store.RouterStatus(ctx, "192.168.0.99"); len(none) != 0 {
		t.Errorf("status of an unknown address = %+v", none)
	}
}
