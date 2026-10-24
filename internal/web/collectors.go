package web

import (
	"bytes"
	"database/sql"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/auth"
	"github.com/atotmakov/home_net_explorer/internal/inventory"
	"github.com/atotmakov/home_net_explorer/internal/store"
	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------- collectors (FR-008, FR-012)

type collectorsData struct {
	Collectors []store.CollectorOverview
	Created    *createdCollector
	Downloads  []string
	Schtasks   string
	Paused     bool // the built-in scanner is paused (feature 004)
}

type createdCollector struct {
	Name       string
	ConfigYAML string
	ConfigHref template.URL
}

// collectorConfig is hne-collector.yaml (contracts/collector-cli.md, feature 009).
type collectorConfig struct {
	ServerURL       string
	Name            string
	Token           string
	Subnets         []string
	ExtraSubnets    []string
	IntervalSeconds int
	// No routers: collectors get routers and access points from the server at each scan
	// (features 012, 013).
}

// configHelp is the # comment written above each setting of hne-collector.yaml (features 008,
// 009), one string per comment line.
var configHelp = []struct {
	key  string
	help []string
}{
	{"server_url", []string{"Address of the Home Net Explorer server that this collector sends its scans to."}},
	{"name", []string{"This collector's name on the server's Collectors page."}},
	{"token", []string{"Secret key that lets this collector upload. Shown only once; keep this file private."}},
	{"subnets", []string{
		"Subnets to scan. Empty []: every private subnet this computer is connected to,",
		"found again at each scan (subnets wider than /22 are skipped).",
		"A list, e.g. [192.168.x.0/24]: scan only these subnets."}},
	{"extra_subnets", []string{
		"Subnets scanned in addition to the ones above, e.g. [192.168.y.0/24] behind a",
		"router. Devices there are found by ping/TCP, without MAC addresses."}},
	{"interval_seconds", []string{
		`Seconds between scans for "hne-collector run" (900 = 15 minutes).`,
		"Routers and access points are set up on their device page in the web UI;",
		"the collector gets them from the server at each scan."}},
}

// commented renders the config as YAML with a # comment line above each setting.
func (c collectorConfig) commented() []byte {
	if c.Subnets == nil {
		c.Subnets = []string{}
	}
	if c.ExtraSubnets == nil {
		c.ExtraSubnets = []string{}
	}
	values := map[string]any{"server_url": c.ServerURL, "name": c.Name, "token": c.Token, "subnets": c.Subnets,
		"extra_subnets": c.ExtraSubnets, "interval_seconds": c.IntervalSeconds}
	var b bytes.Buffer
	for i, h := range configHelp {
		if i > 0 {
			b.WriteString("\n")
		}
		for _, line := range h.help {
			b.WriteString("# " + line + "\n")
		}
		enc := yaml.NewEncoder(&b)
		enc.SetIndent(2)
		enc.Encode(map[string]any{h.key: values[h.key]}) // one key: quoted and indented by yaml
		enc.Close()
	}
	return b.Bytes()
}

// schtasks registers the collector to run at log-on (contracts/collector-cli.md "Scheduling").
const schtasks = `schtasks /Create /TN "Home Net Explorer collector" /SC ONLOGON /TR "\"%USERPROFILE%\hne-collector\hne-collector.exe\" run"`

func (s *Server) renderCollectors(w http.ResponseWriter, r *http.Request, status int, errMsg string, created *createdCollector) {
	cs, err := s.opts.Store.CollectorOverviews(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	data := collectorsData{Collectors: cs, Created: created, Downloads: s.availableDownloads(), Schtasks: schtasks,
		Paused: s.scanView(r.Context()).Paused}
	s.render(w, r, status, "collectors.html", page{Title: "Collectors", Nav: true, Active: "collectors", Error: errMsg, Data: data})
}

func (s *Server) handleCollectors(w http.ResponseWriter, r *http.Request) {
	s.renderCollectors(w, r, http.StatusOK, "", nil)
}

// handleCollectorCreate registers a remote collector and shows its token once, inside a
// ready-to-save hne-collector.yaml.
func (s *Server) handleCollectorCreate(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PostFormValue("name"))
	token, _, err := auth.CreateCollector(r.Context(), s.opts.Store.DB(), name, s.opts.Clock.Now())
	switch {
	case errors.Is(err, auth.ErrBadCollectorName):
		s.renderCollectors(w, r, http.StatusBadRequest, "Use 1–64 lower-case letters, digits or dashes for the name.", nil)
		return
	case errors.Is(err, auth.ErrCollectorExists):
		s.renderCollectors(w, r, http.StatusBadRequest, "A collector named "+name+" already exists.", nil)
		return
	case err != nil:
		s.serverError(w, r, err)
		return
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	cfg := collectorConfig{ServerURL: scheme + "://" + r.Host, Name: name, Token: token, Subnets: []string{}, IntervalSeconds: 900}
	b := cfg.commented()
	created := &createdCollector{
		Name:       name,
		ConfigYAML: string(b),
		ConfigHref: template.URL("data:application/yaml;charset=utf-8," + url.PathEscape(string(b))),
	}
	s.renderCollectors(w, r, http.StatusOK, "", created)
}

func (s *Server) handleCollectorRevoke(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := auth.RevokeCollector(r.Context(), s.opts.Store.DB(), id, s.opts.Clock.Now()); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/collectors", http.StatusSeeOther)
}

// ---------------------------------------------------------------- downloads (T067)

var downloadName = regexp.MustCompile(`^hne-collector-(windows|linux)-(amd64|arm64)(\.exe)?$`)

func (s *Server) availableDownloads() []string {
	if s.opts.DownloadsDir == "" {
		return nil
	}
	entries, err := os.ReadDir(s.opts.DownloadsDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && downloadName.MatchString(e.Name()) {
			out = append(out, e.Name())
		}
	}
	return out
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if s.opts.DownloadsDir == "" || !downloadName.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.opts.DownloadsDir, name)
	if _, err := os.Stat(path); err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(w, r, path)
}

// ---------------------------------------------------------------- subnets (T068)

// handleSubnetAttrs renames or ignores/unignores a subnet (user_subnet_attrs facts).
func (s *Server) handleSubnetAttrs(w http.ResponseWriter, r *http.Request) {
	cidr := r.PostFormValue("cidr")
	var exists int
	if err := s.opts.Store.DB().QueryRowContext(r.Context(), `SELECT count(*) FROM subnets WHERE cidr = ?`, cidr).Scan(&exists); err != nil {
		s.serverError(w, r, err)
		return
	}
	if exists == 0 {
		http.Error(w, "unknown subnet", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	var changes [][2]string
	if _, ok := r.PostForm["name"]; ok {
		changes = append(changes, [2]string{"name", strings.TrimSpace(r.PostForm.Get("name"))})
	}
	if v, ok := r.PostForm["ignored"]; ok {
		if v[0] != "true" && v[0] != "false" {
			http.Error(w, "ignored must be true or false", http.StatusBadRequest)
			return
		}
		changes = append(changes, [2]string{"ignored", v[0]})
	}
	at := s.opts.Clock.Now()
	err := s.opts.Ingester.Do(r.Context(), func(tx *sql.Tx) error {
		for _, c := range changes {
			if err := inventory.SetSubnetAttr(r.Context(), tx, cidr, c[0], c[1], at); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

// newSubnets are subnets discovered in the last 7 days (the home page notice, SC-010).
func newSubnets(subnets []store.SubnetInfo, now time.Time) []store.SubnetInfo {
	var out []store.SubnetInfo
	for _, sn := range subnets {
		if !sn.Ignored && now.Sub(sn.FirstSeen) <= 7*24*time.Hour {
			out = append(out, sn)
		}
	}
	return out
}
