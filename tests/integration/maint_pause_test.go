package integration_test

import (
	"bytes"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/contract/contracttest"
)

// Feature 004, US2: pause and resume the NAS's built-in scanner from the web UI.

func TestMaintPauseResume(t *testing.T) {
	var logs bytes.Buffer
	e := newEnv(t, envOpts{builtinScan: true, log: &logs})
	e.login()
	e.waitFor("the start-up scan", func() bool { return e.runCount() == 1 })

	// Refused without the owner's session or from another site.
	e.anonPost("/maintenance/pause", url.Values{})
	if code := e.crossSitePost("/maintenance/pause", url.Values{}); code != http.StatusForbidden {
		t.Errorf("cross-site pause = %d, want 403", code)
	}
	if body := e.get("/"); !strings.Contains(body, "Scan now") {
		t.Fatal("paused by a refused request")
	}

	res, _ := e.post("/maintenance/pause", url.Values{})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/settings?done=paused" {
		t.Fatalf("pause = %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	home := e.get("/")
	if !strings.Contains(home, "Paused") || !strings.Contains(home, `action="/maintenance/resume"`) || strings.Contains(home, "Scan now") {
		t.Errorf("home page while paused:\n%s", home)
	}
	if body := e.get("/settings"); !strings.Contains(body, "paused since") || !strings.Contains(body, `action="/maintenance/resume"`) {
		t.Error("the Settings card does not show the paused scanner with Resume")
	}
	if body := e.get("/collectors"); !strings.Contains(body, ">paused<") {
		t.Error("the Collectors page does not show nas as paused")
	}
	_, frag := e.req("POST", "/scan", url.Values{})
	if !strings.Contains(frag, "Paused") {
		t.Errorf("Scan now while paused returned %q", frag)
	}
	e.clock.Advance(45 * time.Minute) // three scan intervals
	time.Sleep(200 * time.Millisecond)
	if n := e.runCount(); n != 1 {
		t.Errorf("paused, yet %d runs", n)
	}

	// Remote collectors keep working.
	cfg := e.createCollector("desktop")
	if code, r := e.upload(cfg.Token, contracttest.Fixture(t, "valid_router.json")); code != http.StatusCreated || r.Status != contract.StatusStored {
		t.Errorf("remote upload while paused = %d %s", code, r.Status)
	}

	res, _ = e.post("/maintenance/resume", url.Values{})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/settings?done=resumed" {
		t.Fatalf("resume = %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	if body := e.get("/"); !strings.Contains(body, "Scan now") {
		t.Error("Scan now is not back after resume")
	}
	if !strings.Contains(logs.String(), "action=pause") || !strings.Contains(logs.String(), "action=resume") {
		t.Error("pause/resume not logged")
	}
}

func TestMaintPauseNeedsBuiltinCollector(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.login()
	if body := e.get("/settings"); strings.Contains(body, `action="/maintenance/pause"`) {
		t.Error("the card offers Pause although the built-in collector is disabled")
	}
	for _, p := range []string{"/maintenance/pause", "/maintenance/resume"} {
		if res, _ := e.post(p, url.Values{}); res.StatusCode != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", p, res.StatusCode)
		}
	}
}
