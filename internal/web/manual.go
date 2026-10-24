package web

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/atotmakov/home_net_explorer/internal/inventory"
	"github.com/atotmakov/home_net_explorer/internal/store"
)

// Manual devices (feature 011, contracts/web-ui-changes.md): the owner adds equipment that scans
// cannot see.

type manualForm struct {
	Form  inventory.ManualDevice
	Types []string
}

func (s *Server) renderManualForm(w http.ResponseWriter, r *http.Request, status int, f inventory.ManualDevice, errMsg string) {
	s.render(w, r, status, "device_new.html", page{Title: "Add device", Nav: true, Active: "devices", Error: errMsg,
		Data: manualForm{Form: f, Types: inventory.DeviceTypes}})
}

func (s *Server) handleManualNew(w http.ResponseWriter, r *http.Request) {
	s.renderManualForm(w, r, http.StatusOK, inventory.ManualDevice{}, "")
}

// handleManualAdd creates a manual device and opens its page; refused input re-renders the form.
func (s *Server) handleManualAdd(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	f := inventory.ManualDevice{Name: r.PostForm.Get("name"), Type: r.PostForm.Get("type"),
		MAC: r.PostForm.Get("mac"), Notes: r.PostForm.Get("notes")}
	var id int64
	err := s.opts.Ingester.Do(r.Context(), func(tx *sql.Tx) error {
		key, err := inventory.AddManualDevice(r.Context(), tx, f, s.opts.Clock.Now())
		if err != nil {
			return err
		}
		id, err = store.DeviceIDTx(r.Context(), tx, key)
		return err
	})
	if msg, ok := ownerInputError(err); ok {
		s.renderManualForm(w, r, http.StatusBadRequest, f, msg)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/devices/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

// ownerInputError reports an error the owner can fix (shown on the form, 400).
func ownerInputError(err error) (string, bool) {
	var v inventory.ValidationError
	var inUse *inventory.ErrMACInUse
	switch {
	case errors.As(err, &v):
		return v.Error(), true
	case errors.As(err, &inUse):
		return inUse.Error(), true
	}
	return "", false
}
