package collect

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/clock"
	"github.com/atotmakov/home_net_explorer/internal/collect/ports"
	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// Feature 015: after a presence scan, the collector checks the ports of the due addresses it
// reached directly (or, forced, of all of them) and uploads a separate port run (research R7).

func targetIPs(ts []ports.Target) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.IP.String())
	}
	return out
}

func TestPortJobTargets(t *testing.T) {
	run := &contract.CollectionRun{Observations: []contract.Observation{
		{IP: "192.168.1.20", MAC: "00:00:5e:00:53:20", Method: contract.ObsARP},
		{IP: "192.168.1.21", MAC: "00:00:5e:00:53:21", Method: contract.ObsNeighborCache},
		{IP: "192.168.1.30", Method: contract.ObsICMP},
		{IP: "192.168.0.4", MAC: "00:00:5e:00:53:04", Method: contract.ObsRouterTable},
		{IP: "192.168.0.7", MAC: "00:00:5e:00:53:07", Method: contract.ObsAPClient},
	}}
	plan := &contract.PortScan{Enabled: true, Due: []string{"192.168.1.20", "192.168.0.4", "192.168.1.99"},
		Excluded: []string{"192.168.1.21"}}
	cases := []struct {
		label string
		plan  *contract.PortScan
		force bool
		want  []string
	}{
		{"due and reached directly", plan, false, []string{"192.168.1.20"}},
		{"forced: every address reached directly, minus excluded", plan, true, []string{"192.168.1.20", "192.168.1.30"}},
		{"no plan (older server)", nil, false, nil},
		{"forced without a plan", nil, true, []string{"192.168.1.20", "192.168.1.21", "192.168.1.30"}},
	}
	for _, c := range cases {
		if got := targetIPs(PortTargets(run, c.plan, c.force)); !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v", c.label, got, c.want)
		}
	}
	if ts := PortTargets(run, plan, false); len(ts) != 1 || ts[0].MAC != "00:00:5e:00:53:20" {
		t.Errorf("target MAC = %+v, want the presence scan's", ts)
	}
}

type refusedErr struct{}

func (refusedErr) Error() string { return "connection refused" }

func fakeChecker(dial ports.DialFunc) *ports.Checker {
	return &ports.Checker{Dial: dial, MaxPort: 100, BannerTimeout: 10 * time.Millisecond,
		UDP: func(context.Context, netip.AddrPort, []byte, time.Duration) ([]byte, error) {
			return nil, errors.New("no answer")
		}}
}

func TestPortJobRun(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 10, 27, 3, 0, 0, 0, time.UTC))
	dial := func(ctx context.Context, a netip.AddrPort, _ time.Duration) (net.Conn, error) {
		if a.Addr().String() == "192.168.1.20" && a.Port() == 22 {
			c, s := net.Pipe()
			go func() { s.Read(make([]byte, 1)); s.Close() }()
			return c, nil
		}
		return nil, refusedErr{}
	}
	job := &PortJob{Checker: fakeChecker(dial), Clock: clk, IntervalSeconds: 900,
		Collector: contract.CollectorInfo{Name: "desktop", Version: "test", OS: "windows"}}
	v := contract.Vantage{Interfaces: []contract.Interface{{Name: "home", IP: "192.168.1.10", PrefixLen: 24}}}
	targets := []ports.Target{
		{IP: netip.MustParseAddr("192.168.1.20"), MAC: "00:00:5e:00:53:20"},
		{IP: netip.MustParseAddr("192.168.1.21")},
	}
	run := job.Run(context.Background(), v, targets)
	if err := contract.Validate(run); err != nil {
		t.Fatalf("port run invalid: %v", err)
	}
	if run.Subnets == nil || len(run.Subnets) != 0 || run.Observations == nil || len(run.Observations) != 0 {
		t.Errorf("subnets %v, observations %v: want both empty (a port run)", run.Subnets, run.Observations)
	}
	if len(run.PortChecks) != 2 || run.PortChecks[0].MAC != "00:00:5e:00:53:20" || !run.PortChecks[0].Complete {
		t.Fatalf("checks = %+v", run.PortChecks)
	}
	if o := run.PortChecks[0].Open; len(o) != 1 || o[0].Port != 22 || o[0].Service != "ssh" {
		t.Errorf("open = %+v, want 22/tcp ssh", o)
	}
	if run.PortChecks[1].Open == nil {
		t.Error("open must be an empty list, not null")
	}
	if again := job.Run(context.Background(), v, targets[:1]); again.CollectionID == run.CollectionID {
		t.Error("two port runs share a collection id")
	}
}

// A job stops itself before the 1-hour run window; unfinished addresses are not complete.
func TestPortJobWindow(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 10, 27, 3, 0, 0, 0, time.UTC))
	dial := func(ctx context.Context, _ netip.AddrPort, _ time.Duration) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	job := &PortJob{Checker: fakeChecker(dial), Clock: clk,
		Collector: contract.CollectorInfo{Name: "desktop", Version: "test", OS: "windows"}}
	done := make(chan *contract.CollectionRun)
	go func() {
		done <- job.Run(context.Background(), contract.Vantage{}, []ports.Target{{IP: netip.MustParseAddr("192.168.1.20")}})
	}()
	for clk.Waiters() == 0 {
		time.Sleep(time.Millisecond)
	}
	clk.Advance(56 * time.Minute)
	select {
	case run := <-done:
		if run.PortChecks[0].Complete {
			t.Error("a check cut by the run window is complete")
		}
		if err := contract.Validate(run); err != nil {
			t.Errorf("port run invalid: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the job did not stop at the run window")
	}
}
