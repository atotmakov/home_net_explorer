package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
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
func (c *collector) recordRejections(sources []contract.RunSource) {
	set := loadRejected(c.env.dir)
	if set == nil {
		set = map[string]bool{}
	}
	added := false
	for _, s := range sources {
		if s.Outcome != contract.OutcomeLoginRejected && s.Outcome != contract.OutcomeLocked {
			continue
		}
		for _, rc := range c.cfg.Routers {
			if rc.Address().String() == s.Address && rc.Prefix().String() == s.Subnet && !set[routerHash(rc)] {
				set[routerHash(rc)] = true
				added = true
				c.log.Warn("router login rejected; it is skipped until its config changes or you run check",
					"router", s.Model+" at "+s.Address, "outcome", s.Outcome)
			}
		}
	}
	if !added {
		return
	}
	lines := make([]string, 0, len(set))
	for h := range set {
		lines = append(lines, h)
	}
	slices.Sort(lines)
	path := filepath.Join(c.env.dir, rejectedFile)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		c.log.Error("cannot write the router rejection marker", "path", path, "err", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		c.log.Error("cannot write the router rejection marker", "path", path, "err", err)
	}
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
		fmt.Fprintf(w, "  %s at %s: %s\n", src.Model(), src.Address(), checkPhrase(res))
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
	}
	return res.Outcome
}
