package integration_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// Feature 011, US1: the owner adds a device that scans cannot see (contracts/web-ui-changes.md).

// addManual posts the Add device form and returns the status and the Location header.
func (e *env) addManual(form url.Values) (int, string, string) {
	e.t.Helper()
	res, body := e.post("/devices", form)
	return res.StatusCode, res.Header.Get("Location"), body
}

func TestAddManualDevice(t *testing.T) {
	e := newEnv(t, envOpts{builtinScan: true})
	e.waitFor("initial scan", func() bool { return e.runCount() == 1 })
	e.waitFor("scanner idle", func() bool { return !e.app.Scanner.Status().Running })

	form := e.get("/devices/new")
	for _, want := range []string{`action="/devices"`, `name="name"`, `name="type"`, `<option value="switch"`, `name="mac"`, `name="notes"`} {
		if !strings.Contains(form, want) {
			t.Errorf("Add device form lacks %s", want)
		}
	}
	if !strings.Contains(e.get("/devices"), `href="/devices/new"`) {
		t.Error("the Devices list has no Add device link")
	}

	// Refused: nothing saved, the form says why.
	for label, c := range map[string]struct {
		form url.Values
		want string
	}{
		"empty name": {url.Values{"name": {" "}, "type": {"switch"}}, "a name is required"},
		"bad MAC":    {url.Values{"name": {"x"}, "mac": {"zz"}}, "is not a MAC address"},
		"MAC in use": {url.Values{"name": {"x"}, "mac": {"00-11-32-AA-BB-CC"}}, "nas.lan"},
	} {
		code, _, body := e.addManual(c.form)
		if code != http.StatusBadRequest || !strings.Contains(body, c.want) || !strings.Contains(body, `name="name"`) {
			t.Errorf("%s: %d, want 400 with the form and %q", label, code, c.want)
		}
	}
	if code := e.crossSitePost("/devices", url.Values{"name": {"evil"}}); code != http.StatusForbidden {
		t.Errorf("cross-site add = %d, want 403", code)
	}
	if strings.Contains(e.get("/devices"), "evil") || strings.Contains(e.get("/devices"), "manual</span>") {
		t.Fatal("a refused add saved a device")
	}

	code, loc, body := e.addManual(url.Values{"name": {"TL-SL1218P"}, "type": {"switch"}, "notes": {"under the desk"}})
	if code != http.StatusSeeOther || !regexp.MustCompile(`^/devices/\d+$`).MatchString(loc) {
		t.Fatalf("add = %d to %q\n%s", code, loc, body)
	}
	page := e.get(loc)
	for _, want := range []string{"TL-SL1218P", "Added by hand", "under the desk"} {
		if !strings.Contains(page, want) {
			t.Errorf("device page lacks %q", want)
		}
	}
	list := e.get("/devices")
	row := list[strings.Index(list, "TL-SL1218P"):]
	row = row[:strings.Index(row, "</tr>")]
	if !strings.Contains(row, ">manual<") {
		t.Errorf("Devices row has no manual badge: %s", row)
	}
	if strings.Contains(e.get("/devices?status=online"), "TL-SL1218P") {
		t.Error("the online filter lists the manual device")
	}
	if home := e.get("/"); !strings.Contains(home, "1 added by hand") {
		t.Error("home page does not count the device added by hand")
	}
}
