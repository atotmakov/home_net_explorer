package integration_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Feature 006: the Devices list and device page show online / online (stale) / offline; the
// filter and the home page counts use the same states.

// routerRun is valid_router.json (scanned 2026-10-09 10:00, interval 900 s) moved by d, with the
// router entries limited to keep (IPs); the ARP device 192.168.1.20 is always kept.
func routerRun(t *testing.T, id string, d time.Duration, keep ...string) []byte {
	return withID(t, "valid_router.json", id, func(m map[string]any) {
		shift := func(v any) string {
			tm, _ := time.Parse(time.RFC3339Nano, v.(string))
			return tm.Add(d).Format("2006-01-02T15:04:05.000Z")
		}
		for _, k := range []string{"started_at", "finished_at", "sent_at"} {
			m[k] = shift(m[k])
		}
		var obs []any
		for _, o := range m["observations"].([]any) {
			om := o.(map[string]any)
			om["observed_at"] = shift(om["observed_at"])
			ip := om["ip"].(string)
			if !strings.HasPrefix(ip, "192.168.0.") || keep == nil || contains(keep, ip) {
				obs = append(obs, om)
			}
		}
		m["observations"] = obs
	})
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

var scanned = time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)

func (e *env) deviceID(mac string) int64 {
	e.t.Helper()
	var id int64
	if err := e.app.Store.DB().QueryRow(`SELECT id FROM devices WHERE mac = ?`, mac).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func hasBadge(page, state, label string) bool {
	i := strings.Index(page, fmt.Sprintf(`class="badge s-%s"`, state))
	return i >= 0 && strings.Contains(page[i:], ">"+label+"</span>")
}

func TestStaleOnlineShown(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.login()
	cfg := e.createCollector("desktop")
	if code, r := e.upload(cfg.Token, routerRun(t, "6a000000-0000-4000-8000-000000000001", 0)); code != http.StatusCreated {
		t.Fatalf("upload = %d %s", code, r.Status)
	}
	host := fmt.Sprintf("/devices/%d", e.deviceID("00:00:5e:10:00:02")) // 192.168.0.4

	e.clock.Set(scanned.Add(5 * time.Minute))
	for _, path := range []string{"/devices", host} {
		page := e.get(path)
		if !hasBadge(page, "online", "online") || strings.Contains(page, "badge s-stale") {
			t.Errorf("%s 5 min after the scan: want only green online badges", path)
		}
	}

	e.clock.Set(scanned.Add(40 * time.Minute)) // > 2 × 900 s
	for _, path := range []string{"/devices", host} {
		page := e.get(path)
		if !hasBadge(page, "stale", "online (stale)") || !strings.Contains(page, `title="last seen`) {
			t.Errorf("%s 40 min after the scan: want the gray online (stale) badge with its age", path)
		}
		// FR-009: the text alone tells the states apart.
		if strings.Contains(page, ">online</span>") {
			t.Errorf("%s: a stale device is labelled plain online", path)
		}
	}

	if code, _ := e.upload(cfg.Token, routerRun(t, "6a000000-0000-4000-8000-000000000002", 45*time.Minute)); code != http.StatusCreated {
		t.Fatalf("second upload = %d", code)
	}
	e.clock.Set(scanned.Add(50 * time.Minute))
	if page := e.get(host); !hasBadge(page, "online", "online") {
		t.Error("a new scan did not make the device green again")
	}
}

func TestStatusFilterAndCounts(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.login()
	cfg := e.createCollector("desktop")
	if code, _ := e.upload(cfg.Token, routerRun(t, "6a000000-0000-4000-8000-000000000011", 0)); code != http.StatusCreated {
		t.Fatalf("upload = %d", code)
	}
	// 20 min later only 192.168.0.4 is listed by the router: 192.168.0.7 and .9 go offline.
	if code, _ := e.upload(cfg.Token, routerRun(t, "6a000000-0000-4000-8000-000000000012", 20*time.Minute, "192.168.0.4")); code != http.StatusCreated {
		t.Fatalf("upload = %d", code)
	}
	e.clock.Set(scanned.Add(25 * time.Minute))

	devices := e.get("/devices")
	for _, v := range []string{"online", "stale", "offline"} {
		if !strings.Contains(devices, fmt.Sprintf(`<option value="%s"`, v)) {
			t.Errorf("status filter lacks %q", v)
		}
	}
	for _, v := range []string{"new", "new_offline"} {
		if strings.Contains(devices, fmt.Sprintf(`<option value="%s"`, v)) {
			t.Errorf("status filter still offers %q", v)
		}
	}

	home := e.get("/")
	for _, state := range []string{"online", "stale", "offline"} {
		n := strings.Count(e.get("/devices?status="+state), `<a href="/devices/`)
		if want := fmt.Sprintf(`<span class="num s-%s">%d</span> %s`, state, n, state); !strings.Contains(home, want) {
			t.Errorf("home page lacks %q (the filter lists %d)", want, n)
		}
	}
	if n := strings.Count(e.get("/devices?status=offline"), `<a href="/devices/`); n != 2 {
		t.Errorf("offline devices = %d, want 2 (192.168.0.7 and .9)", n)
	}
}
