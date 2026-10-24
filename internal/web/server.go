package web

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/clock"
	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// Options are the server's dependencies.
type Options struct {
	Clock    clock.Clock
	Log      *slog.Logger
	Store    *store.Store
	Scanner  Scanner  // nil when the built-in collector is disabled
	Ingester Ingester // uploads, and user edits serialized with ingest
	Version  string   // app version, shown in every page footer
	Commit   string   // short commit hash (footer tooltip)
	// DownloadsDir holds the collector binaries served at /downloads/ (built into the image).
	DownloadsDir string
	// SettingDefaults are the settings written again by "drop all data" (feature 004).
	SettingDefaults map[string]string
}

// Scanner is the built-in collector as seen by the UI.
type Scanner interface {
	Trigger() TriggerResult
	Status() ScanStatus
	Paused(ctx context.Context) (bool, time.Time)    // paused by the owner, and since when
	SetPaused(ctx context.Context, pause bool) error // stored; survives restarts (feature 004)
}

// TriggerResult is the answer to "Scan now".
type TriggerResult int

const (
	TriggerStarted TriggerResult = iota
	TriggerRunning               // a scan is already running
	TriggerPaused                // the owner paused the built-in scanner
)

// ScanStatus describes the built-in collector's current/last scan.
type ScanStatus struct {
	Running      bool
	LastStarted  time.Time
	LastFinished time.Time
	LastFound    int
	LastError    string
}

// Ingester stores uploaded runs and runs user-fact transactions serialized with ingest
// (implemented by ingest.Ingester).
type Ingester interface {
	Ingest(ctx context.Context, collectorID int64, raw []byte, run *contract.CollectionRun, receivedAt time.Time) (contract.UploadResult, error)
	Do(ctx context.Context, f func(tx *sql.Tx) error) error
}

// Server is the HTTP front end: the web UI (open to the home network, no login; feature 007)
// and /api/v1 (collector bearer tokens).
type Server struct {
	opts  Options
	mux   *http.ServeMux
	pages map[string]*template.Template
	cop   *http.CrossOriginProtection
}

// page is the data passed to every full-page template.
type page struct {
	Version string // filled in by render
	Commit  string
	Title   string
	Nav     bool   // show the navigation bar
	Active  string // highlighted nav item
	Error   string
	Data    any
}

// New builds a server.
func New(opts Options) (*Server, error) {
	if opts.Clock == nil {
		opts.Clock = clock.Real{}
	}
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	s := &Server{
		opts: opts,
		mux:  http.NewServeMux(),
		cop:  http.NewCrossOriginProtection(),
	}
	// Collectors authenticate with bearer tokens, not cookies, so CSRF checks don't apply.
	s.cop.AddInsecureBypassPattern("/api/v1/")
	if err := s.loadTemplates(); err != nil {
		return nil, err
	}
	s.routes()
	return s, nil
}

func (s *Server) loadTemplates() error {
	names, err := fs.Glob(templateFS, "templates/*.html")
	if err != nil {
		return err
	}
	s.pages = map[string]*template.Template{}
	for _, n := range names {
		base := strings.TrimPrefix(n, "templates/")
		if base == "layout.html" {
			continue
		}
		t, err := template.New(base).Funcs(funcs).ParseFS(templateFS, "templates/layout.html", n)
		if err != nil {
			return fmt.Errorf("web: template %s: %w", base, err)
		}
		s.pages[base] = t
	}
	return nil
}

var funcs = template.FuncMap{
	"statuses":      func() []string { return store.States },
	"stateLabel":    stateLabel,
	"ago":           ago,
	"badgeLabel":    badgeLabel,
	"routerOutcome": routerOutcome,
	"scanProblem":   scanProblem,
	"readOutcome": func(r store.RouterRead) string {
		return routerOutcome(store.RouterStatus{Type: r.Type, Outcome: r.Outcome, Online: r.Online, Offline: r.Offline})
	},
	"fmtTime": func(t time.Time) string {
		if t.IsZero() {
			return "—"
		}
		return t.Local().Format("2006-01-02 15:04")
	},
}

func (s *Server) routes() {
	static, _ := fs.Sub(staticFS, "static")
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("ok"))
	})
	s.mux.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"version": s.opts.Version, "commit": s.opts.Commit})
	})
	// There is no login any more (feature 007): old bookmarks of these pages lead home.
	home := func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/", http.StatusSeeOther) }
	s.mux.HandleFunc("GET /setup", home)
	s.mux.HandleFunc("GET /login", home)
	s.mux.HandleFunc("GET /{$}", s.handleHome)
	s.mux.HandleFunc("GET /devices", s.handleDevices)
	s.mux.HandleFunc("GET /devices/{id}", s.handleDevice)
	s.mux.HandleFunc("POST /devices/{id}/attrs", s.handleDeviceAttrs)
	s.mux.HandleFunc("POST /devices/{id}/router", s.handleRouterSave)
	s.mux.HandleFunc("POST /devices/{id}/router/remove", s.handleRouterRemove)
	s.mux.HandleFunc("POST /devices/{id}/access-point", s.handleAccessPointSave)    // feature 012
	s.mux.HandleFunc("POST /devices/{id}/access-point/remove", s.handleRouterRemove) // same settings row
	s.mux.HandleFunc("POST /devices/{id}/link", s.handleDeviceLink)
	s.mux.HandleFunc("GET /map", s.handleMap)
	s.mux.HandleFunc("GET /ui/map.json", s.handleMapJSON)
	s.mux.HandleFunc("POST /scan", s.handleScan)
	s.mux.HandleFunc("GET /ui/scan-status", s.handleScanStatus)
	s.mux.HandleFunc("GET /settings", s.handleSettings)
	s.mux.HandleFunc("POST /settings", s.handleSettingsSave)
	s.mux.HandleFunc("POST /subnets", s.handleSubnetAttrs)
	s.mux.HandleFunc("POST /maintenance/devices", s.handleRemoveDevices)
	s.mux.HandleFunc("POST /maintenance/collectors", s.handleRemoveCollectors)
	s.mux.HandleFunc("POST /maintenance/everything", s.handleDropAll)
	s.mux.HandleFunc("POST /maintenance/pause", s.handlePause)
	s.mux.HandleFunc("POST /maintenance/resume", s.handleResume)
	s.mux.HandleFunc("GET /collectors", s.handleCollectors)
	s.mux.HandleFunc("POST /collectors", s.handleCollectorCreate)
	s.mux.HandleFunc("POST /collectors/{id}/revoke", s.handleCollectorRevoke)
	s.mux.HandleFunc("GET /downloads/{file}", s.handleDownload)
	s.mux.HandleFunc("POST /api/v1/collections", s.handleUpload)
	s.mux.HandleFunc("GET /api/v1/ping", s.handlePing)
	s.mux.HandleFunc("GET /api/v1/routers/{id}/login", s.handleRouterLogin)
}

// Handler returns the full middleware chain.
func (s *Server) Handler() http.Handler {
	var h http.Handler = s.mux
	h = s.cop.Handler(h)
	h = s.recoverPanics(h)
	return s.logRequests(h)
}

func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				s.serverError(w, r, fmt.Errorf("panic: %v", v))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if r.URL.Path == "/healthz" || strings.HasPrefix(r.URL.Path, "/static/") {
			return
		}
		s.opts.Log.Info("http", "method", r.Method, "path", r.URL.Path, "status", rec.status,
			"duration", time.Since(start).Round(time.Millisecond))
	})
}

func (s *Server) serverError(w http.ResponseWriter, r *http.Request, err error) {
	s.opts.Log.Error("http", "method", r.Method, "path", r.URL.Path, "err", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// render executes a page template inside the layout. Output is buffered so a template error
// never produces a half-written page.
func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, name string, p page) {
	t, ok := s.pages[name]
	if !ok {
		s.serverError(w, r, fmt.Errorf("unknown template %s", name))
		return
	}
	p.Version, p.Commit = s.opts.Version, s.opts.Commit
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", p); err != nil {
		s.serverError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

// renderPartial executes a named block (htmx fragment) without the layout.
func (s *Server) renderPartial(w http.ResponseWriter, r *http.Request, name, block string, data any) {
	s.renderPartialStatus(w, r, http.StatusOK, name, block, data)
}

// renderPartialStatus is renderPartial with an explicit status code.
func (s *Server) renderPartialStatus(w http.ResponseWriter, r *http.Request, status int, name, block string, data any) {
	t, ok := s.pages[name]
	if !ok {
		s.serverError(w, r, fmt.Errorf("unknown template %s", name))
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, block, data); err != nil {
		s.serverError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

// routerOutcome is the owner-facing text of a router or access point read (Collectors page,
// FR-013; access points: feature 012).
func routerOutcome(r store.RouterStatus) string {
	ap := r.Type == contract.SourceTypeAccessPoint
	switch r.Outcome {
	case contract.OutcomeOK:
		if ap {
			return fmt.Sprintf("OK, %d clients, %d left out", r.Online, r.Offline)
		}
		return fmt.Sprintf("OK, %d online / %d offline", r.Online, r.Offline)
	case contract.OutcomeUnreachable:
		return "unreachable"
	case contract.OutcomeLoginRejected:
		return "login rejected"
	case contract.OutcomeLocked:
		if ap {
			return "locked by the access point (too many failed logins)"
		}
		return "locked by the router"
	case contract.OutcomeSessionBusy:
		return "busy (someone is logged into the router)"
	case contract.OutcomePageNotUnderstood:
		return "page not understood (firmware?)"
	case contract.OutcomeSkippedAfterRejection:
		return "skipped: login was rejected, fix the password and run check"
	case contract.OutcomeLoginUnavailable:
		return "login unavailable from the server"
	case contract.OutcomeControllerManaged:
		return "managed by a controller (Omada): no client list of its own"
	}
	return r.Outcome
}

// scanProblem is the owner-facing reason a subnet scan failed (feature 005).
func scanProblem(reason string) string {
	if reason == contract.SkipFakedAnswers {
		return "answers faked on the network path (VPN?): 90% or more of the addresses answered, so nothing was recorded"
	}
	return reason
}

// stateLabel names a shown device state in the status filter (feature 006).
func stateLabel(state string) string {
	if state == store.StateStale {
		return "online (stale)"
	}
	return state
}

// badgeLabel is the text of a device's status badge: stale devices read "online" too; the color
// and the "last seen" tooltip tell fresh from stale (feature 006, FR-001).
func badgeLabel(state string) string {
	if state == store.StateOffline {
		return "offline"
	}
	return "online"
}

// ago formats an age for the "last seen … ago" tooltips (shared with the map, store.Ago).
func ago(d time.Duration) string { return store.Ago(d) }
