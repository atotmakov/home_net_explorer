//go:build hwtest && windows

package collect

import (
	"context"
	"encoding/json"
	"net/netip"
	"os/exec"
	"testing"
)

// Runs only on a real Windows machine, without elevation: go test -tags hwtest ./internal/collect/ (T069).
func TestWindowsVantageAndSendARP(t *testing.T) {
	v, err := WindowsRoutes{}.Vantage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Compare the interfaces with what Windows itself reports.
	out, err := exec.Command("powershell", "-NoProfile", "-Command",
		"Get-NetIPAddress -AddressFamily IPv4 | Where-Object { $_.IPAddress -ne '127.0.0.1' } | Select-Object -ExpandProperty IPAddress | ConvertTo-Json").Output()
	if err != nil {
		t.Fatalf("Get-NetIPAddress: %v", err)
	}
	var want []string
	if err := json.Unmarshal(out, &want); err != nil {
		var one string
		if json.Unmarshal(out, &one) != nil {
			t.Fatalf("parse %s: %v", out, err)
		}
		want = []string{one}
	}
	got := map[string]bool{}
	for _, ifc := range v.Interfaces {
		got[ifc.IP] = true
	}
	for _, ip := range want {
		if a, _ := netip.ParseAddr(ip); a.IsLinkLocalUnicast() {
			continue
		}
		if !got[ip] {
			t.Errorf("interface address %s reported by Windows is missing from the vantage report %+v", ip, v.Interfaces)
		}
	}

	for _, rt := range v.Routes {
		p, _ := netip.ParsePrefix(rt.Destination)
		if p.Bits() != 0 || rt.NextHop == "" {
			continue
		}
		gw := netip.MustParseAddr(rt.NextHop)
		mac, ok, err := SendARPProber{}.Probe(context.Background(), rt.Interface, gw)
		if err != nil || !ok {
			t.Fatalf("SendARP to the default gateway %s: ok=%v err=%v", gw, ok, err)
		}
		t.Logf("gateway %s is at %s", gw, mac)
		return
	}
	t.Skip("no default route")
}
