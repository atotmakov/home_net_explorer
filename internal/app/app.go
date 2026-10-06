package app

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/auth"
	"github.com/atotmakov/home_net_explorer/internal/clock"
	"github.com/atotmakov/home_net_explorer/internal/collect"
	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/ingest"
	"github.com/atotmakov/home_net_explorer/internal/inventory"
	"github.com/atotmakov/home_net_explorer/internal/store"
	"github.com/atotmakov/home_net_explorer/internal/web"
)

// Settings keys.
const (
	SettingOfflineMultiplier = "offline_multiplier"
	SettingBuiltinInterval   = "builtin_interval_seconds"
	BuiltinCollector         = "nas"
)

// Options configure the server.
type Options struct {
	DataDir       string
	Clock         clock.Clock
	Log           *slog.Logger
	ScanInterval  time.Duration  // default built-in scan interval (overridable in Settings)
	Subnets       []netip.Prefix // optional restriction for the built-in collector
	DNSServer     netip.Addr     // optional private DNS server for PTR lookups
	NoBuiltinScan bool
	Engine        *collect.Engine // override the platform scan engine (tests)
	Version       string
}

// App is an assembled server.
type App struct {
	Store    *store.Store
	Applier  *inventory.Applier
	Ingester *ingest.Ingester
	Scanner  *Scanner // nil when the built-in collector is disabled
	Web      *web.Server

	closePlatform func() error
}

// New opens the data directory and wires every component.
func New(ctx context.Context, o Options) (*App, error) {
	if o.Clock == nil {
		o.Clock = clock.Real{}
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.ScanInterval <= 0 {
		o.ScanInterval = 15 * time.Minute
	}
	if err := os.MkdirAll(o.DataDir, 0o750); err != nil {
		return nil, err
	}
	st, err := store.Open(filepath.Join(o.DataDir, "hne.db"))
	if err != nil {
		return nil, err
	}
	a := &App{Store: st, Applier: inventory.NewApplier()}
	a.Ingester = ingest.New(st, a.Applier)

	cid, err := st.EnsureCollector(ctx, BuiltinCollector, store.KindBuiltin, int(o.ScanInterval.Seconds()))
	if err == nil {
		err = st.SetDefaultSetting(ctx, SettingOfflineMultiplier, strconv.Itoa(inventory.DefaultOfflineMultiplier))
	}
	if err == nil {
		err = st.SetDefaultSetting(ctx, SettingBuiltinInterval, strconv.Itoa(int(o.ScanInterval.Seconds())))
	}
	if err != nil {
		st.Close()
		return nil, err
	}

	var scanner web.Scanner
	if !o.NoBuiltinScan {
		engine := o.Engine
		if engine == nil {
			engine, a.closePlatform = platformEngine(o)
		}
		a.Scanner = &Scanner{
			engine: engine, ingester: a.Ingester, store: st, collectorID: cid,
			clock: o.Clock, log: o.Log, subnets: o.Subnets, defaultInterval: o.ScanInterval,
			trigger: make(chan struct{}, 1), done: make(chan struct{}),
		}
		scanner = a.Scanner
	}

	a.Web, err = web.New(web.Options{
		Owner:   auth.NewOwner(st.DB(), o.Clock),
		Clock:   o.Clock,
		Log:     o.Log,
		Store:   st,
		Scanner: scanner,
		Facts:   a.Ingester,
		Version: o.Version,
	})
	if err != nil {
		a.Close()
		return nil, err
	}
	return a, nil
}

func platformEngine(o Options) (*collect.Engine, func() error) {
	prober, presence, neighbors, routes, closeFn := collect.Platform()
	return &collect.Engine{
		Prober: prober, Presence: presence, Neighbors: neighbors, Routes: routes,
		NewResolver: func(v contract.Vantage) collect.Resolver {
			server, _ := collect.PickDNSServer(o.DNSServer, v)
			r, err := collect.NewNameResolver(server)
			if err != nil {
				return nil
			}
			return r
		},
		Clock:     o.Clock,
		Collector: contract.CollectorInfo{Name: BuiltinCollector, Version: version(o.Version), OS: runtime.GOOS},
	}, closeFn
}

func version(v string) string {
	if v == "" {
		return "dev"
	}
	return v
}

// Start launches background work (the built-in collector loop).
func (a *App) Start(ctx context.Context) {
	if a.Scanner != nil {
		a.Scanner.started = true
		go a.Scanner.Run(ctx)
	}
}

// Handler is the HTTP handler.
func (a *App) Handler() http.Handler { return a.Web.Handler() }

// Close waits for the scan loop to stop (cancel Start's context first) and releases resources.
func (a *App) Close() error {
	if a.Scanner != nil && a.Scanner.started {
		<-a.Scanner.done
	}
	var errs []error
	if a.closePlatform != nil {
		errs = append(errs, a.closePlatform())
	}
	errs = append(errs, a.Store.Close())
	return errors.Join(errs...)
}

// Rebuild replays all facts into fresh projections, serialized with ingest.
func (a *App) Rebuild(ctx context.Context) error {
	return a.Ingester.Do(ctx, func(tx *sql.Tx) error { return inventory.RebuildTx(ctx, tx, a.Applier) })
}
