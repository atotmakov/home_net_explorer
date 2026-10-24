package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strings"

	"github.com/atotmakov/home_net_explorer/internal/collect/accesspoint"
	"github.com/atotmakov/home_net_explorer/internal/collect/router"
	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// Access points (feature 012) are configured only in the web UI: the collector learns them from
// ping, fetches each login from the server right before the read, and shares the rejection
// marker with routers (the hashes include the model, so they never collide).

// apCacheFile keeps the last access point list from the server (no credentials), for scans
// while the server is unreachable.
const apCacheFile = "hne-collector.access-points"

func (c *collector) saveAPCache(aps []contract.RouterRef) {
	if aps == nil {
		aps = []contract.RouterRef{}
	}
	b, err := json.Marshal(aps)
	if err != nil {
		return
	}
	path := filepath.Join(c.env.dir, apCacheFile)
	if err := os.WriteFile(path+".tmp", b, 0o600); err == nil {
		os.Rename(path+".tmp", path)
	}
}

func (c *collector) cachedAPs() []contract.RouterRef {
	b, err := os.ReadFile(filepath.Join(c.env.dir, apCacheFile))
	if err != nil {
		return nil
	}
	var aps []contract.RouterRef
	if json.Unmarshal(b, &aps) != nil {
		return nil
	}
	return aps
}

// buildAccessPoints sets the engine's access points from the server's list (or the cache).
func (c *collector) buildAccessPoints(list []contract.RouterRef) {
	var aps []accesspoint.Source
	seen := map[string]bool{}
	for _, ref := range list {
		addr, err := netip.ParseAddr(ref.Address)
		if err != nil || !contract.IsPrivateAddr(addr) || seen[ref.Address] {
			continue
		}
		prefix, err := contract.ParseSubnet(ref.Subnet)
		if err != nil {
			continue
		}
		seen[ref.Address] = true
		id := ref.ID
		login := func(ctx context.Context) (string, router.Secret, error) {
			if id == 0 {
				return "", "", errors.New("access point not known to the server")
			}
			l, err := c.client.RouterLogin(ctx, id)
			if err != nil {
				return "", "", err
			}
			return l.Username, router.Secret(l.Password), nil
		}
		aps = append(aps, accesspoint.NewRemote(ref.Model, addr, prefix, login, markerRejections{c}, c.env.apTransport))
	}
	c.engine.AccessPoints = aps
}

// checkAccessPoints reads each access point once and prints the result: the endpoint used, the
// client count and the key names of the first entry (gate G1), never a value of any client.
func (c *collector) checkAccessPoints(ctx context.Context, w io.Writer) {
	if len(c.engine.AccessPoints) == 0 {
		return
	}
	fmt.Fprintln(w, "Access points:")
	for _, src := range c.engine.AccessPoints {
		rctx, cancel := context.WithTimeout(ctx, accesspoint.DefaultTimeout)
		res := src.Read(rctx)
		cancel()
		fmt.Fprintf(w, "  %s at %s (login from server): %s\n", src.Model(), src.Address(), apCheckPhrase(res))
		if c.jsonOut {
			c.log.Info("access point read", "model", src.Model(), "address", src.Address().String(),
				"outcome", res.Outcome, "clients", len(res.Clients), "endpoint", res.Path, "keys", res.Keys)
		}
	}
}

func apCheckPhrase(res accesspoint.Result) string {
	switch res.Outcome {
	case contract.OutcomeOK:
		noIP := 0
		for _, cl := range res.Clients {
			if !cl.IP.IsValid() {
				noIP++
			}
		}
		keys := strings.Join(res.Keys, ", ")
		if keys == "" {
			keys = "none"
		}
		return fmt.Sprintf("OK, %d clients, %d without an IP address (endpoint %s; keys: %s)", len(res.Clients), noIP, res.Path, keys)
	case contract.OutcomeUnreachable:
		return "unreachable"
	case contract.OutcomeLoginRejected:
		return "login rejected (skipped until its login changes in the web UI or you run check)"
	case contract.OutcomeLocked:
		return "locked by the access point (too many failed logins)"
	case contract.OutcomePageNotUnderstood:
		if res.Detail != "" {
			return "page not understood (firmware?): " + res.Detail
		}
		return "page not understood (firmware?)"
	case contract.OutcomeSkippedAfterRejection:
		return "skipped after a rejected login"
	case contract.OutcomeLoginUnavailable:
		return "login unavailable from the server"
	case contract.OutcomeControllerManaged:
		return "managed by a controller (Omada): no client list of its own"
	}
	return res.Outcome
}
