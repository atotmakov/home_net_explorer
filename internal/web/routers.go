package web

import (
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/atotmakov/home_net_explorer/internal/collect/router"
	"github.com/atotmakov/home_net_explorer/internal/store"
)

// Router settings on the device page (feature 003). The stored password is never rendered:
// the view has no password field, and the form's password input is always empty.

const maxRouterField = 128

// routerData is the Router card of the device page.
type routerData struct {
	Settings   *store.RouterView // nil when not configured
	Models     []string
	Addresses  []store.RouterAddress // candidate addresses (current, private, not ignored)
	Address    store.RouterAddress   // the address collectors will use
	HasAddress bool
	Reads      []store.RouterRead // latest read per collector
	Error      string
}

func (s *Server) loadRouterData(r *http.Request, d store.DeviceRow) (*routerData, error) {
	if d.Type != "router" {
		return nil, nil
	}
	ctx := r.Context()
	rd := &routerData{Models: router.Models()}
	v, ok, err := s.opts.Store.GetRouterSettings(ctx, d.IdentityKey)
	if err != nil {
		return nil, err
	}
	preferred := ""
	if ok {
		rd.Settings, preferred = &v, v.Subnet
	}
	if rd.Addresses, err = s.opts.Store.RouterAddresses(ctx, d.IdentityKey); err != nil {
		return nil, err
	}
	rd.Address, rd.HasAddress = store.PickRouterAddress(rd.Addresses, preferred)
	if rd.HasAddress {
		if rd.Reads, err = s.opts.Store.RouterStatus(ctx, rd.Address.IP); err != nil {
			return nil, err
		}
	}
	return rd, nil
}

// handleRouterSave stores the router settings of a device of type "router".
func (s *Server) handleRouterSave(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDevice(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	rd, err := s.loadRouterData(r, d)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	fail := func(msg string) {
		if rd == nil {
			http.Error(w, msg, http.StatusBadRequest)
			return
		}
		rd.Error = msg
		s.renderDevice(w, r, http.StatusBadRequest, d, rd)
	}
	settings := store.RouterSettings{
		Model:    r.PostForm.Get("model"),
		Subnet:   strings.TrimSpace(r.PostForm.Get("subnet")),
		Username: strings.TrimSpace(r.PostForm.Get("username")),
		Password: r.PostForm.Get("password"),
	}
	switch {
	case rd == nil:
		fail("Set the device type to router first.")
		return
	case !slices.Contains(router.Models(), settings.Model):
		fail("Choose one of the supported router models.")
		return
	case settings.Username == "" || len(settings.Username) > maxRouterField:
		fail("The username must be 1 to 128 characters.")
		return
	case len(settings.Password) > maxRouterField:
		fail("The password must be at most 128 characters.")
		return
	case !rd.HasAddress:
		fail("This device has no current private address, so it can't be used as a router yet.")
		return
	case settings.Subnet != "" && !slices.ContainsFunc(rd.Addresses, func(a store.RouterAddress) bool { return a.CIDR == settings.Subnet }):
		fail("Choose one of the device's current subnets.")
		return
	}
	err = s.opts.Ingester.Do(r.Context(), func(tx *sql.Tx) error {
		return store.SaveRouter(r.Context(), tx, d.IdentityKey, settings, s.opts.Clock.Now())
	})
	if errors.Is(err, store.ErrNoPassword) {
		fail("Enter the router's password.")
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/devices/"+strconv.FormatInt(d.ID, 10)+"?saved=1#router", http.StatusSeeOther)
}

// handleRouterRemove deletes a device's router settings, including the stored password.
func (s *Server) handleRouterRemove(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDevice(w, r)
	if !ok {
		return
	}
	if err := s.opts.Ingester.Do(r.Context(), func(tx *sql.Tx) error {
		return store.DeleteRouter(r.Context(), tx, d.IdentityKey)
	}); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/devices/"+strconv.FormatInt(d.ID, 10)+"#router", http.StatusSeeOther)
}
