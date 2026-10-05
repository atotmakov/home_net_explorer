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

	"github.com/atotmakov/home_net_explorer/internal/auth"
	"github.com/atotmakov/home_net_explorer/internal/clock"
	"github.com/atotmakov/home_net_explorer/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// SessionCookie is the name of the owner session cookie.
const SessionCookie = "hne_session"

// Options are the server's dependencies.
type Options struct {
	Owner   *auth.Owner
	Clock   clock.Clock
	Log     *slog.Logger
	Store   *store.Store
	Scanner Scanner    // nil when the built-in collector is disabled
	Facts   FactWriter // serializes user edits with ingest
	Version string
}

// Scanner is the built-in collector as seen by the UI.
type Scanner interface {
	Trigger() bool
	Status() ScanStatus
}

// ScanStatus describes the built-in collector's current/last scan.
type ScanStatus struct {
	Running      bool
	LastStarted  time.Time
	LastFinished time.Time
	LastFound    int
	LastError    string
}

// FactWriter runs a user-fact transaction serialized with ingest (ingest.Ingester.Do).
type FactWriter interface {
	Do(ctx context.Context, f func(tx *sql.Tx) error) error
}

// Server is the HTTP front end: owner UI and /api/v1.
type Server struct {
	opts    Options
	mux     *http.ServeMux
	pages   map[string]*template.Template
	limiter *auth.RateLimiter
	cop     *http.CrossOriginProtection
}

// page is the data passed to every full-page template.
type page struct {
	Title  string
	Nav    bool   // show the navigation bar (logged in)
	Active string // highlighted nav item
	Error  string
	Data   any
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
		opts:    opts,
		mux:     http.NewServeMux(),
		limiter: auth.NewRateLimiter(5, time.Minute, opts.Clock),
		cop:     http.NewCrossOriginProtection(),
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
	"statuses": func() []string { return []string{"online", "offline", "new", "new_offline"} },
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
	s.mux.HandleFunc("GET /setup", s.handleSetupForm)
	s.mux.HandleFunc("POST /setup", s.handleSetup)
	s.mux.HandleFunc("GET /login", s.handleLoginForm)
	s.mux.HandleFunc("POST /login", s.handleLogin)
	s.mux.HandleFunc("POST /logout", s.handleLogout)
	s.mux.HandleFunc("GET /{$}", s.handleHome)
	s.mux.HandleFunc("GET /devices", s.handleDevices)
	s.mux.HandleFunc("GET /devices/{id}", s.handleDevice)
	s.mux.HandleFunc("POST /devices/{id}/attrs", s.handleDeviceAttrs)
	s.mux.HandleFunc("POST /scan", s.handleScan)
	s.mux.HandleFunc("GET /ui/scan-status", s.handleScanStatus)
	s.mux.HandleFunc("GET /settings", s.handleSettings)
	s.mux.HandleFunc("POST /settings", s.handleSettingsSave)
}

// Handler returns the full middleware chain.
func (s *Server) Handler() http.Handler {
	var h http.Handler = s.mux
	h = s.authGate(h)
	h = s.cop.Handler(h)
	h = s.recoverPanics(h)
	return s.logRequests(h)
}

// authGate: first run → /setup; afterwards every page needs an owner session (FR-030).
func (s *Server) authGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/healthz" || strings.HasPrefix(p, "/static/") || strings.HasPrefix(p, "/api/v1/") {
			next.ServeHTTP(w, r)
			return
		}
		has, err := s.opts.Owner.HasPassword(r.Context())
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		switch {
		case !has && p == "/setup":
			next.ServeHTTP(w, r)
		case !has:
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
		case p == "/setup":
			http.NotFound(w, r)
		case p == "/login":
			next.ServeHTTP(w, r)
		case !s.loggedIn(r):
			http.Redirect(w, r, "/login", http.StatusSeeOther)
		default:
			next.ServeHTTP(w, r)
		}
	})
}

func (s *Server) loggedIn(r *http.Request) bool {
	c, err := r.Cookie(SessionCookie)
	if err != nil {
		return false
	}
	ok, err := s.opts.Owner.ValidSession(r.Context(), c.Value)
	return err == nil && ok
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
