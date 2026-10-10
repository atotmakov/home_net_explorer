package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/clock"
	"github.com/atotmakov/home_net_explorer/internal/collect"
	"github.com/atotmakov/home_net_explorer/internal/collect/collecttest"
	"github.com/atotmakov/home_net_explorer/internal/collect/router/routertest"
	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/store"
)

// Feature 003: the NAS's built-in collector reads routers configured in the web UI, and never
// retries a rejected login (FR-014, research R8).

const builtinPassword = "pw-nas-8Kp!marker"

func routerApp(t *testing.T, f *routertest.Fake) *App {
	t.Helper()
	t0 := time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)
	fnet := collecttest.NewFakeNetwork()
	engine := &collect.Engine{Prober: fnet, Presence: fnet, Neighbors: fnet, Resolver: fnet,
		Routes:    collecttest.NewFakeRoutes("192.168.1.100", "eth0=192.168.1.20/24"),
		Clock:     clock.Real{},
		Collector: contract.CollectorInfo{Name: "nas", Version: "test", OS: "linux"}}
	a, err := New(context.Background(), Options{DataDir: filepath.Join(t.TempDir(), "data"), Clock: clock.NewFake(t0),
		Engine: engine, RouterTransport: f.Transport(), ScanInterval: 15 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	// The router device is known at 192.168.0.1 (seen through a desktop upload).
	run := &contract.CollectionRun{SchemaVersion: 1, CollectionID: "11111111-2222-4333-8444-555555555555",
		Collector: contract.CollectorInfo{Name: "desktop", Version: "test", OS: "windows"},
		StartedAt: t0, FinishedAt: t0.Add(time.Minute), SentAt: t0.Add(time.Minute), IntervalSeconds: 900,
		Vantage: contract.Vantage{Interfaces: []contract.Interface{}, Routes: []contract.Route{}},
		Subnets: []contract.SubnetScan{{CIDR: "192.168.0.0/24", Method: contract.MethodRouterTable, Complete: true, HostsProbed: 1}},
		Observations: []contract.Observation{{ObservedAt: t0, IP: "192.168.0.1", MAC: "00:00:5e:10:00:01",
			Method: contract.ObsRouterTable}}}
	raw, _ := json.Marshal(run)
	cid, err := a.Store.EnsureCollector(context.Background(), "desktop", store.KindRemote, 900)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Ingester.Ingest(context.Background(), cid, raw, run, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	setRouterPassword(t, a, builtinPassword)
	return a
}

func setRouterPassword(t *testing.T, a *App, pw string) {
	t.Helper()
	ctx := context.Background()
	if err := a.Store.Tx(ctx, func(tx *sql.Tx) error {
		return store.SaveRouter(ctx, tx, "mac:00:00:5e:10:00:01",
			store.RouterSettings{Model: "huawei-hg8145v5", Username: "root", Password: pw}, time.Now())
	}); err != nil {
		t.Fatal(err)
	}
}

func latestOutcome(t *testing.T, a *App) string {
	t.Helper()
	var o string
	if err := a.Store.DB().QueryRow(`SELECT rs.outcome FROM run_sources rs JOIN collection_runs r ON r.collection_id = rs.collection_id
		JOIN collectors c ON c.id = r.collector_id WHERE c.name = 'nas' ORDER BY r.rowid DESC LIMIT 1`).Scan(&o); err != nil {
		t.Fatal(err)
	}
	return o
}

func TestBuiltinScannerReadsUIRouter(t *testing.T) {
	f := routertest.New(t, "root", builtinPassword)
	a := routerApp(t, f)
	if _, err := a.Scanner.scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if o := latestOutcome(t, a); o != contract.OutcomeOK {
		t.Errorf("built-in read = %s, want ok", o)
	}
	if f.SessionOpen() {
		t.Error("session left open")
	}
}

func TestBuiltinScannerSkipsRejectedLogin(t *testing.T) {
	f := routertest.New(t, "root", builtinPassword)
	f.SetMode(routertest.WrongPassword)
	a := routerApp(t, f)
	ctx := context.Background()
	a.Scanner.scan(ctx)
	if o := latestOutcome(t, a); o != contract.OutcomeLoginRejected {
		t.Fatalf("first read = %s", o)
	}
	a.Scanner.scan(ctx)
	if f.Logins() != 1 {
		t.Errorf("logins = %d: a rejected login must not be retried", f.Logins())
	}
	if o := latestOutcome(t, a); o != contract.OutcomeSkippedAfterRejection {
		t.Errorf("second read = %s, want skipped_after_rejection", o)
	}
	setRouterPassword(t, a, "pw-changed")
	a.Scanner.scan(ctx)
	if f.Logins() != 2 {
		t.Errorf("logins after a password change = %d, want 2", f.Logins())
	}
}
