package accesspoint

import (
	"context"
	"net/http"
	"net/netip"

	"github.com/atotmakov/home_net_explorer/internal/collect/router"
	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// Remote is an access point configured in the web UI (feature 012). Like router.Remote, it
// fetches its login right before each read; the login lives only inside Read.
type Remote struct {
	model  string
	addr   netip.Addr
	prefix netip.Prefix
	login  router.LoginFunc
	rej    router.Rejections
	rt     http.RoundTripper
}

// NewRemote returns a Source that fetches its login with login before each read. rej may be nil.
func NewRemote(model string, addr netip.Addr, prefix netip.Prefix, login router.LoginFunc, rej router.Rejections, rt http.RoundTripper) *Remote {
	return &Remote{model: model, addr: addr, prefix: prefix, login: login, rej: rej, rt: rt}
}

// Model implements Source.
func (r *Remote) Model() string { return r.model }

// Address implements Source.
func (r *Remote) Address() netip.Addr { return r.addr }

// Prefix implements Source.
func (r *Remote) Prefix() netip.Prefix { return r.prefix }

// Read fetches the login, skips the access point if that exact login was rejected before, reads
// it and records a new rejection (or lockout).
func (r *Remote) Read(ctx context.Context) Result {
	user, pass, err := r.login(ctx)
	if err != nil || user == "" || pass == "" {
		return Result{Outcome: contract.OutcomeLoginUnavailable}
	}
	hash := router.LoginHash(r.model, r.addr.String(), r.prefix.String(), user, pass.Reveal())
	if r.rej != nil && r.rej.Rejected(hash) {
		return Result{Outcome: contract.OutcomeSkippedAfterRejection}
	}
	src, err := New(Config{Model: r.model, Address: r.addr, Prefix: r.prefix, Username: user, Password: pass}, r.rt)
	if err != nil {
		return Result{Outcome: contract.OutcomeUnreachable}
	}
	res := src.Read(ctx)
	if r.rej != nil && (res.Outcome == contract.OutcomeLoginRejected || res.Outcome == contract.OutcomeLocked) {
		r.rej.Reject(hash)
	}
	return res
}
