package app

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/clock"
	"github.com/atotmakov/home_net_explorer/internal/collect"
	"github.com/atotmakov/home_net_explorer/internal/collect/collecttest"
	"github.com/atotmakov/home_net_explorer/internal/collect/ports"
	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/web"
)

// Feature 015: the built-in collector checks the ports of due devices after each scan, in the
// background (research R7, R8).

type fakePorts struct {
	mu      sync.Mutex
	gate    chan struct{} // when set, port 1 of every device waits for it to close
	checked map[string]int
}

func (f *fakePorts) checker() *ports.Checker {
	return &ports.Checker{MaxPort: 100, BannerTimeout: 5 * time.Millisecond,
		Dial: func(ctx context.Context, a netip.AddrPort, _ time.Duration) (net.Conn, error) {
			if a.Port() == 1 {
				f.mu.Lock()
				f.checked[a.Addr().String()]++
				gate := f.gate
				f.mu.Unlock()
				if gate != nil {
					select {
					case <-gate:
					case <-ctx.Done():
					}
				}
			}
			if a.Addr().String() == "192.168.1.50" && a.Port() == 80 {
				c, s := net.Pipe()
				go func() { s.Read(make([]byte, 1)); s.Close() }()
				return c, nil
			}
			return nil, errors.New("connection refused")
		},
		UDP: func(context.Context, netip.AddrPort, []byte, time.Duration) ([]byte, error) {
			return nil, errors.New("no answer")
		}}
}

func portsApp(t *testing.T, clk *clock.Fake, f *fakePorts) *App {
	t.Helper()
	fnet := collecttest.NewFakeNetwork().
		Add("192.168.1.50", collecttest.Host{MAC: "00:00:5e:00:53:50"}).
		Add("192.168.1.51", collecttest.Host{MAC: "00:00:5e:00:53:51"})
	engine := &collect.Engine{Prober: fnet, Presence: fnet, Neighbors: fnet, Resolver: fnet,
		Routes:    collecttest.NewFakeRoutes("192.168.1.100", "eth0=192.168.1.20/24"),
		Clock:     clk,
		Collector: contract.CollectorInfo{Name: "nas", Version: "test", OS: "linux"}}
	a, err := New(context.Background(), Options{DataDir: filepath.Join(t.TempDir(), "data"), Clock: clk, Engine: engine,
		ScanInterval: pauseInterval, PortChecker: f.checker()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

func portChecks(t *testing.T, a *App) int {
	t.Helper()
	var n int
	if err := a.Store.DB().QueryRow(`SELECT count(*) FROM port_checks`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// New devices are checked in the cycle that found them; the port run is ingested.
func TestBuiltinPortJob(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 10, 27, 3, 0, 0, 0, time.UTC))
	f := &fakePorts{checked: map[string]int{}}
	a := portsApp(t, clk, f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.Start(ctx)
	waitRuns(t, a, 2) // the presence run, then the port run
	if n := portChecks(t, a); n != 2 {
		t.Errorf("port checks = %d, want both new devices", n)
	}
	var open int
	a.Store.DB().QueryRow(`SELECT count(*) FROM device_ports WHERE port = 80 AND state = 'open'`).Scan(&open)
	if open != 1 {
		t.Errorf("open 80/tcp rows = %d, want 1", open)
	}
	waitFor(t, "the port job to end", func() bool { return !a.Scanner.Status().PortsRunning })
	settle() // the scan loop waits for its next turn

	// Checked a moment ago: the next scheduled scan checks nothing; "Check ports now" forces all.
	clk.Advance(pauseInterval)
	waitRuns(t, a, 3)
	settle()
	if n := builtinRuns(t, a); n != 3 {
		t.Errorf("runs = %d after a scan with nothing due, want 3", n)
	}
	if r := a.Scanner.TriggerPorts(); r != web.TriggerStarted {
		t.Fatalf("TriggerPorts = %v, want started", r)
	}
	waitRuns(t, a, 5)
	if n := portChecks(t, a); n != 4 {
		t.Errorf("port checks = %d after the forced check, want 4", n)
	}
}

// The port job runs beside the scan loop: a slow job never delays the next scheduled scan, and
// no second job starts while one runs.
func TestBuiltinPortJobInBackground(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 10, 27, 3, 0, 0, 0, time.UTC))
	f := &fakePorts{checked: map[string]int{}, gate: make(chan struct{})}
	a := portsApp(t, clk, f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.Start(ctx)
	waitRuns(t, a, 1)
	waitFor(t, "the port job to start", func() bool { return a.Scanner.Status().PortsRunning })
	if st := a.Scanner.Status(); st.PortsTotal != 2 {
		t.Errorf("status = %+v, want 2 devices being checked", st)
	}
	if r := a.Scanner.TriggerPorts(); r != web.TriggerRunning {
		t.Errorf("TriggerPorts while a job runs = %v, want running", r)
	}

	waitFor(t, "the scan loop to wait", func() bool { return clk.Waiters() >= 2 })
	clk.Advance(pauseInterval)
	waitRuns(t, a, 2) // the next presence run, while the first job is still blocked
	settle()
	f.mu.Lock()
	for ip, n := range f.checked {
		if n != 1 {
			t.Errorf("%s checked %d times, want once (no second job while one runs)", ip, n)
		}
	}
	f.mu.Unlock()

	close(f.gate)
	waitRuns(t, a, 3)
	if n := portChecks(t, a); n != 2 {
		t.Errorf("port checks = %d, want 2", n)
	}
}
