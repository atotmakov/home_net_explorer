package ports

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"
)

// Model a camera that drops the first connection to its web port under load.
// Only silent ports are retried; the second pass uses fewer connections and more time.
func TestRetrySilentPorts(t *testing.T) {
	for _, cancelRetry := range []bool{false, true} {
		t.Run(map[bool]string{false: "recovered", true: "cancelled"}[cancelRetry], func(t *testing.T) {
			f := newFakeNet()
			tg := target(79)
			f.open[tg.IP] = map[int]string{80: ""}
			c := f.checker(128)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var mu sync.Mutex
			attempts := map[int]int{}
			active, peak := 0, 0
			c.Dial = func(ctx context.Context, addr netip.AddrPort, timeout time.Duration) (net.Conn, error) {
				p := int(addr.Port())
				mu.Lock()
				attempts[p]++
				n := attempts[p]
				if n > 1 {
					active++
					peak = max(peak, active)
				}
				mu.Unlock()
				if n > 1 {
					defer func() { mu.Lock(); active--; mu.Unlock() }()
					if timeout < 2*time.Second {
						t.Errorf("retry timeout = %v, want at least 2s", timeout)
					}
					if cancelRetry {
						cancel()
						return nil, ctx.Err()
					}
				}
				if p >= 80 && p <= 87 && n == 1 {
					return nil, timeoutErr{}
				}
				return f.dial(ctx, addr, timeout)
			}
			res := c.Check(ctx, []Target{tg})[0]
			if cancelRetry {
				if res.Complete {
					t.Error("cancelled retry reported complete")
				}
				return
			}
			if !res.Complete || len(res.Open) != 1 || res.Open[0].Port != 80 {
				t.Fatalf("retry did not recover web port: %+v", res)
			}
			for p := 1; p <= 128; p++ {
				want := 1
				if p >= 80 && p <= 87 {
					want = 2
				}
				if attempts[p] != want {
					t.Errorf("port %d: %d attempts, want %d", p, attempts[p], want)
				}
			}
			if peak > 4 {
				t.Errorf("retry concurrency = %d, want <= 4", peak)
			}
		})
	}
}

// A mostly filtered host must not get a second full sweep just because it has
// one reachable service. This keeps retries bounded for firewalled devices.
func TestMostlySilentDeviceNoRetry(t *testing.T) {
	f := newFakeNet()
	c := f.checker(128)
	var mu sync.Mutex
	dials := 0
	c.Dial = func(context.Context, netip.AddrPort, time.Duration) (net.Conn, error) {
		mu.Lock()
		defer mu.Unlock()
		dials++
		if dials == 1 {
			return nil, errors.New("connection refused")
		}
		return nil, timeoutErr{}
	}
	c.Check(context.Background(), []Target{target(9)})
	if dials != 128 {
		t.Errorf("%d dials, want 128", dials)
	}
}
