package web

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"database/sql"
	"github.com/atotmakov/home_net_explorer/internal/inventory"
	"github.com/atotmakov/home_net_explorer/internal/store"
)

// ---------------------------------------------------------------- home

type homeData struct {
	Counts     store.Counts
	Subnets    []store.SubnetInfo
	NewSubnets []store.SubnetInfo
	Scan       *scanView
}

type scanView struct {
	Enabled bool
	Status  ScanStatus
}

func (s *Server) scanView() *scanView {
	if s.opts.Scanner == nil {
		return &scanView{}
	}
	return &scanView{Enabled: true, Status: s.opts.Scanner.Status()}
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	data := homeData{Scan: s.scanView()}
	if st := s.opts.Store; st != nil {
		var err error
		if data.Counts, err = st.HomeCounts(r.Context()); err != nil {
			s.serverError(w, r, err)
			return
		}
		if data.Subnets, err = st.SubnetStatus(r.Context(), s.opts.Clock.Now()); err != nil {
			s.serverError(w, r, err)
			return
		}
		data.NewSubnets = newSubnets(data.Subnets, s.opts.Clock.Now())
	}
	s.render(w, r, http.StatusOK, "home.html", page{Title: "Home", Nav: true, Active: "home", Data: data})
}

// ---------------------------------------------------------------- scan

func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	if s.opts.Scanner == nil {
		http.Error(w, "the built-in collector is disabled", http.StatusServiceUnavailable)
		return
	}
	if !s.opts.Scanner.Trigger() {
		s.renderPartialStatus(w, r, http.StatusConflict, "home.html", "scan-status", s.scanView())
		return
	}
	view := s.scanView()
	view.Status.Running = true // the loop picks the trigger up momentarily
	s.renderPartialStatus(w, r, http.StatusAccepted, "home.html", "scan-status", view)
}

func (s *Server) handleScanStatus(w http.ResponseWriter, r *http.Request) {
	s.renderPartial(w, r, "home.html", "scan-status", s.scanView())
}

// ---------------------------------------------------------------- devices

type devicesData struct {
	Filter  store.DeviceFilter
	Dir     string
	Rows    []store.DeviceRow
	Subnets []store.SubnetInfo
	Sorts   []string
}

func parseFilter(q url.Values) (store.DeviceFilter, error) {
	f := store.DeviceFilter{
		Q:            q.Get("q"),
		Subnet:       q.Get("subnet"),
		Status:       q.Get("status"),
		Manufacturer: q.Get("manufacturer"),
		Sort:         q.Get("sort"),
		Desc:         q.Get("dir") == "desc",
		Randomized:   q.Get("randomized") == "1",
		Weak:         q.Get("weak") == "1",
	}
	for key, dst := range map[string]*time.Time{"seen_after": &f.SeenAfter, "seen_before": &f.SeenBefore} {
		if v := q.Get(key); v != "" {
			t, err := time.ParseInLocation("2006-01-02", v, time.Local)
			if err != nil {
				return f, errors.New(key + " must be YYYY-MM-DD")
			}
			*dst = t
		}
	}
	return f, nil
}

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	f, err := parseFilter(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rows, err := s.opts.Store.ListDevices(r.Context(), f)
	if errors.Is(err, store.ErrBadSort) {
		http.Error(w, "unknown sort key", http.StatusBadRequest)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	data := devicesData{Filter: f, Rows: rows, Sorts: store.SortKeys, Dir: r.URL.Query().Get("dir")}
	if r.Header.Get("HX-Request") == "true" {
		s.renderPartial(w, r, "devices.html", "rows", data)
		return
	}
	if data.Subnets, err = s.opts.Store.SubnetStatus(r.Context(), s.opts.Clock.Now()); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "devices.html", page{Title: "Devices", Nav: true, Active: "devices", Data: data})
}

type deviceData struct {
	Device store.DeviceRow
	Types  []string
	Saved  bool
	Router *routerData // Router card (feature 003); nil unless the type is "router"
}

func (s *Server) loadDevice(w http.ResponseWriter, r *http.Request) (store.DeviceRow, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return store.DeviceRow{}, false
	}
	d, err := s.opts.Store.GetDevice(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return d, false
	}
	if err != nil {
		s.serverError(w, r, err)
		return d, false
	}
	return d, true
}

func (s *Server) handleDevice(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDevice(w, r)
	if !ok {
		return
	}
	rd, err := s.loadRouterData(r, d)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.renderDevice(w, r, http.StatusOK, d, rd)
}

func (s *Server) renderDevice(w http.ResponseWriter, r *http.Request, status int, d store.DeviceRow, rd *routerData) {
	data := deviceData{Device: d, Types: inventory.DeviceTypes, Saved: r.URL.Query().Get("saved") == "1", Router: rd}
	s.render(w, r, status, "device.html", page{Title: d.DisplayName, Nav: true, Active: "devices", Data: data})
}

// handleDeviceAttrs records the fields present in the form as user facts (FR-014).
func (s *Server) handleDeviceAttrs(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDevice(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	current := map[string]string{"name": d.UserName, "notes": d.Notes, "type": d.Type}
	var changes [][2]string
	for _, field := range []string{"name", "notes", "type"} {
		if _, present := r.PostForm[field]; !present {
			continue
		}
		v := strings.TrimSpace(r.PostForm.Get(field))
		if err := inventory.ValidateDeviceAttr(field, v); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if v != current[field] {
			changes = append(changes, [2]string{field, v})
		}
	}
	if len(changes) > 0 {
		at := s.opts.Clock.Now()
		err := s.opts.Ingester.Do(r.Context(), func(tx *sql.Tx) error {
			for _, c := range changes {
				if err := inventory.SetDeviceAttr(r.Context(), tx, d.IdentityKey, c[0], c[1], at); err != nil {
					return err
				}
				// A device that is no longer a router loses its router settings and password.
				if c[0] == "type" && c[1] != "router" {
					if err := store.DeleteRouter(r.Context(), tx, d.IdentityKey); err != nil {
						return err
					}
				}
			}
			return nil
		})
		if err != nil {
			s.serverError(w, r, err)
			return
		}
	}
	http.Redirect(w, r, "/devices/"+strconv.FormatInt(d.ID, 10)+"?saved=1", http.StatusSeeOther)
}

// ---------------------------------------------------------------- settings

type settingsData struct {
	Subnets           []store.SubnetInfo
	Skipped           []store.SkippedSubnet
	IntervalMinutes   int
	OfflineMultiplier int
	BuiltinEnabled    bool
}

func (s *Server) settingInt(r *http.Request, key string, def int) int {
	if v, ok, err := s.opts.Store.Setting(r.Context(), key); err == nil && ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	s.renderSettings(w, r, http.StatusOK, "")
}

func (s *Server) renderSettings(w http.ResponseWriter, r *http.Request, status int, errMsg string) {
	data := settingsData{
		IntervalMinutes:   s.settingInt(r, "builtin_interval_seconds", 900) / 60,
		OfflineMultiplier: s.settingInt(r, "offline_multiplier", inventory.DefaultOfflineMultiplier),
		BuiltinEnabled:    s.opts.Scanner != nil,
	}
	var err error
	if data.Subnets, err = s.opts.Store.SubnetStatus(r.Context(), s.opts.Clock.Now()); err != nil {
		s.serverError(w, r, err)
		return
	}
	if data.Skipped, err = s.opts.Store.SkippedSubnets(r.Context()); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, status, "settings.html", page{Title: "Settings", Nav: true, Active: "settings", Error: errMsg, Data: data})
}

func (s *Server) handleSettingsSave(w http.ResponseWriter, r *http.Request) {
	type field struct {
		form, key string
		min, max  int
		scale     int
		label     string
	}
	fields := []field{
		{"interval_minutes", "builtin_interval_seconds", 1, 1440, 60, "Scan interval must be 1–1440 minutes."},
		{"offline_multiplier", "offline_multiplier", 1, 10, 1, "Offline threshold must be 1–10 missed scans."},
	}
	for _, f := range fields {
		v := strings.TrimSpace(r.PostFormValue(f.form))
		if v == "" {
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < f.min || n > f.max {
			s.renderSettings(w, r, http.StatusBadRequest, f.label)
			return
		}
		if err := s.opts.Store.SetSetting(r.Context(), f.key, strconv.Itoa(n*f.scale)); err != nil {
			s.serverError(w, r, err)
			return
		}
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}
