// Package eaptest is a fake TP-Link EAP225 web interface (standalone mode) for tests
// (contracts/access-point-eap225.md). It is imported only by tests.
package eaptest

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Firmware selects the client list endpoint and the session cookie, as on the owner's units.
type Firmware int

const (
	New Firmware = iota // /data/status.client.user.json, JSESSIONID (192.168.0.5)
	Old                 // /data/monitor.client.client.json, COOKIE= (192.168.0.10)
)

// PathNew and PathOld are the client list endpoints of the two firmware generations.
const (
	PathNew = "/data/status.client.user.json"
	PathOld = "/data/monitor.client.client.json"
)

// Entry is one client as the access point lists it. Keys holds the JSON object as sent, so tests
// can use the real key names (research R2).
type Entry map[string]any

// Client returns an entry with the keys the fixtures assume.
func Client(mac, ip, ssid string, radio int, hostname string) Entry {
	e := Entry{"MAC": mac, "SSID": ssid, "Radio": radio}
	if ip != "" {
		e["IP"] = ip
	}
	if hostname != "" {
		e["hostname"] = hostname
	}
	return e
}

// OldClient returns an entry as the older firmware lists it (seen on the owner's 192.168.0.10,
// 2026-10-24): the name under DeviceName and Radio as text such as "2.4GHz".
func OldClient(mac, ip, ssid, radio, name string) Entry {
	e := Entry{"MAC": mac, "SSID": ssid, "Radio": radio, "AccessPoint": "EAP225", "CCQ": 90}
	if ip != "" {
		e["IP"] = ip
	}
	if name != "" {
		e["DeviceName"] = name
	}
	return e
}

// Request is one request the fake received.
type Request struct {
	Method, Path string
	Login        bool // a login attempt (credentials in the body)
	Session      bool // carried a valid session cookie
}

// AP is a fake access point.
type AP struct {
	Srv      *httptest.Server
	Firmware Firmware
	Username string
	Password string

	mu        sync.Mutex
	managed   bool
	locked    bool
	postOnly  bool   // the list answers only POST operation=load, as the web page asks
	resetOn   string // "METHOD /path": that request gets its connection closed without an answer
	otherPath int    // status of the other firmware's list path when logged in (0: 404)
	clients   []Entry
	list      []byte // raw list body override
	delay     time.Duration
	session   string
	lastError int // login.json "error" after the last attempt
	requests  []Request
	logouts   int
}

// Start runs a fake access point that accepts username/password.
func Start(t testing.TB, fw Firmware, username, password string) *AP {
	t.Helper()
	ap := &AP{Firmware: fw, Username: username, Password: password}
	ap.Srv = httptest.NewServer(http.HandlerFunc(ap.serve))
	t.Cleanup(ap.Srv.Close)
	return ap
}

// Transport sends every request to the fake, whatever address the reader uses (so tests can
// configure a private address such as the owner's 192.168.0.5).
func (ap *AP) Transport() http.RoundTripper {
	addr := ap.Srv.Listener.Addr().String()
	return &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", addr)
	}}
}

// SetClients sets the client list.
func (ap *AP) SetClients(c ...Entry) { ap.mu.Lock(); ap.clients = c; ap.list = nil; ap.mu.Unlock() }

// SetList replaces the list answer with a raw body (for garbage pages).
func (ap *AP) SetList(b []byte) { ap.mu.Lock(); ap.list = b; ap.mu.Unlock() }

// SetManaged makes the access point report that a controller manages it.
func (ap *AP) SetManaged(m bool) { ap.mu.Lock(); ap.managed = m; ap.mu.Unlock() }

// SetPostOnly makes the list answer only a POST with operation=load (a GET gets no list).
func (ap *AP) SetPostOnly(p bool) { ap.mu.Lock(); ap.postOnly = p; ap.mu.Unlock() }

// SetResetOn closes the connection without an answer on requests with this method and path.
func (ap *AP) SetResetOn(method, path string) {
	ap.mu.Lock()
	ap.resetOn = method + " " + path
	ap.mu.Unlock()
}

// SetOtherPath sets the status the other firmware generation's list path answers once logged
// in (the owner's older unit answers something other than 404 there; a POST to it drops the
// connection: gate G1, run 4).
func (ap *AP) SetOtherPath(status int) { ap.mu.Lock(); ap.otherPath = status; ap.mu.Unlock() }

// SetLocked makes every login fail with a lockout.
func (ap *AP) SetLocked(l bool) { ap.mu.Lock(); ap.locked = l; ap.mu.Unlock() }

// SetDelay delays every answer.
func (ap *AP) SetDelay(d time.Duration) { ap.mu.Lock(); ap.delay = d; ap.mu.Unlock() }

// Requests returns the requests received so far.
func (ap *AP) Requests() []Request {
	ap.mu.Lock()
	defer ap.mu.Unlock()
	return append([]Request(nil), ap.requests...)
}

// Logins counts login attempts.
func (ap *AP) Logins() int {
	n := 0
	for _, r := range ap.Requests() {
		if r.Login {
			n++
		}
	}
	return n
}

// Logouts counts logout requests made with a valid session.
func (ap *AP) Logouts() int { ap.mu.Lock(); defer ap.mu.Unlock(); return ap.logouts }

func (ap *AP) cookieName() string {
	if ap.Firmware == Old {
		return "COOKIE"
	}
	return "JSESSIONID"
}

func (ap *AP) hasSession(r *http.Request) bool {
	c, err := r.Cookie(ap.cookieName())
	return err == nil && ap.session != "" && c.Value == ap.session
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (ap *AP) serve(w http.ResponseWriter, r *http.Request) {
	ap.mu.Lock()
	delay := ap.delay
	ap.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	raw, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(raw))
	r.ParseForm()
	ap.mu.Lock()
	defer ap.mu.Unlock()
	req := Request{Method: r.Method, Path: r.URL.Path, Session: ap.hasSession(r)}
	login := r.URL.Path == "/" && r.Method == http.MethodPost && r.PostForm.Get("username") != ""
	req.Login = login
	ap.requests = append(ap.requests, req)
	if ap.resetOn == r.Method+" "+r.URL.Path {
		if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
			conn.Close()
		}
		return
	}

	notLoggedIn := map[string]any{"success": true, "timeout": true, "version": "1.00", "mode": "accessPoint", "status": -1}
	switch {
	case r.URL.Path == "/data/managedMode.json":
		if ap.managed {
			writeJSON(w, map[string]any{"success": false, "timeout": false, "errCode": 403})
			return
		}
		writeJSON(w, map[string]any{"success": true, "timeout": false, "errCode": 0})
	case r.URL.Path == "/" && r.Method == http.MethodGet:
		if ap.Firmware == Old {
			http.SetCookie(w, &http.Cookie{Name: "COOKIE", Value: "c0a8000a00000001", Path: "/"})
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<html><head><title>Login</title></head><body id=\"login-body\"></body></html>"))
	case login && ap.Firmware == Old && !strings.HasPrefix(string(raw), "username="):
		// The older firmware reads the fields in the order its page sends them; any other order
		// is silently not a login (no error recorded, no session): the owner's unit, gate G1.
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html></html>"))
	case login:
		sum := md5.Sum([]byte(ap.Password))
		want := strings.ToUpper(hex.EncodeToString(sum[:]))
		if ap.locked || r.PostForm.Get("username") != ap.Username || r.PostForm.Get("password") != want {
			ap.lastError = 1
			if ap.locked {
				ap.lastError = 3
			}
			writeJSON(w, map[string]any{"result": false})
			return
		}
		ap.lastError = 0
		ap.session = "s" + time.Now().Format("150405.000000000")
		http.SetCookie(w, &http.Cookie{Name: ap.cookieName(), Value: ap.session, Path: "/", HttpOnly: true})
		writeJSON(w, map[string]any{"result": true})
	case r.URL.Path == "/data/login.json":
		state := map[string]any{"error": ap.lastError, "dutMode": "EAP225"}
		if ap.Firmware == New {
			left := 30
			if ap.locked {
				left = 0
			}
			state["retryChances"] = left
			if ap.lastError == 3 {
				state["waitTime"] = 600
			}
		} else {
			state["error"] = map[int]string{0: "0", 1: "1", 3: "3"}[ap.lastError] // the old firmware sends strings
		}
		writeJSON(w, state)
	case r.URL.Path == "/data/logout.json":
		if req.Session {
			ap.logouts++
			ap.session = ""
		}
		writeJSON(w, map[string]any{"success": true})
	case (ap.Firmware == New && r.URL.Path == PathNew) || (ap.Firmware == Old && r.URL.Path == PathOld):
		if !req.Session {
			writeJSON(w, notLoggedIn)
			return
		}
		if ap.postOnly && (r.Method != http.MethodPost || r.PostForm.Get("operation") != "load") {
			writeJSON(w, map[string]any{"success": false, "timeout": false}) // logged in, but no list
			return
		}
		if ap.list != nil {
			w.Write(ap.list)
			return
		}
		clients := ap.clients
		if clients == nil {
			clients = []Entry{}
		}
		if ap.Firmware == Old { // as the owner's unit answers: timeout as text, and an error code
			writeJSON(w, map[string]any{"success": true, "timeout": "false", "error": 0, "data": clients})
			return
		}
		writeJSON(w, map[string]any{"success": true, "timeout": false, "data": clients})
	case ap.otherPath != 0 && req.Session && (r.URL.Path == PathNew || r.URL.Path == PathOld):
		if r.Method == http.MethodPost {
			if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
				conn.Close()
			}
			return
		}
		if ap.otherPath == http.StatusFound {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		w.WriteHeader(ap.otherPath)
	default:
		http.NotFound(w, r)
	}
}
