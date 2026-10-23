package web_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/clock"
	"github.com/atotmakov/home_net_explorer/internal/contract"
	it "github.com/atotmakov/home_net_explorer/internal/inventory/inventorytest"
	"github.com/atotmakov/home_net_explorer/internal/web"
)

// Feature 010: the Map tab (contracts/web-ui-changes.md).

func mapServer(t *testing.T) (*it.Harness, http.Handler) {
	t.Helper()
	h := it.New(t)
	clk := clock.NewFake(it.T0.Add(5 * time.Minute))
	srv, err := web.New(web.Options{Store: h.Store, Ingester: h.Ingester, Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	return h, srv.Handler()
}

// seedRouterLAN ingests a router read of 192.168.0.0/24: the router .1, a phone .4 (LAN1) and an
// access point .9 (LAN2).
func seedRouterLAN(h *it.Harness) {
	run := h.Build(it.Run{Collector: "desktop", Subnets: []contract.SubnetScan{{CIDR: "192.168.0.0/24",
		Method: contract.MethodRouterTable, Complete: true, HostsProbed: 29}}, Obs: []contract.Observation{
		{IP: "192.168.0.1", MAC: "e0:00:5e:10:00:01", Method: contract.ObsRouterTable},
		{IP: "192.168.0.4", MAC: "00:00:5e:10:00:04", Via: "LAN1", Method: contract.ObsRouterTable},
		{IP: "192.168.0.9", MAC: "00:00:5e:10:00:09", Via: "LAN2", Method: contract.ObsRouterTable},
	}})
	run.Sources = []contract.RunSource{{Type: "router", Model: "huawei-hg8145v5", Address: "192.168.0.1",
		Subnet: "192.168.0.0/24", Outcome: contract.OutcomeOK, Online: 3}}
	h.IngestRun(run, run.FinishedAt.Add(time.Second))
}

func deviceID(t *testing.T, h *it.Harness, ip string) int64 {
	t.Helper()
	var id int64
	if err := h.Store.DB().QueryRow(`SELECT device_id FROM device_addresses WHERE ip = ? AND current = 1`, ip).Scan(&id); err != nil {
		t.Fatalf("device %s: %v", ip, err)
	}
	return id
}

func mapJSON(t *testing.T, hd http.Handler) map[string]any {
	t.Helper()
	res, body := get(t, hd, "/ui/map.json")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("/ui/map.json = %d\n%s", res.StatusCode, body)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, body)
	}
	return m
}

func TestMapJSONShape(t *testing.T) {
	h, hd := mapServer(t)
	res, body := get(t, hd, "/ui/map.json?scope=online")
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") ||
		res.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("empty map = %d %q %q", res.StatusCode, res.Header.Get("Content-Type"), res.Header.Get("Cache-Control"))
	}
	compact := strings.Join(strings.Fields(body), "")
	if compact != `{"groups":[],"nodes":[],"edges":[]}` {
		t.Errorf("empty map = %s, want empty arrays (never null)", body)
	}

	h.Ingest(it.Run{Subnets: []contract.SubnetScan{it.Scan("192.168.1.0/24")},
		Obs: []contract.Observation{it.ARP("192.168.1.20", "00:11:32:aa:bb:cc", "nas.lan")}})
	m := mapJSON(t, hd)
	nodes := m["nodes"].([]any)
	if len(nodes) != 1 {
		t.Fatalf("nodes = %v", nodes)
	}
	n := nodes[0].(map[string]any)
	var keys []string
	for k := range n {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	want := []string{"device_id", "gateway_of", "id", "ip", "ips", "kind", "label", "last_seen_ago", "mac", "parent", "state", "type", "vendor"}
	if !slices.Equal(keys, want) {
		t.Errorf("device node keys = %v, want %v", keys, want)
	}
	if n["kind"] != "device" || n["state"] != "online" || n["ip"] != "192.168.1.20" || n["last_seen_ago"] != "5 mins" {
		t.Errorf("device node = %v", n)
	}
	if _, ok := n["gateway_of"].([]any); !ok {
		t.Errorf("gateway_of = %v, want an array", n["gateway_of"])
	}
	if g := m["groups"].([]any); len(g) != 1 || g[0].(map[string]any)["cidr"] != "192.168.1.0/24" {
		t.Errorf("groups = %v", g)
	}
}

func TestMapPage(t *testing.T) {
	h, hd := mapServer(t)
	res, body := get(t, hd, "/map")
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "No devices yet") {
		t.Fatalf("empty /map = %d, want the no-devices notice\n%s", res.StatusCode, body)
	}

	seedRouterLAN(h)
	res, body = get(t, hd, "/map")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("/map = %d", res.StatusCode)
	}
	for _, want := range []string{
		`<a href="/map" aria-current="page">`,
		`<div id="map"`,
		`<script src="/static/vendor/cytoscape.min.js"></script>`,
		`<script type="module" src="/static/map.js"></script>`,
		`<aside id="map-details"`,
		"All devices", "Online only",
		"online", "offline", "gateway", "two subnets", "port / Wi-Fi", "last known", "manual",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/map lacks %q", want)
		}
	}
	if strings.Contains(body, "No devices yet") {
		t.Error("/map shows the no-devices notice with devices")
	}
	if !strings.Contains(body, `value="all" checked`) || strings.Contains(body, `value="online" checked`) {
		t.Error("the map must open on All devices")
	}
	for _, bad := range []string{`src="http://`, `src="https://`, `src="//`} {
		if strings.Contains(body, bad) {
			t.Errorf("/map loads a remote script (%s)", bad)
		}
	}
	for _, js := range []string{"/static/map.js", "/static/map-model.js"} {
		res, _ := get(t, hd, js)
		if res.StatusCode != http.StatusOK || !strings.Contains(res.Header.Get("Content-Type"), "javascript") {
			t.Errorf("%s = %d %q", js, res.StatusCode, res.Header.Get("Content-Type"))
		}
	}
}

func post(t *testing.T, hd http.Handler, path string, form url.Values, origin, referer string) *http.Response {
	t.Helper()
	req := httptest.NewRequest("POST", "http://"+host+path, strings.NewReader(form.Encode()))
	req.Host = host
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	rec := httptest.NewRecorder()
	hd.ServeHTTP(rec, req)
	io.ReadAll(rec.Result().Body)
	return rec.Result()
}

func TestDeviceLinkForm(t *testing.T) {
	h, hd := mapServer(t)
	seedRouterLAN(h)
	phone, ap, router := deviceID(t, h, "192.168.0.4"), deviceID(t, h, "192.168.0.9"), deviceID(t, h, "192.168.0.1")
	id := func(n int64) string { return strconv.FormatInt(n, 10) }
	link := "/devices/" + id(phone) + "/link"
	self := "http://" + host

	_, page := get(t, hd, "/devices/"+id(phone))
	if !strings.Contains(page, `<select name="via"`) || !strings.Contains(page, `value="`+id(ap)+`"`) ||
		!strings.Contains(page, `value="`+id(router)+`"`) || strings.Contains(page, `value="`+id(phone)+`"`) {
		t.Fatalf("device page has no Connected via select with the other devices:\n%s", page)
	}

	res := post(t, hd, link, url.Values{"via": {id(ap)}}, self, self+"/map")
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/map" {
		t.Errorf("set link = %d → %q, want 303 → /map (same-origin Referer)", res.StatusCode, res.Header.Get("Location"))
	}
	hasManual := func() bool {
		for _, e := range mapJSON(t, hd)["edges"].([]any) {
			e := e.(map[string]any)
			if e["kind"] == "manual" && e["source"] == "dev-"+id(phone) && e["target"] == "dev-"+id(ap) {
				return true
			}
		}
		return false
	}
	if !hasManual() {
		t.Error("no manual edge after setting the link")
	}
	if _, page = get(t, hd, "/devices/"+id(phone)); !strings.Contains(page, `value="`+id(ap)+`" selected`) {
		t.Error("the current link is not selected on the device page")
	}

	for _, ref := range []string{"", "http://evil.example/map"} {
		res = post(t, hd, link, url.Values{"via": {id(ap)}}, self, ref)
		if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/devices/"+id(phone) {
			t.Errorf("Referer %q: %d → %q, want 303 → the device page", ref, res.StatusCode, res.Header.Get("Location"))
		}
	}

	if res = post(t, hd, link, url.Values{"via": {""}}, self, ""); res.StatusCode != http.StatusSeeOther || hasManual() {
		t.Errorf("clear link = %d, manual edge left: %v", res.StatusCode, hasManual())
	}
	for _, via := range []string{id(phone), "999999", "abc"} {
		if res = post(t, hd, link, url.Values{"via": {via}}, self, ""); res.StatusCode != http.StatusBadRequest {
			t.Errorf("via=%s = %d, want 400", via, res.StatusCode)
		}
	}
	if res = post(t, hd, "/devices/999999/link", url.Values{"via": {id(ap)}}, self, ""); res.StatusCode != http.StatusNotFound {
		t.Errorf("unknown device = %d, want 404", res.StatusCode)
	}
	if res = post(t, hd, link, url.Values{"via": {id(ap)}}, "http://evil.example", ""); res.StatusCode != http.StatusForbidden {
		t.Errorf("cross-site link = %d, want 403", res.StatusCode)
	}
	if hasManual() {
		t.Error("a refused request changed the link")
	}
}
