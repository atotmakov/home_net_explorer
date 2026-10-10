package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/clock"
	"github.com/atotmakov/home_net_explorer/internal/collect"
	"github.com/atotmakov/home_net_explorer/internal/collect/collecttest"
	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/store"
	"github.com/atotmakov/home_net_explorer/internal/web"
)

// Feature 004, US2: pausing the built-in scanner is stored, survives a restart and stops both
// scheduled scans and "Scan now" until resumed (FR-020–FR-025).

const pauseInterval = 15 * time.Minute

func pauseApp(t *testing.T, dir string, clk *clock.Fake) *App {
	t.Helper()
	fnet := collecttest.NewFakeNetwork().Add("192.168.1.50", collecttest.Host{MAC: "da:a1:19:01:02:03"})
	engine := &collect.Engine{Prober: fnet, Presence: fnet, Neighbors: fnet, Resolver: fnet,
		Routes:    collecttest.NewFakeRoutes("192.168.1.100", "eth0=192.168.1.20/24"),
		Clock:     clk,
		Collector: contract.CollectorInfo{Name: "nas", Version: "test", OS: "linux"}}
	a, err := New(context.Background(), Options{DataDir: dir, Clock: clk, Engine: engine, ScanInterval: pauseInterval})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func builtinRuns(t *testing.T, a *App) int {
	t.Helper()
	var n int
	if err := a.Store.DB().QueryRow(`SELECT count(*) FROM collection_runs r JOIN collectors c ON c.id = r.collector_id
		WHERE c.name = 'nas'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func waitRuns(t *testing.T, a *App, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if builtinRuns(t, a) == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("built-in runs = %d, want %d", builtinRuns(t, a), want)
}

// settle gives the scan loop time to (not) act.
func settle() { time.Sleep(200 * time.Millisecond) }

func TestScannerPause(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	clk := clock.NewFake(time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC))
	ctx := context.Background()

	a := pauseApp(t, dir, clk)
	if err := a.Scanner.SetPaused(ctx, true); err != nil {
		t.Fatal(err)
	}
	v, ok, _ := a.Store.Setting(ctx, store.SettingBuiltinPaused)
	if !ok || v != store.FormatTime(clk.Now()) {
		t.Errorf("builtin_paused = %q (%v), want the pause time", v, ok)
	}
	runCtx, cancel := context.WithCancel(ctx)
	a.Start(runCtx)
	settle()
	if n := builtinRuns(t, a); n != 0 {
		t.Fatalf("paused at start-up, yet %d scans ran", n)
	}
	clk.Advance(5 * pauseInterval)
	settle()
	if n := builtinRuns(t, a); n != 0 {
		t.Errorf("paused, yet %d scans ran after 5 intervals", n)
	}
	if r := a.Scanner.Trigger(); r != web.TriggerPaused {
		t.Errorf("Trigger while paused = %v, want TriggerPaused", r)
	}
	settle()
	if n := builtinRuns(t, a); n != 0 {
		t.Error("Scan now started a scan while paused")
	}
	cancel()
	a.Close()

	// Restart on the same data directory: still paused, no start-up scan.
	b := pauseApp(t, dir, clk)
	defer b.Close()
	if p, at := b.Scanner.Paused(ctx); !p || !at.Equal(clk.Now().Add(-5*pauseInterval)) {
		t.Errorf("after restart Paused = %v at %v", p, at)
	}
	runCtx, cancel = context.WithCancel(ctx)
	defer cancel()
	b.Start(runCtx)
	settle()
	if n := builtinRuns(t, b); n != 0 {
		t.Fatalf("restarted while paused, yet %d scans ran", n)
	}

	// Resume: never scanned, so the first scan is due at once.
	if err := b.Scanner.SetPaused(ctx, false); err != nil {
		t.Fatal(err)
	}
	waitRuns(t, b, 1)
	if p, _ := b.Scanner.Paused(ctx); p {
		t.Error("still paused after resume")
	}
	if _, ok, _ := b.Store.Setting(ctx, store.SettingBuiltinPaused); ok {
		t.Error("builtin_paused is still set after resume")
	}

	// Pause after a scan, let several intervals pass, resume: the overdue scan runs at once.
	if err := b.Scanner.SetPaused(ctx, true); err != nil {
		t.Fatal(err)
	}
	settle()
	clk.Advance(3 * pauseInterval)
	settle()
	if n := builtinRuns(t, b); n != 1 {
		t.Errorf("paused, yet scans ran: %d", n)
	}
	if err := b.Scanner.SetPaused(ctx, false); err != nil {
		t.Fatal(err)
	}
	waitRuns(t, b, 2)

	// Running again: the next scan is due one interval after the last one.
	settle()
	clk.Advance(pauseInterval)
	waitRuns(t, b, 3)
	if r := b.Scanner.Trigger(); r == web.TriggerPaused {
		t.Error("Trigger after resume says paused")
	}
}
