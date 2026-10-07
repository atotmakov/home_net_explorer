package collect_test

import (
	"context"
	"net/netip"
	"testing"

	"github.com/atotmakov/home_net_explorer/internal/collect"
	"github.com/atotmakov/home_net_explorer/internal/collect/collecttest"
	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// A listed subnet that is not on-link is probed for presence (ICMP, then TCP): no MACs.
func TestRoutedSubnetUsesICMPTCP(t *testing.T) {
	net := collecttest.NewFakeNetwork().
		Add("192.168.50.1", collecttest.Host{}).
		Add("192.168.50.7", collecttest.Host{TCPOnly: true, Hostname: "camera.lan"})
	e := newEngine(net, collecttest.NewFakeRoutes("192.168.1.100", "eth0=192.168.1.20/24"))
	run, err := e.Scan(context.Background(), collect.ScanOptions{Targets: []netip.Prefix{netip.MustParsePrefix("192.168.50.0/24")}})
	if err != nil {
		t.Fatal(err)
	}
	s, ok := subnet(run, "192.168.50.0/24")
	if !ok || s.Method != contract.MethodICMPTCP || !s.Complete || s.HostsProbed != 254 {
		t.Fatalf("routed subnet scan = %+v", s)
	}
	got := map[string]contract.Observation{}
	for _, o := range run.Observations {
		got[o.IP] = o
	}
	if o := got["192.168.50.1"]; o.Method != contract.ObsICMP || o.MAC != "" {
		t.Errorf("icmp host = %+v", o)
	}
	if o := got["192.168.50.7"]; o.Method != contract.ObsTCP || o.MAC != "" || o.Hostname != "camera.lan" {
		t.Errorf("tcp-only host = %+v", o)
	}
	if err := contract.Validate(run); err != nil {
		t.Errorf("routed run fails validation: %v", err)
	}
}

func TestPresencePorts(t *testing.T) {
	want := []int{80, 443, 445, 22, 62078}
	if len(collect.PresencePorts) != len(want) {
		t.Fatalf("ports = %v, want %v", collect.PresencePorts, want)
	}
	for i := range want {
		if collect.PresencePorts[i] != want[i] {
			t.Fatalf("ports = %v, want %v", collect.PresencePorts, want)
		}
	}
}
