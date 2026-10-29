package web_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/clock"
	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/inventory"
	it "github.com/atotmakov/home_net_explorer/internal/inventory/inventorytest"
	"github.com/atotmakov/home_net_explorer/internal/web"
)

// Feature 015 (contracts/web-ui-changes.md): the "Open ports" card and "Check ports now".

type fakeScanner struct {
	ports      web.TriggerResult
	status     web.ScanStatus
	portsCalls int
}

func (f *fakeScanner) Trigger() web.TriggerResult { return web.TriggerStarted }
func (f *fakeScanner) TriggerPorts() web.TriggerResult {
	f.portsCalls++
	return f.ports
}
func (f *fakeScanner) Status() web.ScanStatus                    { return f.status }
func (f *fakeScanner) Paused(context.Context) (bool, time.Time)  { return false, time.Time{} }
func (f *fakeScanner) SetPaused(context.Context, bool) error     { return nil }

func portsServer(t *testing.T, sc web.Scanner) (*it.Harness, http.Handler) {
	t.Helper()
	h := it.New(t)
	srv, err := web.New(web.Options{Store: h.Store, Ingester: h.Ingester, Clock: clock.NewFake(it.T0.Add(time.Hour)), Scanner: sc})
	if err != nil {
		t.Fatal(err)
	}
	return h, srv.Handler()
}

func devicePage(t *testing.T, h *it.Harness, hd http.Handler, ip string) string {
	t.Helper()
	res, body := get(t, hd, "/devices/"+strconv.FormatInt(deviceID(t, h, ip), 10))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("device page %s = %d", ip, res.StatusCode)
	}
	return body
}

func TestDevicePagePorts(t *testing.T) {
	h, hd := portsServer(t, nil)
	h.Ingest(it.Run{Subnets: []contract.SubnetScan{it.Scan("192.168.1.0/24")}, Obs: []contract.Observation{
		it.ARP("192.168.1.79", "00:00:5e:00:53:79", "cam"), it.ARP("192.168.1.80", "00:00:5e:00:53:80", "")}})
	h.Ingest(it.Run{Start: it.T0.Add(10 * time.Minute), PortChecks: []contract.PortCheck{{IP: "192.168.1.79",
		MAC: "00:00:5e:00:53:79", Complete: true, Open: []contract.OpenPort{
			{Port: 22, Proto: "tcp", Service: "ssh", ServiceSource: "banner"},
			{Port: 80, Proto: "tcp", Service: "http", ServiceSource: "table"},
			{Port: 8888, Proto: "tcp", Service: "", ServiceSource: "table"}}}}})

	body := devicePage(t, h, hd, "192.168.1.79")
	for _, want := range []string{`id="ports"`, "Open ports", "22/tcp", "ssh", "80/tcp", "http", "8888/tcp", "unknown",
		"192.168.1.79", "Last checked", "nas"} {
		if !strings.Contains(body, want) {
			t.Errorf("camera page lacks %q", want)
		}
	}
	if body := devicePage(t, h, hd, "192.168.1.80"); !strings.Contains(body, "Ports not checked yet") {
		t.Error(`a device never checked does not say "Ports not checked yet"`)
	}
	h.Ingest(it.Run{Start: it.T0.Add(20 * time.Minute), PortChecks: []contract.PortCheck{{IP: "192.168.1.80",
		MAC: "00:00:5e:00:53:80", Complete: true}}})
	if body := devicePage(t, h, hd, "192.168.1.80"); !strings.Contains(body, "No open ports found") {
		t.Error(`a complete check with nothing open does not say "No open ports found"`)
	}

	seedRouterLAN(h) // 192.168.0.x is only a router's list
	if body := devicePage(t, h, hd, "192.168.0.4"); !strings.Contains(body, "Not reachable for port checks") {
		t.Error("a device only listed by a router does not say it cannot be port-checked")
	}

	var key string
	h.Fact(func(tx *sql.Tx) error {
		var err error
		key, err = inventory.AddManualDevice(context.Background(), tx, inventory.ManualDevice{Name: "switch"}, it.T0.Add(time.Hour))
		return err
	})
	var id int64
	h.Store.DB().QueryRow(`SELECT id FROM devices WHERE identity_key = ?`, key).Scan(&id)
	if _, body := get(t, hd, "/devices/"+strconv.FormatInt(id, 10)); strings.Contains(body, `id="ports"`) {
		t.Error("a device added by hand shows the ports card")
	}
}

func TestCheckPortsNow(t *testing.T) {
	sc := &fakeScanner{ports: web.TriggerStarted}
	_, hd := portsServer(t, sc)
	if _, body := get(t, hd, "/"); !strings.Contains(body, `hx-post="/scan/ports"`) || !strings.Contains(body, "Check ports now") {
		t.Error(`home page lacks the "Check ports now" button`)
	}
	res := post(t, hd, "/scan/ports", url.Values{}, "http://"+host, "")
	if res.StatusCode != http.StatusAccepted || sc.portsCalls != 1 {
		t.Errorf("POST /scan/ports = %d (calls %d), want 202 and one trigger", res.StatusCode, sc.portsCalls)
	}
	sc.ports = web.TriggerRunning
	if res := post(t, hd, "/scan/ports", url.Values{}, "http://"+host, ""); res.StatusCode != http.StatusConflict {
		t.Errorf("POST /scan/ports while a port job runs = %d, want 409", res.StatusCode)
	}
	if res := post(t, hd, "/scan/ports", url.Values{}, "http://evil.example", ""); res.StatusCode != http.StatusForbidden {
		t.Errorf("cross-site POST /scan/ports = %d, want 403", res.StatusCode)
	}
	sc.status = web.ScanStatus{PortsRunning: true, PortsDone: 1, PortsTotal: 2}
	if _, body := get(t, hd, "/"); !strings.Contains(body, "Checking ports: 1 of 2 devices") {
		t.Error("home page does not show the port job's progress")
	}

	_, none := portsServer(t, nil)
	if res := post(t, none, "/scan/ports", url.Values{}, "http://"+host, ""); res.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("POST /scan/ports without the built-in collector = %d, want 503", res.StatusCode)
	}
}
