package router

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/netip"

	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// Routers configured in the web UI (feature 003): the collector learns them from the server and
// fetches their login right before each read. The login lives only inside Read.

// LoginFunc fetches a router's current login (from the server's collector API, or from the
// store for the built-in collector).
type LoginFunc func(ctx context.Context) (username string, password Secret, err error)

// Rejections remembers logins a router rejected, so they are never tried again (FR-014).
// Implementations store hashes only.
type Rejections interface {
	Rejected(hash string) bool
	Reject(hash string)
}

// LoginHash identifies one login of one router: any change of the login (e.g. a new password
// saved in the UI) gives a new hash.
func LoginHash(model, address, subnet, username, password string) string {
	h := sha256.New()
	for _, f := range []string{model, address, subnet, username, password} {
		h.Write([]byte(f))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Remote is a router configured in the web UI.
type Remote struct {
	model  string
	addr   netip.Addr
	prefix netip.Prefix
	login  LoginFunc
	rej    Rejections
	rt     http.RoundTripper
}

// NewRemote returns a Source that fetches its login with login before each read. rej may be nil.
func NewRemote(model string, addr netip.Addr, prefix netip.Prefix, login LoginFunc, rej Rejections, rt http.RoundTripper) *Remote {
	return &Remote{model: model, addr: addr, prefix: prefix, login: login, rej: rej, rt: rt}
}

// Model implements Source.
func (r *Remote) Model() string { return r.model }

// Address implements Source.
func (r *Remote) Address() netip.Addr { return r.addr }

// Prefix implements Source.
func (r *Remote) Prefix() netip.Prefix { return r.prefix }

// Read fetches the login, skips the router if that exact login was rejected before, reads it and
// records a new rejection.
func (r *Remote) Read(ctx context.Context) Result {
	user, pass, err := r.login(ctx)
	if err != nil || user == "" || pass == "" {
		return Result{Outcome: contract.OutcomeLoginUnavailable}
	}
	hash := LoginHash(r.model, r.addr.String(), r.prefix.String(), user, pass.Reveal())
	if r.rej != nil && r.rej.Rejected(hash) {
		return Result{Outcome: contract.OutcomeSkippedAfterRejection}
	}
	src, err := New(Config{Model: r.model, URL: "http://" + r.addr.String(), Username: user, Password: pass,
		Subnet: r.prefix.String()}, r.rt)
	if err != nil {
		return Result{Outcome: contract.OutcomeUnreachable}
	}
	res := src.Read(ctx)
	if r.rej != nil && (res.Outcome == contract.OutcomeLoginRejected || res.Outcome == contract.OutcomeLocked) {
		r.rej.Reject(hash)
	}
	return res
}
