package web

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/store"
)

// ---------------------------------------------------------------- maintenance (feature 004)

// confirmed reports whether the owner typed the action's confirmation word (FR-003).
func confirmed(r *http.Request, word string) bool {
	return strings.EqualFold(strings.TrimSpace(r.PostFormValue("confirm")), word)
}

// maintenanceNotice is the message shown after an action. It is built only from a known action
// and integer counts, never from the query text itself.
func maintenanceNotice(q url.Values) string {
	n := func(key string) (int, bool) {
		v, err := strconv.Atoi(q.Get(key))
		return v, err == nil && v >= 0
	}
	switch q.Get("done") {
	case "devices":
		d, ok1 := n("devices")
		r, ok2 := n("runs")
		if ok1 && ok2 {
			return fmt.Sprintf("Removed %d devices and %d scan records.", d, r)
		}
	case "collectors":
		if c, ok := n("collectors"); ok {
			return fmt.Sprintf("Removed %d collectors.", c)
		}
	case "everything":
		d, ok1 := n("devices")
		r, ok2 := n("runs")
		c, ok3 := n("collectors")
		if ok1 && ok2 && ok3 {
			return fmt.Sprintf("Dropped all data: %d devices, %d scan records, %d collectors.", d, r, c)
		}
	case "paused":
		return "Built-in scanner paused."
	case "resumed":
		return "Built-in scanner resumed."
	}
	return ""
}

// removal runs one destructive action: confirmation word, one transaction serialized with
// ingest (all or nothing, FR-004), a log line and a redirect to the notice (FR-005, FR-006).
func (s *Server) removal(w http.ResponseWriter, r *http.Request, action, word string,
	f func(ctx context.Context, tx *sql.Tx, at time.Time) (store.RemovalCounts, error), after func(ctx context.Context) error) {
	if !confirmed(r, word) {
		s.renderSettings(w, r, http.StatusBadRequest, "Type "+word+" to confirm. Nothing was removed.")
		return
	}
	var c store.RemovalCounts
	at := s.opts.Clock.Now()
	if err := s.opts.Ingester.Do(r.Context(), func(tx *sql.Tx) error {
		var err error
		c, err = f(r.Context(), tx, at)
		return err
	}); err != nil {
		s.serverError(w, r, err)
		return
	}
	if after != nil {
		if err := after(r.Context()); err != nil {
			s.serverError(w, r, err)
			return
		}
	}
	s.opts.Log.Info("maintenance", "action", action, "devices", c.Devices, "runs", c.Runs, "collectors", c.Collectors)
	q := url.Values{"done": {action}}
	switch action {
	case "devices":
		q.Set("devices", strconv.Itoa(c.Devices))
		q.Set("runs", strconv.Itoa(c.Runs))
	case "collectors":
		q.Set("collectors", strconv.Itoa(c.Collectors))
	case "everything":
		q.Set("devices", strconv.Itoa(c.Devices))
		q.Set("runs", strconv.Itoa(c.Runs))
		q.Set("collectors", strconv.Itoa(c.Collectors))
	}
	http.Redirect(w, r, "/settings?"+encodeOrdered(q, "done", "devices", "runs", "collectors"), http.StatusSeeOther)
}

// encodeOrdered encodes q with keys in the given order (url.Values.Encode sorts them).
func encodeOrdered(q url.Values, keys ...string) string {
	var parts []string
	for _, k := range keys {
		if v := q.Get(k); v != "" {
			parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
		}
	}
	return strings.Join(parts, "&")
}

func (s *Server) handleRemoveDevices(w http.ResponseWriter, r *http.Request) {
	s.removal(w, r, "devices", "devices", store.RemoveAllDevices, nil)
}

func (s *Server) handleRemoveCollectors(w http.ResponseWriter, r *http.Request) {
	s.removal(w, r, "collectors", "collectors", store.RemoveAllCollectors, nil)
}

func (s *Server) handleDropAll(w http.ResponseWriter, r *http.Request) {
	drop := func(ctx context.Context, tx *sql.Tx, at time.Time) (store.RemovalCounts, error) {
		return store.DropAllData(ctx, tx, at, s.opts.SettingDefaults)
	}
	var wake func(ctx context.Context) error
	if s.opts.Scanner != nil {
		// The pause setting is gone; tell the scan loop so it runs again at once.
		wake = func(ctx context.Context) error { return s.opts.Scanner.SetPaused(ctx, false) }
	}
	s.removal(w, r, "everything", "everything", drop, wake)
}

func (s *Server) handlePause(w http.ResponseWriter, r *http.Request)  { s.setPaused(w, r, true) }
func (s *Server) handleResume(w http.ResponseWriter, r *http.Request) { s.setPaused(w, r, false) }

func (s *Server) setPaused(w http.ResponseWriter, r *http.Request, pause bool) {
	if s.opts.Scanner == nil {
		http.NotFound(w, r)
		return
	}
	if err := s.opts.Scanner.SetPaused(r.Context(), pause); err != nil {
		s.serverError(w, r, err)
		return
	}
	action, done := "resume", "resumed"
	if pause {
		action, done = "pause", "paused"
	}
	s.opts.Log.Info("maintenance", "action", action)
	http.Redirect(w, r, "/settings?done="+done, http.StatusSeeOther)
}
