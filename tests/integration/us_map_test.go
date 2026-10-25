package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/contract/contracttest"
	"github.com/atotmakov/home_net_explorer/internal/store"
)

// Feature 010: the Map tab end to end (quickstart.md §1, §2, §5).

type mapDoc struct {
	Groups []store.TopoGroup `json:"groups"`
	Nodes  []store.TopoNode  `json:"nodes"`
	Edges  []store.TopoEdge  `json:"edges"`
}

func (e *env) mapDoc() mapDoc {
	e.t.Helper()
	var m mapDoc
	if err := json.Unmarshal([]byte(e.get("/ui/map.json")), &m); err != nil {
		e.t.Fatalf("map JSON: %v", err)
	}
	return m
}

func (m mapDoc) device(ip string) (store.TopoNode, bool) {
	for _, n := range m.Nodes {
		if n.Kind == "device" && n.IP == ip {
			return n, true
		}
	}
	return store.TopoNode{}, false
}

func (m mapDoc) has(kind, source, target string) bool {
	for _, x := range m.Edges {
		if x.Kind == kind && x.Source == source && x.Target == target {
			return true
		}
	}
	return false
}

func TestMapEndToEnd(t *testing.T) {
	e := newEnv(t, envOpts{})
	cfg := e.createCollector("desktop")
	e.clock.Set(time.Date(2026, 10, 5, 10, 6, 32, 0, time.UTC))

	// The desktop sees itself on 192.168.1.10 too; its card in 192.168.8.0/24 is another device.
	full := contracttest.Modify(t, "valid_full.json", func(m map[string]any) {
		obs := m["observations"].([]any)
		self := map[string]any{"ip": "192.168.1.10", "mac": "10:7b:44:01:02:03", "method": "arp",
			"observed_at": obs[0].(map[string]any)["observed_at"]}
		m["observations"] = append(obs, self)
	})
	if status, _ := e.upload(cfg.Token, full); status != http.StatusCreated {
		t.Fatalf("upload full = %d", status)
	}
	routerRun := func(id string) []byte {
		return withID(t, "valid_router.json", id, func(m map[string]any) {
			obs := m["observations"].([]any)
			router := map[string]any{"ip": "192.168.0.1", "mac": "e0:00:5e:10:00:01", "method": "router_table",
				"observed_at": obs[0].(map[string]any)["observed_at"]}
			m["observations"] = append(obs, router)
		})
	}
	e.clock.Set(time.Date(2026, 10, 9, 10, 1, 32, 0, time.UTC))
	if status, _ := e.upload(cfg.Token, routerRun("4b1d2c3e-0000-4000-8000-000000000101")); status != http.StatusCreated {
		t.Fatalf("upload router = %d", status)
	}

	sources := e.count(`SELECT count(*) FROM run_sources`)
	m := e.mapDoc()
	e.get("/map")
	if n := e.count(`SELECT count(*) FROM run_sources`); n != sources {
		t.Errorf("run_sources %d → %d: opening the map must not read any router (FR-011)", sources, n)
	}

	rows, err := e.app.Store.ListDevices(context.Background(), store.DeviceFilter{Now: e.clock.Now()})
	if err != nil {
		t.Fatal(err)
	}
	devices := 0
	for _, n := range m.Nodes {
		if n.Kind == "device" {
			devices++
		}
	}
	if devices != len(rows) {
		t.Errorf("map devices = %d, Devices list = %d", devices, len(rows))
	}

	router, ok := m.device("192.168.0.1")
	if !ok {
		t.Fatalf("no router node: %+v", m.Nodes)
	}
	lan1 := "port-" + strconv.FormatInt(router.DeviceID, 10) + "-LAN1"
	phone, _ := m.device("192.168.0.4")
	if !m.has("port", router.ID, lan1) || !m.has("port", lan1, phone.ID) {
		t.Errorf("no router → LAN1 → 192.168.0.4 edges: %+v", m.Edges)
	}
	desk, ok := m.device("192.168.1.10")
	if !ok {
		t.Fatal("no desktop node")
	}
	for _, e := range m.Edges {
		if e.Kind == "bridge" || e.Source == desk.ID && e.Kind != "manual" && e.Kind != "port" {
			t.Errorf("desktop or two-subnet link %+v (each network card is a device of its own)", e)
		}
	}

	// A manual link survives 3 more scans (SC-005).
	ap, _ := m.device("192.168.0.9")
	res, body := e.req("POST", "/devices/"+strconv.FormatInt(phone.DeviceID, 10)+"/link",
		url.Values{"via": {strconv.FormatInt(ap.DeviceID, 10)}})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("set link = %d\n%s", res.StatusCode, body)
	}
	for i := 2; i <= 4; i++ {
		if status, _ := e.upload(cfg.Token, routerRun(fmt.Sprintf("4b1d2c3e-0000-4000-8000-%012d", 100+i))); status != http.StatusCreated {
			t.Fatalf("upload router %d = %d", i, status)
		}
	}
	m = e.mapDoc()
	phone, _ = m.device("192.168.0.4")
	ap, _ = m.device("192.168.0.9")
	if !m.has("manual", phone.ID, ap.ID) {
		t.Errorf("manual link lost after 3 scans: %+v", m.Edges)
	}
	if m.has("port", lan1, phone.ID) {
		t.Error("the phone keeps its port edge next to the manual link")
	}
}

// SC-002: a 250-device map. The timing is skipped under -short (noisy shared runners); the
// generated run is written to $HNE_MAP_RUN_OUT for the in-browser check (quickstart.md §7).
func TestMapPerformance(t *testing.T) {
	e := newEnv(t, envOpts{})
	cfg := e.createCollector("perf")
	start := t0.Add(time.Minute)
	run := contract.CollectionRun{
		SchemaVersion:   contract.SchemaVersion,
		CollectionID:    "4b1d2c3e-0000-4000-8000-000000000250",
		Collector:       contract.CollectorInfo{Name: "perf", Version: "test", OS: "linux"},
		StartedAt:       start,
		FinishedAt:      start.Add(time.Minute),
		SentAt:          start.Add(time.Minute),
		IntervalSeconds: 900,
		Vantage:         contract.Vantage{Interfaces: []contract.Interface{}, Routes: []contract.Route{}},
		Subnets:         []contract.SubnetScan{{CIDR: "192.168.50.0/24", Method: contract.MethodARP, Complete: true, HostsProbed: 254}},
	}
	for i := 1; i <= 250; i++ {
		run.Observations = append(run.Observations, contract.Observation{
			ObservedAt: start.Add(time.Second), IP: fmt.Sprintf("192.168.50.%d", i),
			MAC: fmt.Sprintf("00:00:5e:50:%02x:%02x", i/256, i%256), Method: contract.ObsARP})
	}
	body, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	if out := os.Getenv("HNE_MAP_RUN_OUT"); out != "" {
		if err := os.WriteFile(out, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	e.clock.Set(start.Add(time.Minute + time.Second))
	if status, _ := e.upload(cfg.Token, body); status != http.StatusCreated {
		t.Fatalf("upload = %d", status)
	}

	began := time.Now()
	m := e.mapDoc()
	took := time.Since(began)
	devices := 0
	for _, n := range m.Nodes {
		if n.Kind == "device" {
			devices++
		}
	}
	if devices != 250 {
		t.Errorf("map devices = %d, want 250", devices)
	}
	if testing.Short() {
		t.Logf("250-device map JSON in %v (not asserted under -short)", took)
		return
	}
	if took > 500*time.Millisecond {
		t.Errorf("250-device map JSON took %v, budget 500ms (SC-002)", took)
	}
}
