package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/atotmakov/home_net_explorer/internal/collect/router/routertest"
	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// Feature 002, US3: extra_subnets in hne-collector.json and HNE_EXTRA_SUBNETS (FR-015 – FR-017).

func (h *harness) withExtras(t *testing.T, serverURL string, extras []string, routers ...map[string]any) {
	t.Helper()
	c := map[string]any{"server_url": serverURL, "name": "desktop", "token": "tok", "subnets": []string{},
		"interval_seconds": 900, "extra_subnets": extras}
	if len(routers) > 0 {
		c["routers"] = routers
	}
	writeConfig(t, h.dir, c)
}

func dryRunSubnets(t *testing.T, h *harness) map[string]string {
	t.Helper()
	if code := h.run("scan", "--once", "--dry-run"); code != 0 {
		t.Fatalf("dry run = %d\n%s", code, h.stderr.String())
	}
	run, err := contract.Decode(bytes.NewReader(h.stdout.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	for _, s := range run.Subnets {
		m[s.CIDR] = s.Method
	}
	return m
}

func TestExtraSubnetsConfig(t *testing.T) {
	h := newHarness(t, "http://unused")
	h.withExtras(t, "http://unused", []string{"192.168.0.0/24"})
	if m := dryRunSubnets(t, h); m["192.168.0.0/24"] != contract.MethodICMPTCP || m["192.168.1.0/24"] != contract.MethodARP {
		t.Errorf("subnets = %v, want the on-link subnet plus the extra", m)
	}

	h.env["HNE_EXTRA_SUBNETS"] = "10.20.30.0/24, 10.20.31.0/24"
	m := dryRunSubnets(t, h)
	if m["10.20.30.0/24"] != contract.MethodICMPTCP || m["10.20.31.0/24"] != contract.MethodICMPTCP {
		t.Errorf("subnets = %v, want the extras from HNE_EXTRA_SUBNETS", m)
	}
	if _, ok := m["192.168.0.0/24"]; ok {
		t.Errorf("HNE_EXTRA_SUBNETS must replace the file's extra_subnets: %v", m)
	}
}

func TestExtraSubnetsConfigErrors(t *testing.T) {
	for _, bad := range []string{"8.8.8.0/24", "10.0.0.0/8", "192.168.0.0/31", "192.168.0.1/24", "nonsense"} {
		h := newHarness(t, "http://unused")
		h.withExtras(t, "http://unused", []string{bad})
		if code := h.run("check"); code != 2 {
			t.Errorf("extra %q: exit %d, want 2", bad, code)
		}
		if !strings.Contains(h.stderr.String(), "extra_subnets") {
			t.Errorf("extra %q: the error does not name extra_subnets:\n%s", bad, h.stderr.String())
		}
	}
}

func TestCheckListsExtraSubnets(t *testing.T) {
	srv := newFakeServer(t)
	h := newHarness(t, srv.srv.URL)
	h.withExtras(t, srv.srv.URL, []string{"192.168.50.0/24"})
	if code := h.run("check"); code != 0 {
		t.Fatalf("check = %d\n%s", code, h.stderr.String())
	}
	if !strings.Contains(h.stderr.String(), "192.168.50.0/24    routed: ICMP/TCP presence only, no MACs") {
		t.Errorf("check does not list the extra subnet:\n%s", h.stderr.String())
	}

	f := routertest.New(t, "root", routerPassword)
	h.routerRT = f.Transport()
	h.withExtras(t, srv.srv.URL, []string{"192.168.0.0/24"}, routerEntry())
	if code := h.run("check"); code != 0 {
		t.Fatalf("check = %d\n%s", code, h.stderr.String())
	}
	if !strings.Contains(h.stderr.String(), "192.168.0.0/24     router huawei-hg8145v5 at 192.168.0.1 (fallback: ICMP/TCP)") {
		t.Errorf("check does not show the router subnet's fallback:\n%s", h.stderr.String())
	}
}
