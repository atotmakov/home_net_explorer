// Command hne-server runs the Home Net Explorer server: web UI, upload API, datastore and the
// built-in collector for the NAS's own subnets.
//
//	hne-server [serve] [--listen :8080] [--data /data] [--no-builtin-scan]
//	hne-server healthcheck
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/auth"
	"github.com/atotmakov/home_net_explorer/internal/clock"
	"github.com/atotmakov/home_net_explorer/internal/store"
	"github.com/atotmakov/home_net_explorer/internal/web"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

// Settings keys and defaults.
const (
	settingOfflineMultiplier = "offline_multiplier"
	builtinCollector         = "nas"
)

func main() {
	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 && (args[0] == "serve" || args[0] == "healthcheck") {
		cmd, args = args[0], args[1:]
	}
	cfg, err := loadConfig(args, os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hne-server:", err)
		os.Exit(2)
	}
	switch cmd {
	case "healthcheck":
		os.Exit(runHealthcheck(cfg.Listen))
	default:
		if err := serve(cfg); err != nil {
			slog.Error("hne-server stopped", "err", err)
			os.Exit(1)
		}
	}
}

func serve(cfg Config) error {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(log)
	log.Info("starting", "version", version, "listen", cfg.Listen, "data", cfg.DataDir)

	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(cfg.DataDir, "hne.db"))
	if err != nil {
		return err
	}
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if _, err := st.EnsureCollector(ctx, builtinCollector, store.KindBuiltin, int(cfg.ScanInterval.Seconds())); err != nil {
		return err
	}
	if err := st.SetDefaultSetting(ctx, settingOfflineMultiplier, "3"); err != nil {
		return err
	}

	clk := clock.Real{}
	srv, err := web.New(web.Options{Owner: auth.NewOwner(st.DB(), clk), Clock: clk, Log: log})
	if err != nil {
		return err
	}
	httpSrv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errc := make(chan error, 1)
	go func() { errc <- httpSrv.ListenAndServe() }()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shutdownCtx)
}
