//go:build hwtest

package router

import (
	"context"
	"os"
	"testing"

	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// TestHardwareHG8145V5 reads the real router once (quickstart.md §2 step 8). It runs only with
// -tags hwtest and HNE_HWTEST_ROUTER_URL/USER/PASS set, and logs counts only: never names,
// MACs or credentials.
func TestHardwareHG8145V5(t *testing.T) {
	cfg := Config{Model: ModelHG8145V5, URL: os.Getenv("HNE_HWTEST_ROUTER_URL"),
		Username: os.Getenv("HNE_HWTEST_ROUTER_USER"), Password: Secret(os.Getenv("HNE_HWTEST_ROUTER_PASS"))}
	if cfg.URL == "" || cfg.Username == "" || cfg.Password == "" {
		t.Skip("set HNE_HWTEST_ROUTER_URL, HNE_HWTEST_ROUTER_USER and HNE_HWTEST_ROUTER_PASS")
	}
	src, err := New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	res := src.Read(context.Background())
	if res.Outcome != contract.OutcomeOK {
		t.Fatalf("read = %s", res.Outcome)
	}
	t.Logf("%s at %s: %d online / %d offline devices", src.Model(), src.Address(), res.Online, res.Offline)
	if res.Online == 0 || len(res.Observations) != res.Online {
		t.Errorf("online = %d, observations = %d", res.Online, len(res.Observations))
	}
	for _, o := range res.Observations {
		if !contract.ValidMAC(o.MAC) {
			t.Errorf("a MAC is not lower-case aa:bb:cc:dd:ee:ff")
		}
	}
	// The session was released: a second read right away must log in again (SC-006).
	if res := src.Read(context.Background()); res.Outcome != contract.OutcomeOK {
		t.Errorf("second read = %s (was the first session left open?)", res.Outcome)
	}
}
