package accesspoint

import (
	"bytes"
	"context"
	"log/slog"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/collect/accesspoint/eaptest"
	"github.com/atotmakov/home_net_explorer/internal/collect/router"
	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// Feature 012: reading a TP-Link EAP225 in standalone mode (contracts/access-point-eap225.md).

const (
	testUser     = "admin"
	testPassword = "eap-pw-3Rt!marker"
	testAddr     = "192.168.0.5"
)

func source(t *testing.T, ap *eaptest.AP, password string) Source {
	t.Helper()
	src, err := New(Config{Model: ModelEAP225, Address: netip.MustParseAddr(testAddr),
		Prefix: netip.MustParsePrefix("192.168.0.0/24"), Username: testUser, Password: router.Secret(password)}, ap.Transport())
	if err != nil {
		t.Fatal(err)
	}
	return src
}

func clients() []eaptest.Entry {
	return []eaptest.Entry{
		eaptest.Client("00-00-5E-00-53-13", "192.168.0.13", "Home", 1, "Pixel-9a"),
		eaptest.Client("00-00-5E-00-53-31", "192.168.0.31", "Home", 0, ""),
		eaptest.Client("00-00-5E-00-53-40", "", "Guest", 1, ""), // no IP yet
	}
}

func TestReadBothFirmwares(t *testing.T) {
	want := []Client{
		{MAC: "00:00:5e:00:53:13", IP: netip.MustParseAddr("192.168.0.13"), SSID: "Home", Band: "5 GHz", Hostname: "Pixel-9a"},
		{MAC: "00:00:5e:00:53:31", IP: netip.MustParseAddr("192.168.0.31"), SSID: "Home", Band: "2.4 GHz"},
		{MAC: "00:00:5e:00:53:40", SSID: "Guest", Band: "5 GHz"},
	}
	for _, fw := range []struct {
		name string
		fw   eaptest.Firmware
		path string
	}{{"new", eaptest.New, "status.client.user"}, {"old", eaptest.Old, "monitor.client.client"}} {
		ap := eaptest.Start(t, fw.fw, testUser, testPassword)
		ap.SetClients(clients()...)
		res := source(t, ap, testPassword).Read(context.Background())
		if res.Outcome != contract.OutcomeOK {
			t.Fatalf("%s firmware: outcome %s", fw.name, res.Outcome)
		}
		if !reflect.DeepEqual(res.Clients, want) {
			t.Errorf("%s firmware: clients\n%+v\nwant\n%+v", fw.name, res.Clients, want)
		}
		if res.Path != fw.path {
			t.Errorf("%s firmware: path %q, want %q", fw.name, res.Path, fw.path)
		}
		if !reflect.DeepEqual(res.Keys, []string{"IP", "MAC", "Radio", "SSID", "hostname"}) {
			t.Errorf("%s firmware: keys %v", fw.name, res.Keys)
		}
		if ap.Logins() != 1 || ap.Logouts() != 1 {
			t.Errorf("%s firmware: %d logins, %d logouts; want one of each", fw.name, ap.Logins(), ap.Logouts())
		}
	}
}

// Key names and band values in other spellings (the real units are confirmed by gate G1).
func TestParseTolerant(t *testing.T) {
	body := []byte(`{"success":true,"timeout":false,"data":[
		{"mac":"00:00:5E:00:53:13","ip":"192.168.0.13","ssid":"Home","band":"5GHz","name":"tv"},
		{"MAC":"not-a-mac","IP":"192.168.0.14","SSID":"Home","Radio":"2.4GHz"},
		{"MAC":"00-00-5E-00-53-15","IP":"0.0.0.0","SSID":"Home","Radio":7}]}`)
	got, keys, err := parseClients(body)
	if err != nil {
		t.Fatal(err)
	}
	want := []Client{
		{MAC: "00:00:5e:00:53:13", IP: netip.MustParseAddr("192.168.0.13"), SSID: "Home", Band: "5 GHz", Hostname: "tv"},
		{IP: netip.MustParseAddr("192.168.0.14"), SSID: "Home", Band: "2.4 GHz"},
		{MAC: "00:00:5e:00:53:15", SSID: "Home"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("clients\n%+v\nwant\n%+v", got, want)
	}
	if !reflect.DeepEqual(keys, []string{"band", "ip", "mac", "name", "ssid"}) {
		t.Errorf("keys = %v", keys)
	}
	for label, b := range map[string]string{
		"not JSON":       `<html>login</html>`,
		"not logged in":  `{"success":true,"timeout":true}`,
		"no client list": `{"success":true,"timeout":false}`,
		"failure":        `{"success":false,"timeout":false,"data":[]}`,
	} {
		if _, _, err := parseClients([]byte(b)); err == nil {
			t.Errorf("%s: parsed", label)
		}
	}
	if c, _, err := parseClients([]byte(`{"success":true,"timeout":false,"data":[]}`)); err != nil || len(c) != 0 {
		t.Errorf("empty list = %v, %v", c, err)
	}
}

func TestVia(t *testing.T) {
	for c, want := range map[Client]string{
		{SSID: "Home", Band: "5 GHz"}:               "Home (5 GHz)",
		{SSID: "Home"}:                              "Home",
		{Band: "2.4 GHz"}:                           "Wi-Fi (2.4 GHz)",
		{SSID: strings.Repeat("n", 40), Band: "5 GHz"}: strings.Repeat("n", 24) + " (5 GHz)",
	} {
		if got := Via(c); got != want || len(got) > contract.MaxViaLen {
			t.Errorf("Via(%+v) = %q, want %q", c, got, want)
		}
	}
}

func TestReadOutcomes(t *testing.T) {
	ap := eaptest.Start(t, eaptest.New, testUser, testPassword)
	if res := source(t, ap, "wrong").Read(context.Background()); res.Outcome != contract.OutcomeLoginRejected {
		t.Errorf("wrong password = %s, want login_rejected", res.Outcome)
	}
	if n := ap.Logins(); n != 1 {
		t.Errorf("login attempts = %d, want exactly 1 (never retried)", n)
	}

	locked := eaptest.Start(t, eaptest.New, testUser, testPassword)
	locked.SetLocked(true)
	if res := source(t, locked, testPassword).Read(context.Background()); res.Outcome != contract.OutcomeLocked {
		t.Errorf("locked = %s, want locked", res.Outcome)
	}

	oldLocked := eaptest.Start(t, eaptest.Old, testUser, testPassword)
	if res := source(t, oldLocked, "wrong").Read(context.Background()); res.Outcome != contract.OutcomeLoginRejected {
		t.Errorf("old firmware, wrong password = %s, want login_rejected", res.Outcome)
	}

	managed := eaptest.Start(t, eaptest.New, testUser, testPassword)
	managed.SetManaged(true)
	if res := source(t, managed, testPassword).Read(context.Background()); res.Outcome != contract.OutcomeControllerManaged {
		t.Errorf("managed = %s, want controller_managed", res.Outcome)
	}
	if managed.Logins() != 0 {
		t.Error("logged in to a controller-managed access point")
	}

	garbage := eaptest.Start(t, eaptest.Old, testUser, testPassword)
	garbage.SetList([]byte(`<html>oops</html>`))
	if res := source(t, garbage, testPassword).Read(context.Background()); res.Outcome != contract.OutcomePageNotUnderstood {
		t.Errorf("garbage list = %s, want page_not_understood", res.Outcome)
	}
	if garbage.Logouts() != 1 {
		t.Error("no logout after a garbage list")
	}

	big := eaptest.Start(t, eaptest.New, testUser, testPassword)
	big.SetList(bytes.Repeat([]byte("x"), maxBody+1))
	if res := source(t, big, testPassword).Read(context.Background()); res.Outcome != contract.OutcomePageNotUnderstood {
		t.Errorf("oversized list = %s, want page_not_understood", res.Outcome)
	}

	slow := eaptest.Start(t, eaptest.New, testUser, testPassword)
	slow.SetDelay(300 * time.Millisecond)
	src := source(t, slow, testPassword).(*eap225)
	src.timeout = 100 * time.Millisecond
	if res := src.Read(context.Background()); res.Outcome != contract.OutcomeUnreachable {
		t.Errorf("slow access point = %s, want unreachable", res.Outcome)
	}

	gone := eaptest.Start(t, eaptest.New, testUser, testPassword)
	gone.Srv.Close()
	if res := source(t, gone, testPassword).Read(context.Background()); res.Outcome != contract.OutcomeUnreachable {
		t.Errorf("closed access point = %s, want unreachable", res.Outcome)
	}
}

// The credentials never show up in errors, logs or printed values (feature 002 FR-006).
func TestNoCredentialsInOutput(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cfg := Config{Model: ModelEAP225, Address: netip.MustParseAddr(testAddr), Prefix: netip.MustParsePrefix("192.168.0.0/24"),
		Username: testUser, Password: router.Secret(testPassword)}
	log.Info("config", "cfg", cfg)
	if _, err := New(Config{Model: "nope", Username: testUser, Password: router.Secret(testPassword)}, nil); err == nil ||
		strings.Contains(err.Error(), testPassword) {
		t.Errorf("bad config error = %v", err)
	}
	if _, err := New(Config{Model: ModelEAP225, Address: netip.MustParseAddr("8.8.8.8"), Username: testUser,
		Password: router.Secret(testPassword)}, nil); err == nil {
		t.Error("a public access point address was accepted")
	}
	if strings.Contains(logs.String(), testPassword) {
		t.Errorf("log shows the password: %s", logs.String())
	}
}
