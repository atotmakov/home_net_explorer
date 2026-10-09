// Package routertest provides a fake Huawei HG8145V5 web interface (contracts/router-hg8145v5.md)
// so no test ever talks to a real router (Constitution II). It is imported only by tests.
package routertest

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// Mode scripts how the fake router behaves.
type Mode int

const (
	OK            Mode = iota // normal login, list and logout
	WrongPassword             // every login is rejected (FailStat = 1)
	Locked                    // logins are refused while LockLeftTime > 0
	SessionBusy               // someone else is logged in
	GarbledList               // the device list page is not the expected JavaScript
	NoOntToken                // index.asp carries no onttoken, so no logout is possible
	Hang                      // GetRandCount.asp never answers
)

// FixturePath is the anonymized capture served as the device list.
func FixturePath() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "testdata", "hg8145v5_getlanuserdevinfo.asp")
}

// Fake is an httptest server that implements the five requests the collector may send, and
// fails the test on any other request (FR-004, read-only access).
type Fake struct {
	t          testing.TB
	srv        *httptest.Server
	user, pass string
	release    chan struct{}

	mu        sync.Mutex
	mode      Mode
	list      []byte
	randToken string
	sid       string
	ontToken  string
	requests  []string
	logins    int
	logouts   int
	creds     []string // "user:password" of each login attempt
}

// New starts a fake router that accepts user/pass. It is closed when the test ends.
func New(t testing.TB, user, pass string) *Fake {
	t.Helper()
	list, err := os.ReadFile(FixturePath())
	if err != nil {
		t.Fatal(err)
	}
	f := &Fake{t: t, user: user, pass: pass, list: list, release: make(chan struct{})}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	t.Cleanup(func() { close(f.release) }) // runs first: unblocks hanging handlers
	return f
}

// URL is the fake's real (loopback) base URL.
func (f *Fake) URL() string { return f.srv.URL }

// Transport sends every request, whatever its host, to the fake. Configs keep a private
// router address (FR-005) and tests plug this in as the router's RoundTripper.
func (f *Fake) Transport() http.RoundTripper { return Redirect(f.srv.URL) }

// SetMode changes the fake's behavior.
func (f *Fake) SetMode(m Mode) {
	f.mu.Lock()
	f.mode = m
	f.mu.Unlock()
}

// SetList replaces the device list page.
func (f *Fake) SetList(b []byte) {
	f.mu.Lock()
	f.list = b
	f.mu.Unlock()
}

// Requests returns "METHOD /path" for every request received, in order.
func (f *Fake) Requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

// Logins is the number of login attempts.
func (f *Fake) Logins() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.logins
}

// Logouts is the number of accepted logouts.
func (f *Fake) Logouts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.logouts
}

// Credentials returns "user:password" for each login attempt.
func (f *Fake) Credentials() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.creds...)
}

// SessionOpen reports whether a session is still logged in.
func (f *Fake) SessionOpen() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sid != ""
}

func (f *Fake) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	mode := f.mode
	f.mu.Unlock()

	switch r.Method + " " + r.URL.Path {
	case "POST /asp/GetRandCount.asp":
		if mode == Hang {
			select {
			case <-r.Context().Done():
			case <-f.release:
			}
			return
		}
		tok := randomHex(24)
		f.mu.Lock()
		f.randToken = tok
		f.mu.Unlock()
		fmt.Fprint(w, "﻿"+tok)
	case "POST /login.cgi":
		f.login(w, r, mode)
	case "GET /html/bbsp/common/GetLanUserDevInfo.asp":
		if !f.authorized(r) {
			fmt.Fprint(w, redirectPage("/"))
			return
		}
		f.mu.Lock()
		list := f.list
		f.mu.Unlock()
		if mode == GarbledList {
			fmt.Fprint(w, loginPage(0, 0, 0))
			return
		}
		w.Write(list)
	case "GET /index.asp":
		if !f.authorized(r) {
			fmt.Fprint(w, redirectPage("/"))
			return
		}
		if mode == NoOntToken {
			fmt.Fprint(w, "<html><body>main page</body></html>")
			return
		}
		tok := randomHex(24)
		f.mu.Lock()
		f.ontToken = tok
		f.mu.Unlock()
		fmt.Fprintf(w, `<html><body><input type="hidden" name="onttoken" id="onttoken" value="%s"></body></html>`, tok)
	case "POST /logout.cgi":
		r.ParseForm()
		f.mu.Lock()
		if r.URL.Query().Get("RequestFile") == "html/logout.html" && f.ontToken != "" &&
			r.PostForm.Get("x.X_HW_Token") == f.ontToken && f.authorizedLocked(r) {
			f.sid, f.ontToken = "", ""
			f.logouts++
		}
		f.mu.Unlock()
		fmt.Fprint(w, redirectPage("/"))
	case "GET /":
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case mode == Locked:
			fmt.Fprint(w, loginPage(1, 3, 57))
		case mode == WrongPassword && f.logins > 0:
			fmt.Fprint(w, loginPage(1, f.logins, 0))
		default:
			fmt.Fprint(w, loginPage(0, 0, 0))
		}
	default:
		f.t.Errorf("router fake: unexpected request %s %s (read-only access allows only the documented requests)", r.Method, r.URL)
		http.NotFound(w, r)
	}
}

func (f *Fake) login(w http.ResponseWriter, r *http.Request, mode Mode) {
	r.ParseForm()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logins++
	pass, _ := base64.StdEncoding.DecodeString(r.PostForm.Get("PassWord"))
	f.creds = append(f.creds, r.PostForm.Get("UserName")+":"+string(pass))
	if c, err := r.Cookie("Cookie"); err != nil || c.Value != "body:Language:english:id=-1" {
		f.t.Errorf("router fake: login without the pre-login cookie (got %v)", r.Header.Get("Cookie"))
	}
	tok := r.PostForm.Get("x.X_HW_Token")
	if tok == "" || tok != f.randToken {
		f.t.Errorf("router fake: login with a stale or missing x.X_HW_Token")
	}
	f.randToken = ""
	if r.PostForm.Get("Language") != "english" {
		f.t.Errorf("router fake: login without Language=english")
	}
	switch {
	case mode == SessionBusy || f.sid != "":
		fmt.Fprint(w, `<html><script>alert("The user has already logged in.");top.location.replace('/');</script></html>`)
	case mode == Locked, mode == WrongPassword, r.PostForm.Get("UserName") != f.user, string(pass) != f.pass:
		fmt.Fprint(w, redirectPage("/"))
	default:
		f.sid = randomHex(32)
		w.Header().Set("Set-Cookie", "Cookie=sid="+f.sid+":Language:english:id=1;path=/")
		fmt.Fprint(w, redirectPage("index.asp"))
	}
}

func (f *Fake) authorized(r *http.Request) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.authorizedLocked(r)
}

func (f *Fake) authorizedLocked(r *http.Request) bool {
	c, err := r.Cookie("Cookie")
	return err == nil && f.sid != "" && strings.HasPrefix(c.Value, "sid="+f.sid+":")
}

func redirectPage(page string) string {
	return `<!DOCTYPE html><html><head><title>Waiting...</title><script type="text/javascript">
var pageName = '` + page + `';
top.location.replace(pageName);
</script></head><body></body></html>`
}

func loginPage(failStat, loginTimes, lockLeft int) string {
	return fmt.Sprintf(`<html><head><script type="text/javascript">
var FailStat ='%d';
var LoginTimes = '%d';
var LockLeftTime = '%d';
var errloginlockNum = '3';
</script></head><body><form id="loginform"></form></body></html>`, failStat, loginTimes, lockLeft)
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Redirect returns a RoundTripper that sends every request to target (an http:// base URL),
// keeping the path, query and Host header of the original request.
func Redirect(target string) http.RoundTripper {
	u, err := url.Parse(target)
	if err != nil {
		panic(err)
	}
	return &redirect{to: u, rt: &http.Transport{}}
}

type redirect struct {
	to *url.URL
	rt http.RoundTripper
}

func (r *redirect) RoundTrip(req *http.Request) (*http.Response, error) {
	out := req.Clone(req.Context())
	out.URL.Scheme, out.URL.Host = r.to.Scheme, r.to.Host
	return r.rt.RoundTrip(out)
}
