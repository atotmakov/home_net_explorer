package router

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
)

const testPassword = "pw-Zq8v!marker"

func validConfig() Config {
	return Config{Model: ModelHG8145V5, URL: "http://192.168.0.1", Username: "root", Password: Secret(testPassword)}
}

func TestRegistry(t *testing.T) {
	src, err := New(validConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if src.Model() != "huawei-hg8145v5" || src.Address() != netip.MustParseAddr("192.168.0.1") ||
		src.Prefix() != netip.MustParsePrefix("192.168.0.0/24") {
		t.Errorf("source = %s at %s for %s", src.Model(), src.Address(), src.Prefix())
	}
	cfg := validConfig()
	cfg.Model = "netgear-r7000"
	_, err = New(cfg, nil)
	if err == nil || !strings.Contains(err.Error(), "huawei-hg8145v5") {
		t.Errorf("unknown model error = %v, want it to name the supported models", err)
	}
	if got := Models(); len(got) != 1 || got[0] != "huawei-hg8145v5" {
		t.Errorf("Models() = %v", got)
	}
}

func TestConfigValidate(t *testing.T) {
	bad := map[string]func(c *Config){
		"public address":        func(c *Config) { c.URL = "http://8.8.8.8" },
		"hostname":              func(c *Config) { c.URL = "http://router.lan" },
		"loopback":              func(c *Config) { c.URL = "http://127.0.0.1:8080" },
		"no scheme":             func(c *Config) { c.URL = "192.168.0.1" },
		"ftp":                   func(c *Config) { c.URL = "ftp://192.168.0.1" },
		"path":                  func(c *Config) { c.URL = "http://192.168.0.1/admin" },
		"userinfo":              func(c *Config) { c.URL = "http://root:x@192.168.0.1" },
		"ipv6":                  func(c *Config) { c.URL = "http://[fd00::1]" },
		"missing username":      func(c *Config) { c.Username = "" },
		"missing password":      func(c *Config) { c.Password = "" },
		"missing model":         func(c *Config) { c.Model = "" },
		"public subnet":         func(c *Config) { c.Subnet = "8.8.8.0/24" },
		"subnet too wide":       func(c *Config) { c.Subnet = "10.0.0.0/8" },
		"subnet too narrow":     func(c *Config) { c.Subnet = "192.168.0.0/31" },
		"subnet not masked":     func(c *Config) { c.Subnet = "192.168.0.1/24" },
		"subnet not a CIDR":     func(c *Config) { c.Subnet = "192.168.0.0" },
		"username in URL error": func(c *Config) { c.URL = "http://" + testPassword + "@8.8.8.8" },
	}
	for label, f := range bad {
		c := validConfig()
		f(&c)
		err := c.Validate()
		if err == nil {
			t.Errorf("%s: accepted", label)
			continue
		}
		if strings.Contains(err.Error(), testPassword) || strings.Contains(err.Error(), "root") {
			t.Errorf("%s: error %q reveals a credential", label, err)
		}
	}
	good := map[string]struct {
		f      func(c *Config)
		prefix string
	}{
		"default /24":       {func(c *Config) {}, "192.168.0.0/24"},
		"other private /24": {func(c *Config) { c.URL = "http://10.20.30.254" }, "10.20.30.0/24"},
		"https and port":    {func(c *Config) { c.URL = "https://172.16.5.1:8443/" }, "172.16.5.0/24"},
		"explicit subnet":   {func(c *Config) { c.Subnet = "192.168.0.0/23" }, "192.168.0.0/23"},
	}
	for label, tc := range good {
		c := validConfig()
		tc.f(&c)
		if err := c.Validate(); err != nil {
			t.Errorf("%s: %v", label, err)
			continue
		}
		if got := c.Prefix(); got != netip.MustParsePrefix(tc.prefix) {
			t.Errorf("%s: Prefix() = %s, want %s", label, got, tc.prefix)
		}
	}
}

// FR-006 / SC-004: the password never shows up when a Secret or a Config is printed, logged or
// marshaled.
func TestSecretNeverPrinted(t *testing.T) {
	cfg := validConfig()
	var out []string
	for _, v := range []any{cfg.Password, cfg, &cfg} {
		out = append(out, fmt.Sprint(v), fmt.Sprintf("%v %+v %#v %s %q", v, v, v, v, v))
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, string(b))
	}
	var logs bytes.Buffer
	slog.New(slog.NewTextHandler(&logs, nil)).Info("t", "secret", cfg.Password, "config", cfg)
	slog.New(slog.NewJSONHandler(&logs, nil)).Info("t", "secret", cfg.Password, "config", cfg)
	out = append(out, logs.String())
	for _, s := range out {
		if strings.Contains(s, testPassword) {
			t.Errorf("password visible in %q", s)
		}
	}
	if cfg.Password.Reveal() != testPassword {
		t.Error("Reveal() must return the password")
	}
	var back Config
	if err := json.Unmarshal([]byte(`{"model":"huawei-hg8145v5","url":"http://192.168.0.1","username":"root","password":"`+testPassword+`"}`), &back); err != nil {
		t.Fatal(err)
	}
	if back.Password.Reveal() != testPassword {
		t.Error("a password read from hne-collector.json must be usable")
	}
}

func TestFilter(t *testing.T) {
	at := time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)
	p := netip.MustParsePrefix("192.168.0.0/24")
	devs := []Device{
		{IP: netip.MustParseAddr("192.168.0.9"), MAC: "00:00:5e:10:00:09", Online: true},
		{IP: netip.MustParseAddr("192.168.0.4"), MAC: "00:00:5e:10:00:04", Hostname: "host-01", Via: "LAN1", Online: true},
		{IP: netip.MustParseAddr("192.168.0.5"), MAC: "00:00:5e:10:00:05", Hostname: "gone"}, // offline: FR-007
		{IP: netip.MustParseAddr("0.0.0.0"), MAC: "00:00:5e:10:00:01"},                       // outside: FR-008
		{IP: netip.MustParseAddr("192.168.8.20"), MAC: "00:00:5e:10:00:20", Online: true},    // outside: FR-008
	}
	res := Filter(devs, p, at)
	if res.Outcome != contract.OutcomeOK || res.Online != 2 || res.Offline != 1 {
		t.Errorf("result = %s %d online / %d offline, want ok 2 / 1 (in-subnet only)", res.Outcome, res.Online, res.Offline)
	}
	if len(res.Observations) != 2 {
		t.Fatalf("observations = %+v", res.Observations)
	}
	o := res.Observations[0]
	if o.IP != "192.168.0.4" || o.MAC != "00:00:5e:10:00:04" || o.Hostname != "host-01" ||
		o.HostnameSource != contract.HostnameSourceRouter || o.Via != "LAN1" ||
		o.Method != contract.ObsRouterTable || !o.ObservedAt.Equal(at) {
		t.Errorf("first observation = %+v (sorted by IP)", o)
	}
	if o := res.Observations[1]; o.Hostname != "" || o.HostnameSource != "" || o.Via != "" {
		t.Errorf("observation without hostname/via = %+v", o)
	}
}

// The capture's counts inside 192.168.0.0/24: 13 online, 16 offline (the offline static
// 0.0.0.0 entry is outside the subnet and not counted).
func TestFilterFixtureCounts(t *testing.T) {
	devs, err := parseHG8145V5(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	res := Filter(devs, netip.MustParsePrefix("192.168.0.0/24"), time.Now())
	if res.Online != 13 || res.Offline != 16 || len(res.Observations) != 13 {
		t.Errorf("fixture = %d online / %d offline / %d observations, want 13 / 16 / 13", res.Online, res.Offline, len(res.Observations))
	}
}
