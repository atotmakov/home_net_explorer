package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// Feature 015 (contracts/collector-cli-changes.md): port checks after the presence scan, asked
// for after the presence upload; --ports forces every address found.

func portRun(t *testing.T, body []byte) *contract.CollectionRun {
	t.Helper()
	run, err := contract.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("upload is not a run: %v", err)
	}
	return run
}

func checkedIPs(run *contract.CollectionRun) []string {
	var out []string
	for _, c := range run.PortChecks {
		out = append(out, c.IP)
	}
	return out
}

func TestScanOncePorts(t *testing.T) {
	srv := newFakeServer(t)
	srv.plans = []*contract.PortScan{{Enabled: true, Due: []string{"192.168.1.20"}, Excluded: []string{}}}
	h := newHarness(t, srv.srv.URL)
	h.hosts = []string{"192.168.1.20", "192.168.1.21"}
	h.portsOpen = map[string][]int{"192.168.1.20": {22, 80}}
	if code := h.run("scan", "--once"); code != 0 {
		t.Fatalf("scan --once = %d\n%s", code, h.stderr.String())
	}
	if srv.uploads != 2 {
		t.Fatalf("uploads = %d, want the presence run then the port run", srv.uploads)
	}
	presence, pr := portRun(t, srv.bodies[0]), portRun(t, srv.bodies[1])
	if len(presence.Subnets) == 0 || len(presence.PortChecks) != 0 {
		t.Errorf("first upload: %d subnets, %d port checks; want a presence run", len(presence.Subnets), len(presence.PortChecks))
	}
	if len(pr.Subnets) != 0 || !slices.Equal(checkedIPs(pr), []string{"192.168.1.20"}) {
		t.Errorf("port run: %d subnets, checks %v; want only the due address", len(pr.Subnets), checkedIPs(pr))
	}
	if err := contract.Validate(pr); err != nil {
		t.Errorf("port run invalid: %v", err)
	}
	if o := pr.PortChecks[0].Open; len(o) != 2 || o[0].Port != 22 || o[1].Port != 80 {
		t.Errorf("open = %+v, want 22 and 80", o)
	}
	if !strings.Contains(h.stderr.String(), "port check finished") {
		t.Errorf("no port summary:\n%s", h.stderr.String())
	}
}

// The plan is asked for after the presence upload: a device the server learns from that upload
// is checked in the same run (FR-008, research R5).
func TestPortPlanAfterUpload(t *testing.T) {
	srv := newFakeServer(t)
	srv.plans = []*contract.PortScan{nil, {Enabled: true, Due: []string{"192.168.1.20"}, Excluded: []string{}}}
	h := newHarness(t, srv.srv.URL)
	h.hosts = []string{"192.168.1.20"}
	if code := h.run("scan", "--once"); code != 0 {
		t.Fatalf("scan --once = %d\n%s", code, h.stderr.String())
	}
	if srv.pings < 2 || srv.uploads != 2 || !slices.Equal(checkedIPs(portRun(t, srv.bodies[1])), []string{"192.168.1.20"}) {
		t.Errorf("pings=%d uploads=%d; want the second ping's due address checked", srv.pings, srv.uploads)
	}
}

func TestScanOnceForcePorts(t *testing.T) {
	srv := newFakeServer(t)
	srv.plans = []*contract.PortScan{{Enabled: true, Due: []string{}, Excluded: []string{"192.168.1.21"}}}
	h := newHarness(t, srv.srv.URL)
	h.hosts = []string{"192.168.1.20", "192.168.1.21"}
	if code := h.run("scan", "--once", "--ports"); code != 0 {
		t.Fatalf("scan --once --ports = %d\n%s", code, h.stderr.String())
	}
	if srv.uploads != 2 {
		t.Fatalf("uploads = %d, want 2", srv.uploads)
	}
	if got := checkedIPs(portRun(t, srv.bodies[1])); !slices.Equal(got, []string{"192.168.1.20", "192.168.1.100"}) {
		t.Errorf("forced checks = %v, want every address found except the excluded one", got)
	}
	if slices.Contains(h.checked, "192.168.1.21") {
		t.Error("an excluded address was checked")
	}

	// Nothing due and not forced: no port run.
	srv2 := newFakeServer(t)
	srv2.plans = []*contract.PortScan{{Enabled: true, Due: []string{}, Excluded: []string{}}}
	h2 := newHarness(t, srv2.srv.URL)
	if code := h2.run("scan", "--once"); code != 0 || srv2.uploads != 1 {
		t.Errorf("nothing due: code %d, uploads %d; want 0 and only the presence run", code, srv2.uploads)
	}
}

func TestDryRunPorts(t *testing.T) {
	srv := newFakeServer(t)
	h := newHarness(t, srv.srv.URL)
	h.hosts = []string{"192.168.1.20"}
	if code := h.run("scan", "--once", "--dry-run", "--ports"); code != 0 {
		t.Fatalf("dry run = %d\n%s", code, h.stderr.String())
	}
	dec := json.NewDecoder(bytes.NewReader(h.stdout.Bytes()))
	var runs []contract.CollectionRun
	for {
		var r contract.CollectionRun
		if err := dec.Decode(&r); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("stdout: %v\n%s", err, h.stdout.String())
		}
		runs = append(runs, r)
	}
	if len(runs) != 2 || len(runs[1].PortChecks) == 0 {
		t.Fatalf("dry run printed %d runs, want the presence run and a port run", len(runs))
	}
	if srv.uploads != 0 || h.spooled(t) != 0 {
		t.Error("dry run must not upload or spool")
	}
}

func TestCheckPrintsPortPlan(t *testing.T) {
	srv := newFakeServer(t)
	srv.plans = []*contract.PortScan{{Enabled: true, Due: []string{"192.168.1.20"}, Excluded: []string{}}}
	h := newHarness(t, srv.srv.URL)
	if code := h.run("check"); code != 0 {
		t.Fatalf("check = %d", code)
	}
	if !strings.Contains(h.stderr.String(), "Port checks: on (1 due)") {
		t.Errorf("check output lacks the port plan:\n%s", h.stderr.String())
	}
	if len(h.checked) != 0 {
		t.Error("check must never check ports")
	}
	srv.plans = []*contract.PortScan{{Enabled: false, Due: []string{}, Excluded: []string{}}}
	h.run("check")
	if !strings.Contains(h.stderr.String(), "Port checks: off") {
		t.Errorf("check output lacks 'off':\n%s", h.stderr.String())
	}
}

// run: after a presence run, a background port job uploads its run; --ports forces the first.
func TestRunLoopPorts(t *testing.T) {
	srv := newFakeServer(t)
	srv.plans = []*contract.PortScan{{Enabled: true, Due: []string{}, Excluded: []string{}}}
	h := newHarness(t, srv.srv.URL)
	h.hosts = []string{"192.168.1.20"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv.onUpload = func() {
		if srv.uploads == 2 {
			cancel()
		}
	}
	h.ctx = ctx
	if code := h.run("run", "--ports"); code != 0 {
		t.Fatalf("run = %d\n%s", code, h.stderr.String())
	}
	if srv.uploads < 2 || len(portRun(t, srv.bodies[1]).PortChecks) == 0 {
		t.Errorf("uploads = %d; want a port run after the first presence run", srv.uploads)
	}
}
