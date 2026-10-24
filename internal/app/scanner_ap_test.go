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
	"github.com/atotmakov/home_net_explorer/internal/collect/accesspoint/eaptest"
	"github.com/atotmakov/home_net_explorer/internal/collect/collecttest"
	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/store"
)

// Feature 012: the NAS's built-in collector reads the access points configured in the web UI,
// like routers, and never retries a rejected login.
func TestBuiltinScannerReadsAccessPoint(t *testing.T) {
	ctx := context.Background()
	t0 := time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)
	ap := eaptest.Start(t, eaptest.Old, "admin", builtinPassword)
	ap.SetClients(eaptest.Client("00-00-5E-10-00-13", "192.168.1.13", "Home", 1, "phone"))
	fnet := collecttest.NewFakeNetwork()
	clk := clock.NewFake(t0.Add(time.Hour))
	engine := &collect.Engine{Prober: fnet, Presence: fnet, Neighbors: fnet, Resolver: fnet,
		Routes:    collecttest.NewFakeRoutes("192.168.1.100", "eth0=192.168.1.20/24"),
		Clock:     clk,
		Collector: contract.CollectorInfo{Name: "nas", Version: "test", OS: "linux"}}
	a, err := New(ctx, Options{DataDir: filepath.Join(t.TempDir(), "data"), Clock: clk,
		Engine: engine, AccessPointTransport: ap.Transport(), ScanInterval: 15 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	// The access point is known at 192.168.1.5.
	run := &contract.CollectionRun{SchemaVersion: 1, CollectionID: "11111111-2222-4333-8444-555555555556",
		Collector: contract.CollectorInfo{Name: "desktop", Version: "test", OS: "windows"},
		StartedAt: t0, FinishedAt: t0.Add(time.Minute), SentAt: t0.Add(time.Minute), IntervalSeconds: 900,
		Vantage:      contract.Vantage{Interfaces: []contract.Interface{}, Routes: []contract.Route{}},
		Subnets:      []contract.SubnetScan{{CIDR: "192.168.1.0/24", Method: contract.MethodARP, Complete: true, HostsProbed: 254}},
		Observations: []contract.Observation{{ObservedAt: t0, IP: "192.168.1.5", MAC: "00:00:5e:10:00:05", Method: contract.ObsARP}}}
	raw, _ := json.Marshal(run)
	cid, err := a.Store.EnsureCollector(ctx, "desktop", store.KindRemote, 900)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Ingester.Ingest(ctx, cid, raw, run, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	save := func(pw string) {
		t.Helper()
		if err := a.Store.Tx(ctx, func(tx *sql.Tx) error {
			return store.SaveRouter(ctx, tx, "mac:00:00:5e:10:00:05",
				store.RouterSettings{Model: "tplink-eap225", Username: "admin", Password: pw}, time.Now())
		}); err != nil {
			t.Fatal(err)
		}
	}
	save(builtinPassword)

	if _, err := a.Scanner.scan(ctx); err != nil {
		t.Fatal(err)
	}
	if o := latestOutcome(t, a); o != contract.OutcomeOK {
		t.Errorf("built-in access point read = %s, want ok", o)
	}
	var via, src string
	if err := a.Store.DB().QueryRow(`SELECT via, via_source FROM sightings WHERE mac = '00:00:5e:10:00:13'`).Scan(&via, &src); err != nil ||
		via != "Home (5 GHz)" || src != "192.168.1.5" {
		t.Errorf("phone sighting via=%q via_source=%q err=%v", via, src, err)
	}

	save("wrong")
	for i := 0; i < 3; i++ {
		if _, err := a.Scanner.scan(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if ap.Logins() != 2 {
		t.Errorf("login attempts = %d, want 2 (one good, one rejected, then skipped)", ap.Logins())
	}
	if o := latestOutcome(t, a); o != contract.OutcomeSkippedAfterRejection {
		t.Errorf("after a rejection = %s, want skipped_after_rejection", o)
	}
}
