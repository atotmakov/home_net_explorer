package router

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// ModelHG8145V5 is the Huawei HG8145V5 GPON ONT (contracts/router-hg8145v5.md).
const ModelHG8145V5 = "huawei-hg8145v5"

// DefaultTimeout bounds one whole router read (plan.md: never delay a scan by more than this).
const DefaultTimeout = 10 * time.Second

// ErrPageNotUnderstood means the router answered with a page this collector cannot parse
// (e.g. after a firmware change).
var ErrPageNotUnderstood = errors.New("router page not understood")

// Secret is a credential. It prints, logs and marshals as "***"; only Reveal returns the value.
type Secret string

const masked = "***"

// String implements fmt.Stringer.
func (Secret) String() string { return masked }

// GoString implements fmt.GoStringer (%#v).
func (Secret) GoString() string { return masked }

// MarshalJSON keeps the secret out of any JSON output (it is read from the config as a plain
// string).
func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + masked + `"`), nil }

// LogValue implements slog.LogValuer.
func (Secret) LogValue() slog.Value { return slog.StringValue(masked) }

// Reveal returns the credential, for the login request and the rejection-marker hash only.
func (s Secret) Reveal() string { return string(s) }

// Config is one router entry of hne-collector.json (data-model.md "RouterSource").
type Config struct {
	Model    string `json:"model"`
	URL      string `json:"url"`
	Username string `json:"username"`
	Password Secret `json:"password"`
	Subnet   string `json:"subnet,omitempty"` // default: the router address with /24 (research R5)
}

// Validate checks the entry. Errors never contain the username or the password (FR-006).
func (c Config) Validate() error {
	if !slices.Contains(Models(), c.Model) {
		return fmt.Errorf("model %q is not supported (supported: %v)", c.Model, Models())
	}
	if _, err := c.address(); err != nil {
		return err
	}
	if (c.Username == "") != (c.Password == "") {
		return errors.New("username and password go together (leave both out for a router set up in the web UI)")
	}
	if c.Subnet != "" {
		if _, err := contract.ParseSubnet(c.Subnet); err != nil {
			return fmt.Errorf("subnet: %w", err)
		}
	}
	return nil
}

// address parses URL: http(s)://<private IPv4>[:port] with no path, query or user info (FR-005).
func (c Config) address() (netip.Addr, error) {
	const want = "url must be http(s)://<private IPv4 address>[:port]"
	u, err := url.Parse(c.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return netip.Addr{}, errors.New(want)
	}
	a, err := netip.ParseAddr(u.Hostname())
	if err != nil || !a.Is4() || !contract.IsPrivateAddr(a) {
		return netip.Addr{}, errors.New(want)
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return netip.Addr{}, errors.New(want)
		}
	}
	return a, nil
}

// ServerManaged reports an entry without username and password: a router configured in the web
// UI, listed for information; its login comes from the server (feature 003).
func (c Config) ServerManaged() bool { return c.Username == "" && c.Password == "" }

// Address is the router's IPv4 address (the zero Addr if the config is invalid).
func (c Config) Address() netip.Addr {
	a, _ := c.address()
	return a
}

// Prefix is the subnet the router serves: Subnet, or the router address with /24.
func (c Config) Prefix() netip.Prefix {
	if c.Subnet != "" {
		if p, err := contract.ParseSubnet(c.Subnet); err == nil {
			return p
		}
	}
	return netip.PrefixFrom(c.Address(), 24).Masked()
}

// Device is one entry of a router's device list.
type Device struct {
	IP       netip.Addr
	MAC      string // lower-case aa:bb:cc:dd:ee:ff
	Hostname string // "" when the router has none
	Via      string // LAN port or Wi-Fi interface, "" when unknown
	Online   bool
}

// Result is the outcome of one router read. Online/Offline count the router's entries inside
// its subnet; Observations are the online ones (FR-007, FR-008). Both are empty unless
// Outcome is ok.
type Result struct {
	Outcome      string
	Online       int
	Offline      int
	Observations []contract.Observation
}

// Source reads the device list of one configured router.
type Source interface {
	Model() string
	Address() netip.Addr
	Prefix() netip.Prefix
	Read(ctx context.Context) Result
}

// Models lists the supported router models.
func Models() []string { return []string{ModelHG8145V5} }

// New validates cfg and returns its Source. rt carries the router's HTTP traffic (nil: a
// direct connection that ignores proxy settings, since the router is on the LAN).
func New(cfg Config, rt http.RoundTripper) (Source, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.ServerManaged() {
		return nil, errors.New("no username and password: this router's login comes from the server")
	}
	if rt == nil {
		rt = &http.Transport{Proxy: nil, MaxIdleConns: 2, IdleConnTimeout: 30 * time.Second}
	}
	switch cfg.Model {
	case ModelHG8145V5:
		return newHG8145V5(cfg, rt), nil
	}
	return nil, fmt.Errorf("model %q is not supported", cfg.Model)
}

// Filter turns a device list into a Result: entries outside p are dropped (FR-008), only
// online entries become router_table observations (FR-007), stamped at.
func Filter(devs []Device, p netip.Prefix, at time.Time) Result {
	res := Result{Outcome: contract.OutcomeOK, Observations: []contract.Observation{}}
	for _, d := range devs {
		if !p.Contains(d.IP) {
			continue
		}
		if !d.Online {
			res.Offline++
			continue
		}
		res.Online++
		o := contract.Observation{ObservedAt: at, IP: d.IP.String(), MAC: d.MAC, Via: d.Via, Method: contract.ObsRouterTable}
		if d.Hostname != "" {
			o.Hostname, o.HostnameSource = d.Hostname, contract.HostnameSourceRouter
		}
		res.Observations = append(res.Observations, o)
	}
	slices.SortFunc(res.Observations, func(a, b contract.Observation) int {
		return netip.MustParseAddr(a.IP).Compare(netip.MustParseAddr(b.IP))
	})
	return res
}

// Skipped wraps src so that Read reports skipped_after_rejection without contacting the router
// (FR-011: no login attempts after a rejected login).
func Skipped(src Source) Source { return skipped{src} }

type skipped struct{ Source }

func (skipped) Read(context.Context) Result {
	return Result{Outcome: contract.OutcomeSkippedAfterRejection}
}
