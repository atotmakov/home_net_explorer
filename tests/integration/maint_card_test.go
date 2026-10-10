package integration_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract/contracttest"
)

// Feature 004: the Maintenance card on the Settings page and helpers shared by the maint_ tests.

// anonPost posts without the owner's session.
func (e *env) anonPost(path string, form url.Values) int {
	e.t.Helper()
	r, _ := http.NewRequest("POST", e.srv.URL+path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", e.srv.URL)
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := c.Do(r)
	if err != nil {
		e.t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}

// crossSitePost posts with the owner's session from another site.
func (e *env) crossSitePost(path string, form url.Values) int {
	e.t.Helper()
	r, _ := http.NewRequest("POST", e.srv.URL+path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "http://evil.example")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	res, err := e.http.Do(r)
	if err != nil {
		e.t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}

// shifted is a fixture with a new collection_id and every time moved by d.
func shifted(t *testing.T, fixture, id string, d time.Duration) []byte {
	t.Helper()
	move := func(v any) any {
		s, ok := v.(string)
		if !ok {
			return v
		}
		tm, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			t.Fatalf("fixture time %q: %v", s, err)
		}
		return tm.Add(d).UTC().Format("2006-01-02T15:04:05.000Z")
	}
	return contracttest.Modify(t, fixture, func(m map[string]any) {
		m["collection_id"] = id
		for _, k := range []string{"started_at", "finished_at", "sent_at"} {
			m[k] = move(m[k])
		}
		if obs, ok := m["observations"].([]any); ok {
			for _, o := range obs {
				om := o.(map[string]any)
				om["observed_at"] = move(om["observed_at"])
			}
		}
	})
}

func TestMaintenanceCard(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.login()
	if body := e.get("/settings"); !strings.Contains(body, `id="maintenance"`) {
		t.Fatal("no Maintenance card on the Settings page")
	}
	for q, want := range map[string]string{
		"done=devices&devices=4&runs=7":                 "Removed 4 devices and 7 scan records.",
		"done=collectors&collectors=2":                  "Removed 2 collectors.",
		"done=everything&devices=1&runs=2&collectors=3": "Dropped all data: 1 devices, 2 scan records, 3 collectors.",
		"done=paused":  "Built-in scanner paused.",
		"done=resumed": "Built-in scanner resumed.",
		"done=devices&devices=4&runs=7&extra=%3Cscript%3E": "Removed 4 devices and 7 scan records.",
	} {
		if body := e.get("/settings?" + q); !strings.Contains(body, want) {
			t.Errorf("?%s: notice %q missing", q, want)
		}
	}
	for _, q := range []string{"done=%3Cscript%3Ealert(1)%3C/script%3E", "done=devices&devices=x&runs=1"} {
		body := e.get("/settings?" + q)
		if strings.Contains(body, "Removed") || strings.Contains(body, "<script>alert") {
			t.Errorf("?%s: a notice was shown or the value echoed", q)
		}
	}
}
