package app

import (
	"context"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	it "github.com/atotmakov/home_net_explorer/internal/inventory/inventorytest"
	"github.com/atotmakov/home_net_explorer/internal/store"
)

// Feature 006 (research R5): data derived under older rules is re-derived once at start-up.
func TestOneTimeRebuild(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	open := func() *App {
		t.Helper()
		a, err := New(ctx, Options{DataDir: dir, NoBuiltinScan: true})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	exec := func(a *App, q string) {
		t.Helper()
		if _, err := a.Store.DB().Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	status := func(a *App) string {
		t.Helper()
		var s string
		if err := a.Store.DB().QueryRow(`SELECT status FROM devices WHERE mac = '00:00:00:00:00:50'`).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}

	a := open()
	h := it.Over(t, a.Store, a.Applier, a.Ingester)
	phone := it.ARP("192.168.1.50", "00:00:00:00:00:50", "")
	h.Ingest(it.Run{Subnets: []contract.SubnetScan{it.Scan("192.168.1.0/24")}, Obs: []contract.Observation{phone}})
	h.Ingest(it.Run{Start: it.T0.Add(15 * time.Minute), Subnets: []contract.SubnetScan{it.Scan("192.168.1.0/24")}})
	// As derived by the old rule (3 missed scans): still online, no event, no rules version.
	exec(a, `UPDATE devices SET status = 'online'`)
	exec(a, `DELETE FROM events WHERE type = 'went_offline'`)
	exec(a, `DELETE FROM settings WHERE key = '`+store.SettingProjectionRules+`'`)
	a.Close()

	a = open()
	if s := status(a); s != "offline" {
		t.Errorf("after the upgrade start status = %s, want offline (re-derived by the new rule)", s)
	}
	if v, _, _ := a.Store.Setting(ctx, store.SettingProjectionRules); v != "9" {
		t.Errorf("projection_rules = %q, want 9", v)
	}

	// Already at the current rules: no rebuild on the next start.
	exec(a, `UPDATE devices SET status = 'online'`)
	a.Close()
	a = open()
	defer a.Close()
	if s := status(a); s != "online" {
		t.Errorf("a second start rebuilt again (status = %s)", s)
	}
}
