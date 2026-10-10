package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/clock"
	"github.com/atotmakov/home_net_explorer/internal/collect"
	"github.com/atotmakov/home_net_explorer/internal/collect/router"
	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/ingest"
	"github.com/atotmakov/home_net_explorer/internal/store"
	"github.com/atotmakov/home_net_explorer/internal/web"
)

// Scanner is the built-in collector: it scans the NAS's own subnets at start-up, every
// interval, and on demand, and ingests each run through the same path as uploads (Principle IV).
type Scanner struct {
	engine          *collect.Engine
	ingester        *ingest.Ingester
	store           *store.Store
	collectorID     int64
	clock           clock.Clock
	log             *slog.Logger
	subnets         []netip.Prefix
	defaultInterval time.Duration
	routerRT        http.RoundTripper

	trigger chan struct{}
	done    chan struct{}
	started bool

	rejMu     sync.Mutex // guards router_rejected_builtin
	mu        sync.Mutex
	status    web.ScanStatus
	lastStart time.Time
}

// Run scans immediately, then on every interval or trigger, until ctx is cancelled.
func (s *Scanner) Run(ctx context.Context) {
	defer close(s.done)
	s.scanOnce(ctx)
	for {
		next := s.lastStarted().Add(s.interval(ctx))
		select {
		case <-ctx.Done():
			return
		case <-s.trigger:
		case <-s.clock.After(next.Sub(s.clock.Now())):
		}
		if ctx.Err() != nil {
			return
		}
		s.scanOnce(ctx)
	}
}

// Trigger requests an on-demand scan. It returns false if a scan is already running.
func (s *Scanner) Trigger() bool {
	s.mu.Lock()
	running := s.status.Running
	s.mu.Unlock()
	if running {
		return false
	}
	select {
	case s.trigger <- struct{}{}:
	default: // one is already pending
	}
	return true
}

// Status reports the current scan state.
func (s *Scanner) Status() web.ScanStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *Scanner) lastStarted() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastStart
}

func (s *Scanner) interval(ctx context.Context) time.Duration {
	if v, ok, err := s.store.Setting(ctx, SettingBuiltinInterval); err == nil && ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return s.defaultInterval
}

func (s *Scanner) scanOnce(ctx context.Context) {
	now := s.clock.Now()
	s.mu.Lock()
	s.status.Running, s.status.LastStarted = true, now
	s.lastStart = now
	s.mu.Unlock()

	found, err := s.scan(ctx)

	s.mu.Lock()
	s.status.Running, s.status.LastFinished = false, s.clock.Now()
	s.status.LastFound = found
	s.status.LastError = ""
	if err != nil {
		s.status.LastError = err.Error()
		s.log.Error("built-in scan failed", "err", err)
	}
	s.mu.Unlock()
}

func (s *Scanner) scan(ctx context.Context) (int, error) {
	ignored, err := s.store.IgnoredSubnets(ctx)
	if err != nil {
		return 0, err
	}
	s.engine.IntervalSeconds = int(s.interval(ctx).Seconds())
	if s.engine.Routers, err = s.routers(ctx); err != nil {
		return 0, err
	}
	run, err := s.engine.Scan(ctx, collect.ScanOptions{Targets: s.subnets, Ignored: ignored})
	if err != nil {
		return 0, err
	}
	now := s.clock.Now()
	run.SentAt = now
	if err := contract.Validate(run); err != nil {
		return 0, err
	}
	raw, err := json.Marshal(run)
	if err != nil {
		return 0, err
	}
	// Ingest even if ctx was cancelled mid-scan, so a partial run isn't lost.
	if _, err := s.ingester.Ingest(context.WithoutCancel(ctx), s.collectorID, raw, run, now); err != nil {
		return 0, err
	}
	s.log.Info("built-in scan", "subnets", len(run.Subnets), "found", len(run.Observations),
		"duration", run.FinishedAt.Sub(run.StartedAt).Round(time.Millisecond))
	return len(run.Observations), nil
}

// routers returns the routers set up in the web UI as sources for the built-in collector
// (feature 003, research R8). Logins are read from the store right before each read; rejected
// logins are remembered in the setting router_rejected_builtin (FR-014).
func (s *Scanner) routers(ctx context.Context) ([]router.Source, error) {
	list, err := s.store.ListRouters(ctx)
	if err != nil {
		return nil, err
	}
	var out []router.Source
	for _, rr := range list {
		addr, err := netip.ParseAddr(rr.Address)
		if err != nil {
			continue
		}
		prefix, err := netip.ParsePrefix(rr.Subnet)
		if err != nil {
			continue
		}
		id := rr.ID
		login := func(ctx context.Context) (string, router.Secret, error) {
			l, err := s.store.RouterLogin(ctx, id)
			if err != nil {
				return "", "", err
			}
			return l.Username, router.Secret(l.Password), nil
		}
		out = append(out, router.NewRemote(rr.Model, addr, prefix, login, builtinRejections{s}, s.routerRT))
	}
	return out, nil
}

// builtinRejections keeps rejected login hashes (never credentials) in a server setting, so a
// wrong password saved in the UI is tried once until it changes, also across restarts.
type builtinRejections struct{ s *Scanner }

func (b builtinRejections) load(ctx context.Context) []string {
	v, ok, err := b.s.store.Setting(ctx, store.SettingRouterRejectedBuiltin)
	if err != nil || !ok {
		return nil
	}
	var hs []string
	json.Unmarshal([]byte(v), &hs)
	return hs
}

func (b builtinRejections) Rejected(hash string) bool {
	return slices.Contains(b.load(context.Background()), hash)
}

func (b builtinRejections) Reject(hash string) {
	b.s.rejMu.Lock()
	defer b.s.rejMu.Unlock()
	ctx := context.Background()
	hs := b.load(ctx)
	if slices.Contains(hs, hash) {
		return
	}
	v, _ := json.Marshal(append(hs, hash))
	if err := b.s.store.SetSetting(ctx, store.SettingRouterRejectedBuiltin, string(v)); err != nil {
		b.s.log.Error("cannot record a rejected router login", "err", err)
		return
	}
	b.s.log.Warn("router login rejected; the built-in collector skips it until the router is saved again in the web UI")
}
