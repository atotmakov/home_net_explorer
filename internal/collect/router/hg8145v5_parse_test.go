package router

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strings"
	"testing"

	"github.com/atotmakov/home_net_explorer/internal/collect/router/routertest"
	"github.com/atotmakov/home_net_explorer/internal/contract"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(routertest.FixturePath())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// esc writes s the way the router does: punctuation as \xNN escapes.
func esc(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == ':' || r == '.' || r == '-' {
			fmt.Fprintf(&b, `\x%02x`, r)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// dev is one USERDevice record (16 arguments).
func dev(ip, mac, port, status, host string) string {
	return fmt.Sprintf(`new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.1","%s","%s","%s","DHCP","vendor","%s","ETH","0\x3a5","%s","1","0","0","","","0"),`,
		esc(ip), esc(mac), port, status, esc(host))
}

// devNew is one USERDeviceNew record (17 arguments, RealMacAddr after MacAddr).
func devNew(ip, mac, realMAC, port, status, host string) string {
	return fmt.Sprintf(`new USERDeviceNew("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.1","%s","%s","%s","%s","DHCP","vendor","%s","WIFI","0\x3a5","%s","1","0","0","","","0"),`,
		esc(ip), esc(mac), esc(realMAC), port, status, esc(host))
}

// page mirrors the array selection of GetLanUserDevInfo.asp.
func page(productType, isRealmac, realmacArray, plainArray, product2Array string) []byte {
	return []byte(fmt.Sprintf("﻿function USERDevice(){}\r\nvar ProductType = '%s';\r\nvar isRealmac = '%s';\r\n"+
		"if (ProductType != '2') {\r\n    if (isRealmac == 1) {\r\n        var UserDevinfo = new Array(%snull);\r\n"+
		"    } else {\r\n        var UserDevinfo = new Array(%snull);\r\n    }\r\n} else {\r\n"+
		"    var UserDevinfo = new Array(%snull);\r\n}\r\n",
		productType, isRealmac, realmacArray, plainArray, product2Array))
}

func byIP(t *testing.T, devs []Device, ip string) Device {
	t.Helper()
	for _, d := range devs {
		if d.IP == netip.MustParseAddr(ip) {
			return d
		}
	}
	t.Fatalf("no device %s in %d devices", ip, len(devs))
	return Device{}
}

func TestParseFixture(t *testing.T) {
	devs, err := parseHG8145V5(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 30 {
		t.Fatalf("devices = %d, want 30", len(devs))
	}
	online := 0
	for _, d := range devs {
		if d.Online {
			online++
		}
		if !contract.ValidMAC(d.MAC) {
			t.Errorf("%s: MAC %q is not lower-case aa:bb:cc:dd:ee:ff", d.IP, d.MAC)
		}
	}
	if online != 13 {
		t.Errorf("online = %d, want 13", online)
	}
	d := byIP(t, devs, "192.168.0.4") // escaped as 192\x2e168\x2e0\x2e4 in the capture
	if d.MAC != "00:00:5e:10:00:02" || d.Hostname != "host-01" || d.Via != "LAN1" || !d.Online {
		t.Errorf("192.168.0.4 = %+v", d)
	}
	if z := byIP(t, devs, "0.0.0.0"); z.Online {
		t.Errorf("the static 0.0.0.0 entry is offline in the capture: %+v", z)
	}
}

func TestParseRealMAC(t *testing.T) {
	b := page("1", "1",
		devNew("192.168.0.5", "00:00:5e:00:00:aa", "00:00:5e:00:00:bb", "SSID2", "Online", "tv"),
		dev("192.168.0.6", "00:00:5e:00:00:cc", "LAN1", "Online", "wrong-array"),
		dev("192.168.0.7", "00:00:5e:00:00:dd", "LAN2", "Online", "wrong-array"))
	devs, err := parseHG8145V5(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 1 {
		t.Fatalf("devices = %+v, want only the USERDeviceNew array", devs)
	}
	if d := devs[0]; d.MAC != "00:00:5e:00:00:bb" || d.Via != "SSID2" || d.Hostname != "tv" || d.IP != netip.MustParseAddr("192.168.0.5") {
		t.Errorf("isRealmac = 1 device = %+v (RealMacAddr must win)", d)
	}
}

func TestParseArraySelection(t *testing.T) {
	a := devNew("192.168.0.5", "00:00:5e:00:00:aa", "00:00:5e:00:00:aa", "LAN1", "Online", "a")
	b := dev("192.168.0.6", "00:00:5e:00:00:bb", "LAN1", "Online", "b")
	c := dev("192.168.0.7", "00:00:5e:00:00:cc", "LAN1", "Online", "c")
	for _, tc := range []struct{ product, realmac, want string }{
		{"1", "0", "b"}, {"1", "1", "a"}, {"2", "0", "c"}, {"2", "1", "c"},
	} {
		devs, err := parseHG8145V5(page(tc.product, tc.realmac, a, b, c))
		if err != nil || len(devs) != 1 || devs[0].Hostname != tc.want {
			t.Errorf("ProductType %s, isRealmac %s: %+v, %v; want array %q", tc.product, tc.realmac, devs, err, tc.want)
		}
	}
}

func TestParseFieldMapping(t *testing.T) {
	recs := dev("192.168.0.10", "00:00:5E:AA:BB:CC", "LAN0", "Online", "--") +
		dev("192.168.0.11", "00:00:5e:00:00:11", "SSID0", "Offline", "") +
		dev("192.168.0.12", "00:00:5e:00:00:12", "--", "Online", "phone") +
		dev("192.168.0.13", "00:00:5e:00:00:13", "", "ONLINE", "x") +
		dev("192.168.0.14", "00:00:5e:00:00:14", "SSID3", "Online", "y") +
		dev("--", "00:00:5e:00:00:15", "LAN1", "Online", "no-ipv4") +
		dev("fe80::1", "00:00:5e:00:00:16", "LAN1", "Online", "ipv6") +
		dev("192.168.0.17", "not-a-mac", "LAN1", "Online", "bad-mac")
	devs, err := parseHG8145V5(page("1", "0", "", recs, ""))
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 5 {
		t.Fatalf("devices = %d, want 5 (entries without an IPv4 address or a valid MAC are skipped): %+v", len(devs), devs)
	}
	if d := byIP(t, devs, "192.168.0.10"); d.MAC != "00:00:5e:aa:bb:cc" || d.Via != "" || d.Hostname != "" {
		t.Errorf("LAN0 / -- host / upper-case MAC = %+v", d)
	}
	if d := byIP(t, devs, "192.168.0.11"); d.Online || d.Via != "" {
		t.Errorf("SSID0 offline = %+v", d)
	}
	if d := byIP(t, devs, "192.168.0.12"); d.Via != "" || d.Hostname != "phone" {
		t.Errorf("port -- = %+v", d)
	}
	if d := byIP(t, devs, "192.168.0.13"); !d.Online || d.Via != "" {
		t.Errorf("status ONLINE, empty port = %+v", d)
	}
	if d := byIP(t, devs, "192.168.0.14"); d.Via != "SSID3" {
		t.Errorf("SSID3 = %+v", d)
	}
}

func TestParseEmptyList(t *testing.T) {
	devs, err := parseHG8145V5(page("1", "0", "", "", ""))
	if err != nil || len(devs) != 0 {
		t.Errorf("empty list = %+v, %v; want no devices and no error", devs, err)
	}
}

func TestParseGarbled(t *testing.T) {
	full := string(fixture(t))
	short := strings.Replace(dev("192.168.0.10", "00:00:5e:00:00:10", "LAN1", "Online", "x"), `,"0"),`, `),`, 1)
	cases := map[string][]byte{
		"login page":            []byte(`<html><script>var FailStat ='0';</script><form id="loginform"></form></html>`),
		"empty body":            nil,
		"truncated array":       []byte(full[:len(full)*2/3]),
		"wrong argument count":  page("1", "0", "", short, ""),
		"missing ProductType":   []byte(strings.Replace(full, "var ProductType", "var ProductKind", 1)),
		"missing arrays":        []byte(strings.ReplaceAll(full, "var UserDevinfo = new Array(", "var Other = new Array(")),
		"wrong constructor":     []byte(strings.Replace(string(page("1", "0", "", dev("192.168.0.10", "00:00:5e:00:00:10", "LAN1", "Online", "x"), "")), "new USERDevice(", "new OTHERDevice(", 1)),
		"unterminated argument": page("1", "0", "", `new USERDevice("InternetGatewayDevice`, ""),
	}
	for label, b := range cases {
		if _, err := parseHG8145V5(b); !errors.Is(err, ErrPageNotUnderstood) {
			t.Errorf("%s: err = %v, want ErrPageNotUnderstood", label, err)
		}
	}
}
