// Package accesspoint reads the client lists of Wi-Fi access points (feature 012): which devices
// are connected to an access point, on which Wi-Fi network and band. It mirrors package router.
package accesspoint

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"slices"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/collect/router"
	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// ModelEAP225 is the TP-Link EAP225 in standalone mode, both firmware generations
// (contracts/access-point-eap225.md).
const ModelEAP225 = "tplink-eap225"

// DefaultTimeout bounds one whole access point read (SC-003).
const DefaultTimeout = 10 * time.Second

// Models lists the supported access point models.
func Models() []string { return contract.AccessPointModels }

// Client is one entry of an access point's client list.
type Client struct {
	MAC      string     // lower-case aa:bb:cc:dd:ee:ff, "" when the entry has no valid MAC
	IP       netip.Addr // zero when the access point doesn't know it
	SSID     string
	Band     string // "2.4 GHz", "5 GHz" or ""
	Hostname string
}

// Result is the outcome of one read. Clients is empty unless Outcome is ok. Path names the
// endpoint that answered and Keys the key names of the first entry, for the check command
// (gate G1: confirmed on the owner's units without printing any value). Detail says, for
// page_not_understood, what the access point answered: key names and flags only, never a value.
type Result struct {
	Outcome string
	Clients []Client
	Path    string
	Keys    []string
	Detail  string
}

// Source reads the client list of one access point.
type Source interface {
	Model() string
	Address() netip.Addr
	Prefix() netip.Prefix
	Read(ctx context.Context) Result
}

// Config is one access point as the server lists it, with the login fetched for one read.
type Config struct {
	Model    string
	Address  netip.Addr
	Prefix   netip.Prefix // the subnet the access point is in (as the server resolved it)
	Username string
	Password router.Secret
}

// New validates cfg and returns its Source. rt carries the HTTP traffic (nil: a direct
// connection that ignores proxy settings, since the access point is on the LAN). Errors never
// contain the username or password.
func New(cfg Config, rt http.RoundTripper) (Source, error) {
	if !slices.Contains(Models(), cfg.Model) {
		return nil, fmt.Errorf("model %q is not supported (supported: %v)", cfg.Model, Models())
	}
	if !cfg.Address.Is4() || !contract.IsPrivateAddr(cfg.Address) {
		return nil, errors.New("the access point address must be a private IPv4 address")
	}
	if cfg.Username == "" || cfg.Password == "" {
		return nil, errors.New("username and password are required")
	}
	if rt == nil {
		rt = &http.Transport{Proxy: nil, MaxIdleConns: 2, IdleConnTimeout: 30 * time.Second}
	}
	return newEAP225(cfg, rt), nil
}

// Via is the label of a client's connection: "<SSID> (<band>)", cut so it fits the contract's
// 32 characters.
func Via(c Client) string {
	ssid := c.SSID
	if ssid == "" {
		ssid = "Wi-Fi"
	}
	if c.Band == "" {
		return cut(ssid, contract.MaxViaLen)
	}
	suffix := " (" + c.Band + ")"
	return cut(ssid, contract.MaxViaLen-len(suffix)) + suffix
}

// cut shortens s to at most n bytes without splitting a UTF-8 character.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n]
}
