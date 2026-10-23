package web

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/atotmakov/home_net_explorer/internal/store"
)

// ---------------------------------------------------------------- map (feature 010)

type mapData struct {
	Empty bool // no devices yet
}

// handleMap renders the Map page; the graph is drawn by static/map.js from /ui/map.json.
func (s *Server) handleMap(w http.ResponseWriter, r *http.Request) {
	c, err := s.opts.Store.HomeCounts(r.Context(), s.opts.Clock.Now())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "map.html", page{Title: "Map", Nav: true, Active: "map", Data: mapData{Empty: c.Total == 0}})
}

// handleMapJSON serves the topology (contracts/web-ui-changes.md). Query parameters are ignored:
// the page filters client-side.
func (s *Server) handleMapJSON(w http.ResponseWriter, r *http.Request) {
	tp, err := s.opts.Store.Topology(r.Context(), s.opts.Clock.Now())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, tp)
}

// handleDeviceLink sets (via=<device id>) or clears (via empty) the device's manual
// "connected via" link, as an append-only user fact (research R7).
func (s *Server) handleDeviceLink(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDevice(w, r)
	if !ok {
		return
	}
	via := strings.TrimSpace(r.PostFormValue("via"))
	var target store.DeviceRow
	if via != "" {
		id, err := strconv.ParseInt(via, 10, 64)
		if err != nil || id == d.ID {
			http.Error(w, "choose another device", http.StatusBadRequest)
			return
		}
		target, err = s.opts.Store.GetDevice(r.Context(), id, s.opts.Clock.Now())
		if errors.Is(err, store.ErrNotFound) || (err == nil && target.Status == "merged_away") {
			http.Error(w, "unknown device", http.StatusBadRequest)
			return
		}
		if err != nil {
			s.serverError(w, r, err)
			return
		}
	}
	at := s.opts.Clock.Now()
	err := s.opts.Ingester.Do(r.Context(), func(tx *sql.Tx) error {
		if via == "" {
			return store.ClearUserLink(r.Context(), tx, d.IdentityKey, at)
		}
		return store.SetUserLink(r.Context(), tx, d.IdentityKey, target.IdentityKey, at)
	})
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	back := "/devices/" + strconv.FormatInt(d.ID, 10)
	if ref, err := url.Parse(r.Referer()); err == nil && ref.Host == r.Host && ref.Path != "" &&
		(ref.Scheme == "http" || ref.Scheme == "https") {
		back = ref.Path
		if ref.RawQuery != "" {
			back += "?" + ref.RawQuery
		}
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}
