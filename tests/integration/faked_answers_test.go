package integration_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/contract/contracttest"
)

// Feature 005, US2: a subnet a collector skipped because its network path faked answers shows as
// a scan error on the Collectors page and the home page until a normal run.

func TestFakedAnswersShownInUI(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.login()
	cfg := e.createCollector("desktop")
	code, r := e.upload(cfg.Token, contracttest.Fixture(t, "valid_faked_answers.json"))
	if code != http.StatusCreated || r.Status != contract.StatusStored {
		t.Fatalf("upload = %d %s", code, r.Status)
	}
	collectors := e.get("/collectors")
	for _, want := range []string{"scan error", "192.168.0.0/24", "answers faked on the network path"} {
		if !strings.Contains(collectors, want) {
			t.Errorf("Collectors page lacks %q", want)
		}
	}
	home := e.get("/")
	for _, want := range []string{"Scan problems", "desktop", "192.168.0.0/24"} {
		if !strings.Contains(home, want) {
			t.Errorf("home page lacks %q", want)
		}
	}
	if n := e.count(`SELECT count(*) FROM subnets WHERE cidr = '192.168.0.0/24'`); n != 0 {
		t.Error("a faked subnet became a subnet record")
	}

	// The next run scans the subnet normally: the warnings are gone.
	normal := contracttest.Modify(t, "valid_faked_answers.json", func(m map[string]any) {
		m["collection_id"] = "7c9e6a1b-2d3f-4e5a-8b7c-005fa4ed0002"
		for _, k := range []string{"started_at", "finished_at", "sent_at"} {
			tm, _ := time.Parse(time.RFC3339Nano, m[k].(string))
			m[k] = tm.Add(15 * time.Minute).Format("2006-01-02T15:04:05.000Z")
		}
		obs := m["observations"].([]any)[0].(map[string]any)
		tm, _ := time.Parse(time.RFC3339Nano, obs["observed_at"].(string))
		obs["observed_at"] = tm.Add(15 * time.Minute).Format("2006-01-02T15:04:05.000Z")
		m["subnets"].([]any)[1] = map[string]any{"cidr": "192.168.0.0/24", "method": "icmp_tcp", "complete": true, "hosts_probed": 254}
	})
	if code, r := e.upload(cfg.Token, normal); code != http.StatusCreated {
		t.Fatalf("normal upload = %d %s", code, r.Status)
	}
	if body := e.get("/collectors"); strings.Contains(body, "scan error") {
		t.Error("the scan error is still shown after a normal run")
	}
	if body := e.get("/"); strings.Contains(body, "Scan problems") {
		t.Error("the home notice is still shown after a normal run")
	}
}
