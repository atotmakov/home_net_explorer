package web

import (
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/atotmakov/home_net_explorer/internal/contract"
	"github.com/atotmakov/home_net_explorer/internal/store"
)

// Router settings on the device page (feature 003), and access point settings, which follow the
// same rules and share the settings table (feature 012, research R4). The stored password is
// never rendered: the view has no password field, and the form's password input is always empty.

const maxRouterField = 128

// sourceKind is what a settings card configures: a router or an access point.
type sourceKind struct {
	Type   string // device type that offers the card
	Title  string // card heading
	Noun   string // in messages and buttons
	Anchor string // card id and URL path segment
	Models []string
}

var (
	routerKind      = sourceKind{Type: "router", Title: "Router", Noun: "router", Anchor: "router", Models: contract.RouterModels}
	accessPointKind = sourceKind{Type: "access_point", Title: "Access point", Noun: "access point", Anchor: "access-point", Models: contract.AccessPointModels}
)

// routerData is the Router or Access point card of the device page.
type routerData struct {
	Kind       sourceKind
	Settings   *store.RouterView // nil when not configured
	Models     []string
	Addresses  []store.RouterAddress // candidate addresses (current, private, not ignored)
	Address    store.RouterAddress   // the address collectors will use
	HasAddress bool
	Reads      []store.RouterRead // latest read per collector
	Error      string
}

func (s *Server) loadRouterData(r *http.Request, d store.DeviceRow) (*routerData, error) {
	var kind sourceKind
	switch d.Type {
	case routerKind.Type:
		kind = routerKind
	case accessPointKind.Type:
		kind = accessPointKind
	default:
		return nil, nil
	}
	ctx := r.Context()
	rd := &routerData{Kind: kind, Models: kind.Models}
	v, ok, err := s.opts.Store.GetRouterSettings(ctx, d.IdentityKey)
	if err != nil {
		return nil, err
	}
	preferred := ""
	if ok && slices.Contains(kind.Models, v.Model) {
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
	s.saveSource(w, r, routerKind)
}

// handleAccessPointSave stores the access point settings of a device of type "access_point".
func (s *Server) handleAccessPointSave(w http.ResponseWriter, r *http.Request) {
	s.saveSource(w, r, accessPointKind)
}

func (s *Server) saveSource(w http.ResponseWriter, r *http.Request, kind sourceKind) {
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
		fail("Set the device type to " + kind.Noun + " first.")
		return
	case rd.Kind.Type != kind.Type:
		fail("This device is not a " + kind.Noun + ": change its type first.")
		return
	case !slices.Contains(kind.Models, settings.Model):
		fail("Choose one of the supported " + kind.Noun + " models.")
		return
	case settings.Username == "" || len(settings.Username) > maxRouterField:
		fail("The username must be 1 to 128 characters.")
		return
	case len(settings.Password) > maxRouterField:
		fail("The password must be at most 128 characters.")
		return
	case !rd.HasAddress:
		fail("This device has no current private address, so it can't be used as a " + kind.Noun + " yet.")
		return
	case settings.Subnet != "" && !slices.ContainsFunc(rd.Addresses, func(a store.RouterAddress) bool { return a.CIDR == settings.Subnet }):
		fail("Choose one of the device's current subnets.")
		return
	}
	err = s.opts.Ingester.Do(r.Context(), func(tx *sql.Tx) error {
		return store.SaveRouter(r.Context(), tx, d.IdentityKey, settings, s.opts.Clock.Now())
	})
	if errors.Is(err, store.ErrNoPassword) {
		fail("Enter the " + kind.Noun + "'s password.")
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/devices/"+strconv.FormatInt(d.ID, 10)+"?saved=1#"+kind.Anchor, http.StatusSeeOther)
}

// handleRouterRemove deletes a device's router or access point settings, including the stored
// password (one settings row per device, whichever card posted).
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
	anchor := routerKind.Anchor
	if d.Type == accessPointKind.Type {
		anchor = accessPointKind.Anchor
	}
	http.Redirect(w, r, "/devices/"+strconv.FormatInt(d.ID, 10)+"#"+anchor, http.StatusSeeOther)
}
