package collect

import (
	"context"
	"net/netip"
	"slices"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/clock"
	"github.com/atotmakov/home_net_explorer/internal/collect/ports"
	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// portWindow is how long a port job may run: it stops before the 1-hour run window so its run
// stays valid (feature 015, research R7).
const portWindow = contract.MaxRunWindow - 5*time.Minute

// PortTargets picks the addresses of a presence run to port-check (feature 015): those the
// collector reached itself (ARP, neighbor cache, ICMP/TCP; not router or access point lists),
// limited to the server's due list, or all of them when forced; never an excluded one.
func PortTargets(run *contract.CollectionRun, plan *contract.PortScan, force bool) []ports.Target {
	due, excluded := map[string]bool{}, map[string]bool{}
	if plan != nil {
		for _, ip := range plan.Due {
			due[ip] = true
		}
		for _, ip := range plan.Excluded {
			excluded[ip] = true
		}
	}
	var out []ports.Target
	for _, o := range run.Observations {
		switch o.Method {
		case contract.ObsARP, contract.ObsNeighborCache, contract.ObsICMP, contract.ObsTCP:
		default:
			continue
		}
		if excluded[o.IP] || (!force && !due[o.IP]) {
			continue
		}
		ip, err := netip.ParseAddr(o.IP)
		if err != nil {
			continue
		}
		out = append(out, ports.Target{IP: ip, MAC: o.MAC})
	}
	slices.SortFunc(out, func(a, b ports.Target) int { return a.IP.Compare(b.IP) })
	return out
}

// PortJob checks the ports of a list of addresses and returns them as a port run: a v1 run
// with no subnets or observations, only port_checks (research R7).
type PortJob struct {
	Checker         *ports.Checker
	Clock           clock.Clock
	Collector       contract.CollectorInfo
	IntervalSeconds int
}

// Run checks the targets and returns the port run. It stops itself before the run window;
// addresses not finished by then are reported as not complete.
func (j *PortJob) Run(ctx context.Context, v contract.Vantage, targets []ports.Target) *contract.CollectionRun {
	clk := j.Clock
	if clk == nil {
		clk = clock.Real{}
	}
	checker := ports.Checker{}
	if j.Checker != nil {
		checker = *j.Checker
	}
	if checker.Clock == nil {
		checker.Clock = clk
	}
	if v.Interfaces == nil {
		v.Interfaces = []contract.Interface{}
	}
	if v.Routes == nil {
		v.Routes = []contract.Route{}
	}
	run := &contract.CollectionRun{
		SchemaVersion:   contract.SchemaVersion,
		CollectionID:    NewUUID(),
		Collector:       j.Collector,
		StartedAt:       clk.Now(),
		IntervalSeconds: j.IntervalSeconds,
		Vantage:         v,
		Subnets:         []contract.SubnetScan{},
		Observations:    []contract.Observation{},
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-clk.After(portWindow):
			cancel()
		case <-ctx.Done():
		}
	}()
	run.PortChecks = checker.Check(ctx, targets)
	run.FinishedAt = clk.Now()
	run.SentAt = run.FinishedAt
	return run
}
