// Command hne-server runs the Home Net Explorer server: web UI, upload API, datastore and the
// built-in collector for the NAS's own subnets.
//
//	hne-server [serve] [--listen :8080] [--data /data] [--no-builtin-scan]
//	hne-server rebuild       recompute all projections from the stored runs and user facts
//	hne-server healthcheck   exit 0 if the server answers /healthz (container HEALTHCHECK)
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/app"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 && (args[0] == "serve" || args[0] == "healthcheck" || args[0] == "rebuild") {
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
	case "rebuild":
		err = rebuild(cfg)
	default:
		err = serve(cfg)
	}
	if err != nil {
		slog.Error("hne-server", "cmd", cmd, "err", err)
		os.Exit(1)
	}
}

func options(cfg Config, log *slog.Logger, noScan bool) app.Options {
	return app.Options{
		DataDir:       cfg.DataDir,
		Log:           log,
		ScanInterval:  cfg.ScanInterval,
		Subnets:       cfg.Subnets,
		DNSServer:     cfg.DNSServer,
		NoBuiltinScan: noScan,
		Version:       version,
	}
}

func serve(cfg Config) error {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(log)
	log.Info("starting", "version", version, "listen", cfg.Listen, "data", cfg.DataDir, "builtin_scan", !cfg.NoBuiltinScan)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a, err := app.New(ctx, options(cfg, log, cfg.NoBuiltinScan))
	if err != nil {
		return err
	}
	a.Start(ctx)
	defer a.Close()

	httpSrv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           a.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- httpSrv.ListenAndServe() }()
	select {
	case err := <-errc:
		stop()
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

func rebuild(cfg Config) error {
	ctx := context.Background()
	a, err := app.New(ctx, options(cfg, slog.Default(), true))
	if err != nil {
		return err
	}
	defer a.Close()
	start := time.Now()
	if err := a.Rebuild(ctx); err != nil {
		return err
	}
	fmt.Printf("rebuilt projections in %s\n", time.Since(start).Round(time.Millisecond))
	return nil
}
