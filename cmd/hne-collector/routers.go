package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/atotmakov/home_net_explorer/internal/collect/router"
	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// rejectedFile lists, one SHA-256 per line, the router configs whose login was rejected
// (research R6). It holds hashes only, never credentials.
const rejectedFile = "hne-collector.router-rejected"

// routerHash identifies one router config; editing any field (e.g. the password) changes it.
func routerHash(rc router.Config) string {
	h := sha256.New()
	for _, f := range []string{rc.Model, rc.URL, rc.Username, rc.Password.Reveal(), rc.Subnet} {
		h.Write([]byte(f))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func loadRejected(dir string) map[string]bool {
	b, err := os.ReadFile(filepath.Join(dir, rejectedFile))
	if err != nil {
		return nil
	}
	set := map[string]bool{}
	for _, line := range strings.Fields(string(b)) {
		set[line] = true
	}
	return set
}

// recordRejections adds the routers whose login was rejected (or locked) in this run to the
// marker, so later scans don't try them again (FR-011).
// Routers set up in the web UI record their own rejections through markerRejections (feature 003).
func (c *collector) recordRejections(sources []contract.RunSource) {
	for _, s := range sources {
		if s.Outcome != contract.OutcomeLoginRejected && s.Outcome != contract.OutcomeLocked {
			continue
		}
		for _, rc := range c.cfg.Routers {
			if !rc.ServerManaged() && rc.Address().String() == s.Address && rc.Prefix().String() == s.Subnet {
				if c.addRejection(routerHash(rc)) {
					c.log.Warn("router login rejected; it is skipped until its config changes or you run check",
						"router", s.Model+" at "+s.Address, "outcome", s.Outcome)
				}
			}
		}
	}
}

// addRejection adds one hash to the marker file (write-temp + rename) and reports whether it was
// new. Router reads run concurrently, so writes are serialized.
func (c *collector) addRejection(hash string) bool {
	c.markerMu.Lock()
	defer c.markerMu.Unlock()
	set := loadRejected(c.env.dir)
	if set == nil {
		set = map[string]bool{}
	}
	if set[hash] {
		return false
	}
	set[hash] = true
	lines := make([]string, 0, len(set))
	for h := range set {
		lines = append(lines, h)
	}
	slices.Sort(lines)
	path := filepath.Join(c.env.dir, rejectedFile)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		c.log.Error("cannot write the router rejection marker", "path", path, "err", err)
		return false
	}
	if err := os.Rename(tmp, path); err != nil {
		c.log.Error("cannot write the router rejection marker", "path", path, "err", err)
		return false
	}
	return true
}

// markerRejections stores rejected logins of routers set up in the web UI in the marker file.
type markerRejections struct{ c *collector }

func (m markerRejections) Rejected(hash string) bool {
	m.c.markerMu.Lock()
	defer m.c.markerMu.Unlock()
	return loadRejected(m.c.env.dir)[hash]
}

func (m markerRejections) Reject(hash string) {
	if m.c.addRejection(hash) {
		m.c.log.Warn("router login rejected; it is skipped until its login changes in the web UI or you run check")
	}
}

// routerCacheFile keeps the last router list from the server (no credentials), for scans while
// the server is unreachable (feature 003, FR-007a).
const routerCacheFile = "hne-collector.routers"

func (c *collector) saveRouterCache(routers []contract.RouterRef) {
	if routers == nil {
		routers = []contract.RouterRef{}
	}
	b, err := json.Marshal(routers)
	if err != nil {
		return
	}
	path := filepath.Join(c.env.dir, routerCacheFile)
	if err := os.WriteFile(path+".tmp", b, 0o600); err == nil {
		os.Rename(path+".tmp", path)
	}
}

func (c *collector) cachedRouters() ([]contract.RouterRef, bool) {
	b, err := os.ReadFile(filepath.Join(c.env.dir, routerCacheFile))
	if err != nil {
		return nil, false
	}
	var rs []contract.RouterRef
	if json.Unmarshal(b, &rs) != nil {
		return nil, false
	}
	return rs, true
}

// buildRouters sets the engine's routers for the next read: the config file's routers with
// credentials, plus every router set up in the web UI whose address the file doesn't cover
// (FR-013). Without any server list (no ping, no cache), the file's entries without
// credentials stand in for it; their reads then report login_unavailable.
func (c *collector) buildRouters() {
	routers := append([]router.Source(nil), c.fileRouters...)
	covered := map[string]bool{}
	for _, rc := range c.cfg.Routers {
		if !rc.ServerManaged() {
			covered[rc.Address().String()] = true
		}
	}
	list := c.serverRouters
	if !c.haveRouterList {
		list = nil
		for _, rc := range c.cfg.Routers {
			if rc.ServerManaged() {
				list = append(list, contract.RouterRef{Model: rc.Model, Address: rc.Address().String(), Subnet: rc.Prefix().String()})
			}
		}
	}
	for _, rr := range list {
		addr, err := netip.ParseAddr(rr.Address)
		if err != nil || !contract.IsPrivateAddr(addr) || covered[rr.Address] {
			continue
		}
		prefix, err := contract.ParseSubnet(rr.Subnet)
		if err != nil {
			continue
		}
		covered[rr.Address] = true
		id := rr.ID
		login := func(ctx context.Context) (string, router.Secret, error) {
			if id == 0 {
				return "", "", errors.New("router not known to the server")
			}
			l, err := c.client.RouterLogin(ctx, id)
			if err != nil {
				return "", "", err
			}
			return l.Username, router.Secret(l.Password), nil
		}
		routers = append(routers, router.NewRemote(rr.Model, addr, prefix, login, markerRejections{c}, c.env.routerTransport))
	}
	c.engine.Routers = routers
}

// checkRouters reads each configured router once and prints the result (FR-012). Credentials
// are never printed.
func (c *collector) checkRouters(ctx context.Context, w io.Writer) {
	if len(c.engine.Routers) == 0 {
		return
	}
	fmt.Fprintln(w, "Routers:")
	var sources []contract.RunSource
	for _, src := range c.engine.Routers {
		rctx, cancel := context.WithTimeout(ctx, router.DefaultTimeout)
		res := src.Read(rctx)
		cancel()
		from := "config"
		if _, ok := src.(*router.Remote); ok {
			from = "server"
		}
		fmt.Fprintf(w, "  %s at %s (login from %s): %s\n", src.Model(), src.Address(), from, checkPhrase(res))
		if c.jsonOut {
			c.log.Info("router read", "model", src.Model(), "address", src.Address().String(), "subnet", src.Prefix().String(),
				"outcome", res.Outcome, "online", res.Online, "offline", res.Offline)
		}
		sources = append(sources, contract.RunSource{Type: contract.SourceTypeRouter, Model: src.Model(),
			Address: src.Address().String(), Subnet: src.Prefix().String(), Outcome: res.Outcome})
	}
	c.recordRejections(sources)
}

func checkPhrase(res router.Result) string {
	switch res.Outcome {
	case contract.OutcomeOK:
		return fmt.Sprintf("OK, %d online / %d offline devices listed", res.Online, res.Offline)
	case contract.OutcomeUnreachable:
		return "unreachable"
	case contract.OutcomeLoginRejected:
		return "login rejected (router skipped until its config changes or you run check)"
	case contract.OutcomeLocked:
		return "locked by the router"
	case contract.OutcomeSessionBusy:
		return "busy (someone is logged into the router)"
	case contract.OutcomePageNotUnderstood:
		return "page not understood (firmware?)"
	case contract.OutcomeSkippedAfterRejection:
		return "skipped after a rejected login"
	case contract.OutcomeLoginUnavailable:
		return "login unavailable from the server"
	}
	return res.Outcome
}
