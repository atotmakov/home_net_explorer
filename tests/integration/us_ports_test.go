package integration_test

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/contract/contracttest"
)

// Feature 015, US1 end to end: a presence upload makes a device due in the ping; its port run
// lists the open ports on its page.
func TestPortsEndToEnd(t *testing.T) {
	e := newEnv(t, envOpts{})
	cfg := e.createCollector("desktop")
	e.clock.Set(time.Date(2026, 10, 27, 3, 0, 30, 0, time.UTC))
	presence := contracttest.Modify(t, "valid_minimal.json", func(m map[string]any) {
		m["collector"].(map[string]any)["name"] = "desktop"
		m["started_at"], m["finished_at"], m["sent_at"] = "2026-10-27T03:00:00.000Z", "2026-10-27T03:00:20.000Z", "2026-10-27T03:00:21.000Z"
		m["observations"] = []any{map[string]any{"observed_at": "2026-10-27T03:00:10.000Z", "ip": "192.168.1.79",
			"mac": "00:00:5e:00:53:79", "method": "arp"}}
	})
	if status, res := e.upload(cfg.Token, presence); status != http.StatusCreated {
		t.Fatalf("presence upload = %d %+v", status, res)
	}

	var ping contract.PingResponse
	if status := e.api(cfg.Token, "/api/v1/ping", &ping); status != http.StatusOK || ping.PortScan == nil ||
		!slices.Contains(ping.PortScan.Due, "192.168.1.79") {
		t.Fatalf("ping = %d %+v, want 192.168.1.79 due", status, ping.PortScan)
	}

	e.clock.Set(time.Date(2026, 10, 27, 3, 2, 0, 0, time.UTC))
	portRun := contracttest.Modify(t, "valid_port_run.json", func(m map[string]any) {
		m["collector"].(map[string]any)["name"] = "desktop"
		m["started_at"], m["finished_at"], m["sent_at"] = "2026-10-27T03:01:00.000Z", "2026-10-27T03:01:50.000Z", "2026-10-27T03:01:51.000Z"
		c := m["port_checks"].([]any)[0].(map[string]any)
		c["started_at"], c["finished_at"] = "2026-10-27T03:01:00.000Z", "2026-10-27T03:01:41.000Z"
	})
	if status, res := e.upload(cfg.Token, portRun); status != http.StatusCreated {
		t.Fatalf("port run upload = %d %+v", status, res)
	}

	var id int64
	e.app.Store.DB().QueryRow(`SELECT id FROM devices WHERE mac = '00:00:5e:00:53:79'`).Scan(&id)
	page := e.get("/devices/" + strconv.FormatInt(id, 10))
	for _, want := range []string{"80/tcp", "554/tcp", "rtsp", "Last checked"} {
		if !strings.Contains(page, want) {
			t.Errorf("device page lacks %q", want)
		}
	}
}
