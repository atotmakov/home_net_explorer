package accesspoint

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
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
	if body, err := e.do(ctx, client, http.MethodGet, pathManaged, nil); err == nil {
		var m struct {
			Success bool `json:"success"`
			ErrCode int  `json:"errCode"`
		}
		if json.Unmarshal(body, &m) == nil && !m.Success && m.ErrCode == http.StatusForbidden {
			return Result{Outcome: contract.OutcomeControllerManaged}
		}
	} else if errors.Is(err, errUnreachable) {
		return Result{Outcome: contract.OutcomeUnreachable}
	}
	// The login page sets the older firmware's session cookie.
	if _, err := e.do(ctx, client, http.MethodGet, pathLoginPage, nil); errors.Is(err, errUnreachable) {
		return Result{Outcome: contract.OutcomeUnreachable}
	}
	if outcome := e.login(ctx, client); outcome != "" {
		return Result{Outcome: outcome}
	}
	defer e.logout(client)

	for _, p := range listPaths {
		body, err := e.do(ctx, client, http.MethodGet, p.path+"?operation=load", nil)
		if errors.Is(err, errNotFound) {
			continue // the other firmware generation
		}
		if err != nil {
			return failed(err)
		}
		clients, keys, err := parseClients(body)
		if err != nil {
			return failed(err)
		}
		return Result{Outcome: contract.OutcomeOK, Clients: clients, Path: p.name, Keys: keys}
	}
	return Result{Outcome: contract.OutcomePageNotUnderstood}
}

func failed(err error) Result {
	if errors.Is(err, errUnreachable) {
		return Result{Outcome: contract.OutcomeUnreachable}
	}
	return Result{Outcome: contract.OutcomePageNotUnderstood}
}

// login posts the username and the upper-case hex MD5 of the password, as the login page does.
// It returns "" when logged in, else the outcome.
func (e *eap225) login(ctx context.Context, c *http.Client) string {
	sum := md5.Sum([]byte(e.cfg.Password.Reveal()))
	form := url.Values{"username": {e.cfg.Username}, "password": {strings.ToUpper(hex.EncodeToString(sum[:]))}}
	body, err := e.do(ctx, c, http.MethodPost, pathLoginPage, form)
	if errors.Is(err, errUnreachable) {
		return contract.OutcomeUnreachable
	}
	var ans struct {
		Result *bool `json:"result"`
	}
	if err == nil && json.Unmarshal(body, &ans) == nil && ans.Result != nil && *ans.Result {
		return ""
	}
	// Not confirmed: the login state tells a wrong password from a lockout.
	state, err := e.do(ctx, c, http.MethodPost, pathLoginData, url.Values{"operation": {"read"}})
	if err != nil {
		return failed(err).Outcome
	}
	var st struct {
		Error        any  `json:"error"` // a number, or a string on older firmware
		RetryChances *int `json:"retryChances"`
	}
	if json.Unmarshal(state, &st) != nil {
		return contract.OutcomePageNotUnderstood
	}
	code := number(st.Error)
	switch {
	case code == 3 || (st.RetryChances != nil && *st.RetryChances == 0):
		return contract.OutcomeLocked
	case code == 1:
		return contract.OutcomeLoginRejected
	case ans.Result != nil && !*ans.Result:
		return contract.OutcomeLoginRejected
	}
	return "" // no error recorded: try the list (it says so if the session is not valid)
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
	e.do(ctx, c, http.MethodGet, pathLogout, nil)
}

// do sends one request and returns the body of a 200 answer.
func (e *eap225) do(ctx context.Context, c *http.Client, method, path string, form url.Values) ([]byte, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, e.base+path, body)
	if err != nil {
		return nil, errNotListed
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Referer", e.base+"/")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	res, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s %s", errUnreachable, method, strings.SplitN(path, "?", 2)[0]) // no credentials in errors
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, maxBody+1))
	if err != nil {
		return nil, fmt.Errorf("%w: reading %s", errUnreachable, path)
	}
	switch {
	case res.StatusCode == http.StatusNotFound:
		return nil, errNotFound
	case res.StatusCode != http.StatusOK || len(b) > maxBody:
		return nil, errNotListed
	}
	return b, nil
}

// parseClients reads a client list answer: {"success":true,"timeout":false,"data":[{…}]}. Keys
// are matched case-insensitively (research R2); keys returns the first entry's key names.
func parseClients(body []byte) (clients []Client, keys []string, err error) {
	var ans map[string]json.RawMessage
	if json.Unmarshal(body, &ans) != nil {
		return nil, nil, errNotListed
	}
	var success, timeout bool
	json.Unmarshal(field(ans, "success"), &success)
	json.Unmarshal(field(ans, "timeout"), &timeout)
	if !success || timeout {
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
	c.Hostname = text(m, "hostname", "name", "host")
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
