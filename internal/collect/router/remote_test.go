package router

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"testing"

	"github.com/atotmakov/home_net_explorer/internal/collect/router/routertest"
	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// Feature 003: a router configured in the web UI; its login comes from the server per read.

var (
	ispAddr   = netip.MustParseAddr("192.168.0.1")
	ispPrefix = netip.MustParsePrefix("192.168.0.0/24")
)

func TestRemoteReadsWithFetchedLogin(t *testing.T) {
	f := routertest.New(t, "root", testPassword)
	calls := 0
	login := func(ctx context.Context) (string, Secret, error) {
		calls++
		return "root", Secret(testPassword), nil
	}
	src := NewRemote(ModelHG8145V5, ispAddr, ispPrefix, login, nil, f.Transport())
	if src.Model() != ModelHG8145V5 || src.Address() != ispAddr || src.Prefix() != ispPrefix {
		t.Errorf("source = %s %s %s", src.Model(), src.Address(), src.Prefix())
	}
	res := src.Read(context.Background())
	if res.Outcome != contract.OutcomeOK || res.Online != 13 || res.Offline != 16 {
		t.Errorf("read = %s %d/%d", res.Outcome, res.Online, res.Offline)
	}
	if calls != 1 {
		t.Errorf("login fetched %d times, want once per read", calls)
	}
	if c := f.Credentials(); len(c) != 1 || c[0] != "root:"+testPassword {
		t.Errorf("router saw %v", c)
	}
	if f.SessionOpen() {
		t.Error("session left open")
	}

	var out bytes.Buffer
	fmt.Fprintf(&out, "%v %+v %#v", src, src, src)
	slog.New(slog.NewTextHandler(&out, nil)).Info("t", "src", src)
	if strings.Contains(out.String(), testPassword) {
		t.Error("a Remote source reveals the password")
	}
}

func TestRemoteLoginUnavailable(t *testing.T) {
	f := routertest.New(t, "root", testPassword)
	login := func(ctx context.Context) (string, Secret, error) { return "", "", errors.New("server unreachable") }
	res := NewRemote(ModelHG8145V5, ispAddr, ispPrefix, login, nil, f.Transport()).Read(context.Background())
	if res.Outcome != contract.OutcomeLoginUnavailable || len(res.Observations) != 0 {
		t.Errorf("read = %+v, want login_unavailable", res)
	}
	if n := len(f.Requests()); n != 0 {
		t.Errorf("the router was contacted %d times without a login", n)
	}
}

func TestConfigServerManaged(t *testing.T) {
	c := Config{Model: ModelHG8145V5, URL: "http://192.168.0.1"}
	if err := c.Validate(); err != nil || !c.ServerManaged() {
		t.Errorf("entry without credentials: %v, server-managed %v", err, c.ServerManaged())
	}
	c.Username = "root"
	if err := c.Validate(); err == nil {
		t.Error("a username without a password must be a config error")
	}
	c.Username, c.Password = "", "pw"
	if err := c.Validate(); err == nil {
		t.Error("a password without a username must be a config error")
	}
	if validConfig().ServerManaged() {
		t.Error("an entry with credentials is not server-managed")
	}
	if _, err := New(Config{Model: ModelHG8145V5, URL: "http://192.168.0.1"}, nil); err == nil {
		t.Error("New must refuse a server-managed entry (it has no login to read with)")
	}
}
