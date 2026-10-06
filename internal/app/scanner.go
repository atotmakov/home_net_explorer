package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/clock"
	"github.com/atotmakov/home_net_explorer/internal/collect"
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

	trigger chan struct{}
	done    chan struct{}
	started bool

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
