package integration_test

import (
	"context"
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

// Feature 011, US3: edit and remove a manual device (FR-007, FR-008, FR-012).
func TestEditRemoveManualDevice(t *testing.T) {
	e := newEnv(t, envOpts{builtinScan: true})
	e.waitFor("initial scan", func() bool { return e.runCount() == 1 })
	e.waitFor("scanner idle", func() bool { return !e.app.Scanner.Status().Running })
	_, loc, _ := e.addManual(url.Values{"name": {"switch"}, "type": {"switch"}})
	if loc == "" {
		t.Fatal("add failed")
	}
	sw := strings.TrimPrefix(loc, "/devices/")
	nas := deviceID(t, e, "nas.lan")

	// Rename and retype through the usual form.
	if res, _ := e.post(loc+"/attrs", url.Values{"name": {"TL-SL1218P"}, "type": {"switch"}}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("rename = %d", res.StatusCode)
	}
	if !strings.Contains(e.get("/devices"), "TL-SL1218P") || !strings.Contains(e.get("/ui/map.json"), `"label":"TL-SL1218P"`) {
		t.Error("the new name is not on the Devices list and the Map")
	}

	// The MAC: set, refused when in use, cleared.
	page := e.get(loc)
	if !strings.Contains(page, `action="`+loc+`/mac"`) || !strings.Contains(page, `action="`+loc+`/remove"`) {
		t.Fatalf("manual device page lacks the MAC and Remove forms")
	}
	if res, _ := e.post(loc+"/mac", url.Values{"mac": {"00-00-5E-00-53-77"}}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("set MAC = %d", res.StatusCode)
	}
	if !strings.Contains(e.get(loc), "00:00:5e:00:53:77") {
		t.Error("MAC not shown after saving")
	}
	if res, body := e.post(loc+"/mac", url.Values{"mac": {"00:11:32:aa:bb:cc"}}); res.StatusCode != http.StatusBadRequest || !strings.Contains(body, "nas.lan") {
		t.Errorf("MAC in use = %d, want 400 naming nas.lan", res.StatusCode)
	}
	if res, _ := e.post(loc+"/mac", url.Values{"mac": {""}}); res.StatusCode != http.StatusSeeOther || strings.Contains(e.get(loc), "00:00:5e:00:53:77") {
		t.Error("clearing the MAC did not work")
	}

	// A scanned device has neither form, and its routes answer 404.
	if page := e.get("/devices/" + nas); strings.Contains(page, `action="/devices/`+nas+`/remove"`) || strings.Contains(page, `action="/devices/`+nas+`/mac"`) {
		t.Error("a scanned device's page has the MAC or Remove form")
	}
	for _, p := range []string{"/mac", "/remove"} {
		if res, _ := e.post("/devices/"+nas+p, url.Values{"mac": {""}, "confirm": {"remove"}}); res.StatusCode != http.StatusNotFound {
			t.Errorf("POST %s on a scanned device = %d, want 404", p, res.StatusCode)
		}
	}

	// The NAS connects via the switch; removing the switch needs the confirmation and drops the link.
	if res, _ := e.post("/devices/"+nas+"/link", url.Values{"via": {sw}}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("link = %d", res.StatusCode)
	}
	if res, _ := e.post(loc+"/remove", url.Values{"confirm": {"yes"}}); res.StatusCode != http.StatusBadRequest || !strings.Contains(e.get("/devices"), "TL-SL1218P") {
		t.Fatalf("remove without typing remove = %d, or the device went away", res.StatusCode)
	}
	if res, _ := e.post(loc+"/remove", url.Values{"confirm": {"remove"}}); res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/devices" {
		t.Fatalf("remove = %d to %q", res.StatusCode, res.Header.Get("Location"))
	}
	if strings.Contains(e.get("/devices"), "TL-SL1218P") || strings.Contains(e.get("/ui/map.json"), "TL-SL1218P") {
		t.Error("the removed device is still listed or on the Map")
	}
	nasPage := e.get("/devices/" + nas)
	if strings.Contains(nasPage, "TL-SL1218P") {
		t.Error("the NAS still connects via the removed device")
	}
	if strings.Contains(e.get("/ui/map.json"), `"kind":"manual"`) {
		t.Error("a manual edge is left on the Map")
	}
	if res, _ := e.req("GET", loc, nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("removed device page = %d, want 404", res.StatusCode)
	}
}

// FR-009: a link that would make a loop is refused with a message.
func TestLinkLoopRefused(t *testing.T) {
	e := newEnv(t, envOpts{builtinScan: true})
	e.waitFor("initial scan", func() bool { return e.runCount() == 1 })
	e.waitFor("scanner idle", func() bool { return !e.app.Scanner.Status().Running })
	_, loc, _ := e.addManual(url.Values{"name": {"TL-SL1218P"}, "type": {"switch"}})
	sw := strings.TrimPrefix(loc, "/devices/")
	nas := deviceID(t, e, "nas.lan")
	if res, _ := e.post("/devices/"+nas+"/link", url.Values{"via": {sw}}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("link = %d", res.StatusCode)
	}
	res, body := e.post(loc+"/link", url.Values{"via": {nas}})
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(body, "this would make a loop") {
		t.Errorf("loop link = %d %s, want 400 with the loop message", res.StatusCode, body)
	}
}

// SC-003: the manual device and its links survive later scans and a rebuild of the inventory.
func TestManualDeviceSurvives(t *testing.T) {
	e := newEnv(t, envOpts{builtinScan: true})
	e.waitFor("initial scan", func() bool { return e.runCount() == 1 })
	e.waitFor("scanner idle", func() bool { return !e.app.Scanner.Status().Running })
	_, loc, _ := e.addManual(url.Values{"name": {"TL-SL1218P"}, "type": {"switch"}})
	sw := strings.TrimPrefix(loc, "/devices/")
	nas := deviceID(t, e, "nas.lan")
	if res, _ := e.post("/devices/"+nas+"/link", url.Values{"via": {sw}}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("link = %d", res.StatusCode)
	}
	edgeTo := func() bool {
		m := e.mapDoc()
		for _, n := range m.Nodes {
			if n.Label == "TL-SL1218P" && n.State == "manual" {
				return m.has("manual", "dev-"+nas, n.ID)
			}
		}
		return false
	}
	if !edgeTo() {
		t.Fatal("no manual edge from the NAS to the switch")
	}
	for i := 0; i < 3; i++ {
		n := e.runCount()
		if res, _ := e.req("POST", "/scan", url.Values{}); res.StatusCode != http.StatusAccepted {
			t.Fatalf("POST /scan = %d", res.StatusCode)
		}
		e.waitFor("scan", func() bool { return e.runCount() == n+1 })
		e.waitFor("scanner idle", func() bool { return !e.app.Scanner.Status().Running })
	}
	if !edgeTo() || !strings.Contains(e.get(loc), "Added by hand") {
		t.Error("the switch or its link is gone after three scans")
	}
	if err := e.app.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !edgeTo() || !strings.Contains(e.get(loc), "TL-SL1218P") {
		t.Error("the switch or its link is gone after a rebuild")
	}
}
