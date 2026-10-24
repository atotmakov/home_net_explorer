package accesspoint

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// The EAP225 web interface as its own login page uses it (contracts/access-point-eap225.md).
// Only these requests are ever sent (FR-004, read-only).
const (
	pathManaged   = "/data/managedMode.json"
	pathLoginPage = "/"
	pathLoginData = "/data/login.json"
	pathLogout    = "/data/logout.json"

	maxBody       = 1 << 20 // larger answers are not a client list
	logoutTimeout = 5 * time.Second
)

// The client list endpoints, newer firmware first (research R1).
var listPaths = []struct{ path, name string }{
	{"/data/status.client.user.json", "status.client.user"},
	{"/data/monitor.client.client.json", "monitor.client.client"},
}

var (
	errUnreachable = errors.New("access point unreachable")
	errNotFound    = errors.New("not found")
	errNotListed   = errors.New("client list not understood")
)

type eap225 struct {
	cfg     Config
	base    string
	rt      http.RoundTripper
	timeout time.Duration
}

func newEAP225(cfg Config, rt http.RoundTripper) *eap225 {
	return &eap225{cfg: cfg, base: "http://" + cfg.Address.String(), rt: rt, timeout: DefaultTimeout}
}

func (e *eap225) Model() string        { return ModelEAP225 }
func (e *eap225) Address() netip.Addr  { return e.cfg.Address }
func (e *eap225) Prefix() netip.Prefix { return e.cfg.Prefix }

// Read logs in, reads the client list and logs out. It never retries a login.
func (e *eap225) Read(ctx context.Context) Result {
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Transport: e.rt, Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse // never follow the access point anywhere
	}}

	// A controller-managed access point has no client list of its own: don't log in.
	if body, err := e.do(ctx, client, http.MethodGet, pathManaged, ""); err == nil {
		var m struct {
			Success bool `json:"success"`
			ErrCode int  `json:"errCode"`
		}
		if json.Unmarshal(body, &m) == nil && !m.Success && m.ErrCode == http.StatusForbidden {
			return Result{Outcome: contract.OutcomeControllerManaged}
		}
	} else if errors.Is(err, errUnreachable) {
		return failed(err)
	}
	// The login page sets the older firmware's session cookie.
	if _, err := e.do(ctx, client, http.MethodGet, pathLoginPage, ""); errors.Is(err, errUnreachable) {
		return failed(err)
	}
	if outcome, err := e.login(ctx, client); outcome != "" {
		res := Result{Outcome: outcome}
		if err != nil {
			res = failed(err)
		}
		if outcome == contract.OutcomePageNotUnderstood {
			res.Detail = "the login state (data/login.json) was not understood"
		}
		return res
	}
	defer e.logout(client)

	// Each firmware generation has one list path. An answer other than 200 means "not this
	// endpoint" (the older unit answers the newer path with something other than 404 once logged
	// in, and drops the connection on a POST to it: gate G1, run 4).
	const noEndpoint = "no client list endpoint answered"
	detail := noEndpoint
	for _, p := range listPaths {
		body, err := e.do(ctx, client, http.MethodGet, p.path+"?operation=load", "")
		if errors.Is(err, errUnreachable) {
			return failed(err)
		}
		if err == nil && !jsonObject(body) {
			err = errNotListed // e.g. 200 with an empty body (gate G1, run 5): not this endpoint
		}
		if err != nil {
			if !errors.Is(err, errNotFound) || detail == noEndpoint { // keep the most useful description
				detail = p.name + " answered " + describe(body, err)
			}
			continue
		}
		if notLoggedIn(body) {
			return Result{Outcome: contract.OutcomePageNotUnderstood,
				Detail: p.name + " answered not logged in (timeout=true): the access point did not accept the login session"}
		}
		clients, keys, perr := parseClients(body)
		if perr != nil {
			// A 200 that is not a list: ask as Store.load on the page does (js/su/data/store.js),
			// with operation=load in a POST body. A failure here keeps the GET's description.
			if pbody, err2 := e.do(ctx, client, http.MethodPost, p.path, "operation=load"); err2 == nil {
				if c, k, err3 := parseClients(pbody); err3 == nil {
					clients, keys, perr = c, k, nil
				}
			}
		}
		if perr != nil {
			return Result{Outcome: contract.OutcomePageNotUnderstood, Detail: p.name + " answered " + describe(body, nil)}
		}
		return Result{Outcome: contract.OutcomeOK, Clients: clients, Path: p.name, Keys: keys}
	}
	return Result{Outcome: contract.OutcomePageNotUnderstood, Detail: detail}
}

// describe says what an answer looked like without quoting it: its size if it isn't JSON, else
// the top-level key names, the success and timeout flags, and whether a client list is there.
func describe(body []byte, err error) string {
	var se statusError
	if errors.As(err, &se) {
		return "HTTP " + strconv.Itoa(int(se))
	}
	if errors.Is(err, errNotFound) {
		return "HTTP 404"
	}
	if errors.Is(err, errNotListed) && body != nil {
		err = nil // describe the body
	}
	if err != nil {
		return "an answer that is too large"
	}
	var ans map[string]json.RawMessage
	if json.Unmarshal(body, &ans) != nil {
		return "not JSON (" + strconv.Itoa(len(body)) + " bytes)"
	}
	keys := make([]string, 0, len(ans))
	for k := range ans {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	flag := func(name string) string {
		if raw := field(ans, name); raw != nil {
			return strconv.FormatBool(truthy(raw))
		}
		return "missing"
	}
	s := "JSON with keys " + strings.Join(keys, ", ") + "; success=" + flag("success") + ", timeout=" + flag("timeout")
	if field(ans, "data") == nil && field(ans, "clients") == nil {
		s += "; no client list"
	}
	return s
}

func failed(err error) Result {
	if errors.Is(err, errUnreachable) {
		return Result{Outcome: contract.OutcomeUnreachable, Detail: strings.TrimPrefix(err.Error(), errUnreachable.Error()+": ")}
	}
	return Result{Outcome: contract.OutcomePageNotUnderstood}
}

// login posts the username and the upper-case hex MD5 of the password, in that order, as the
// login page does (the older firmware ignores another order: gate G1, run 2). It returns "" when
// logged in, else the outcome.
func (e *eap225) login(ctx context.Context, c *http.Client) (string, error) {
	sum := md5.Sum([]byte(e.cfg.Password.Reveal()))
	form := "username=" + url.QueryEscape(e.cfg.Username) + "&password=" + strings.ToUpper(hex.EncodeToString(sum[:]))
	body, err := e.do(ctx, c, http.MethodPost, pathLoginPage, form)
	if errors.Is(err, errUnreachable) {
		return contract.OutcomeUnreachable, err
	}
	var ans struct {
		Result *bool `json:"result"`
	}
	if err == nil && json.Unmarshal(body, &ans) == nil && ans.Result != nil && *ans.Result {
		return "", nil
	}
	// Not confirmed: the login state tells a wrong password from a lockout.
	state, err := e.do(ctx, c, http.MethodPost, pathLoginData, "operation=read")
	if err != nil {
		return failed(err).Outcome, err
	}
	var st struct {
		Error        any  `json:"error"` // a number, or a string on older firmware
		RetryChances *int `json:"retryChances"`
	}
	if json.Unmarshal(state, &st) != nil {
		return contract.OutcomePageNotUnderstood, nil
	}
	code := number(st.Error)
	switch {
	case code == 3 || (st.RetryChances != nil && *st.RetryChances == 0):
		return contract.OutcomeLocked, nil
	case code == 1:
		return contract.OutcomeLoginRejected, nil
	case ans.Result != nil && !*ans.Result:
		return contract.OutcomeLoginRejected, nil
	}
	return "", nil // no error recorded: try the list (it says so if the session is not valid)
}

func number(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(x))
		return n
	}
	return 0
}

// logout ends the session with its own short deadline; errors are ignored (the access point
// expires the session by itself).
func (e *eap225) logout(c *http.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), logoutTimeout)
	defer cancel()
	e.do(ctx, c, http.MethodGet, pathLogout, "")
}

// do sends one request and returns the body of a 200 answer. form is an encoded form body
// ("" for none), in the field order the access point's own page uses.
func (e *eap225) do(ctx context.Context, c *http.Client, method, path, form string) ([]byte, error) {
	var body io.Reader
	if form != "" {
		body = strings.NewReader(form)
	}
	req, err := http.NewRequestWithContext(ctx, method, e.base+path, body)
	if err != nil {
		return nil, errNotListed
	}
	if form != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	}
	req.Header.Set("Referer", e.base+"/")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	where := method + " " + strings.SplitN(path, "?", 2)[0] // no query, form or credentials in errors
	res, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %s", errUnreachable, where, netKind(err))
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, maxBody+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %s (reading the answer): %s", errUnreachable, where, netKind(err))
	}
	switch {
	case res.StatusCode == http.StatusNotFound:
		return nil, errNotFound
	case res.StatusCode != http.StatusOK:
		return nil, statusError(res.StatusCode)
	case len(b) > maxBody:
		return nil, errNotListed
	}
	return b, nil
}

// netKind names a transport failure without quoting it.
func netKind(err error) string {
	var ne net.Error
	msg := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()):
		return "timed out"
	case errors.Is(err, syscall.ECONNREFUSED) || strings.Contains(msg, "refused"):
		return "connection refused"
	case errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF):
		return "connection closed without an answer"
	case errors.Is(err, syscall.ECONNRESET) || strings.Contains(msg, "reset") || strings.Contains(msg, "forcibly closed"):
		return "connection reset"
	}
	return "network error"
}

// statusError is an answer other than 200 or 404.
type statusError int

func (e statusError) Error() string { return "HTTP " + strconv.Itoa(int(e)) }

// parseClients reads a client list answer: {"success":true,"timeout":false,"data":[{…}]}. Keys
// are matched case-insensitively (research R2); keys returns the first entry's key names.
func parseClients(body []byte) (clients []Client, keys []string, err error) {
	var ans map[string]json.RawMessage
	if json.Unmarshal(body, &ans) != nil {
		return nil, nil, errNotListed
	}
	if !truthy(field(ans, "success")) || truthy(field(ans, "timeout")) {
		return nil, nil, errNotListed
	}
	list := field(ans, "data")
	if list == nil {
		list = field(ans, "clients")
	}
	var entries []map[string]json.RawMessage
	if list == nil || json.Unmarshal(list, &entries) != nil {
		return nil, nil, errNotListed
	}
	clients = []Client{}
	for i, en := range entries {
		if i == 0 {
			for k := range en {
				keys = append(keys, k)
			}
			sort.Strings(keys)
		}
		clients = append(clients, clientFrom(en))
	}
	return clients, keys, nil
}

// truthy reads a flag the firmware sends as true or as the text "true" (the older firmware sends
// timeout as "false").
func truthy(raw json.RawMessage) bool {
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		return b
	}
	var s string
	return json.Unmarshal(raw, &s) == nil && strings.EqualFold(strings.TrimSpace(s), "true")
}

// jsonObject reports whether body is a JSON object, the shape of every answer of the data pages.
func jsonObject(body []byte) bool {
	var m map[string]json.RawMessage
	return json.Unmarshal(body, &m) == nil
}

// notLoggedIn reports an answer that says the session is not (or no longer) valid.
func notLoggedIn(body []byte) bool {
	var ans map[string]json.RawMessage
	return json.Unmarshal(body, &ans) == nil && truthy(field(ans, "timeout"))
}

func field(m map[string]json.RawMessage, names ...string) json.RawMessage {
	for _, n := range names {
		for k, v := range m {
			if strings.EqualFold(k, n) {
				return v
			}
		}
	}
	return nil
}

func text(m map[string]json.RawMessage, names ...string) string {
	raw := field(m, names...)
	if raw == nil {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return ""
}

func clientFrom(m map[string]json.RawMessage) Client {
	var c Client
	mac := strings.ToLower(strings.ReplaceAll(text(m, "mac"), "-", ":"))
	if contract.ValidMAC(mac) {
		c.MAC = mac
	}
	if ip, err := netip.ParseAddr(text(m, "ip")); err == nil && ip.Is4() && !ip.IsUnspecified() {
		c.IP = ip
	}
	c.SSID = text(m, "ssid")
	c.Band = band(text(m, "radio", "band", "freq"))
	c.Hostname = text(m, "hostname", "devicename", "name", "host") // DeviceName: older firmware
	if len(c.Hostname) > 253 || c.Hostname == "--" {
		c.Hostname = ""
	}
	return c
}

// band maps the access point's radio value to "2.4 GHz" or "5 GHz".
func band(v string) string {
	s := strings.ToLower(strings.ReplaceAll(v, " ", ""))
	switch {
	case s == "0" || strings.HasPrefix(s, "2.4") || s == "2g":
		return "2.4 GHz"
	case s == "1" || strings.HasPrefix(s, "5") || s == "5g":
		return "5 GHz"
	}
	return ""
}
