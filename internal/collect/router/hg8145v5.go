package router

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// The HG8145V5 web interface as its own login page uses it (research R1,
// contracts/router-hg8145v5.md). Only these requests are ever sent (FR-004, read-only).
const (
	pathRandToken = "/asp/GetRandCount.asp"
	pathLogin     = "/login.cgi"
	pathList      = "/html/bbsp/common/GetLanUserDevInfo.asp"
	pathIndex     = "/index.asp"
	pathLogout    = "/logout.cgi?RequestFile=html/logout.html"
	pathLoginPage = "/"

	preLoginCookie = "body:Language:english:id=-1"
	maxBody        = 1 << 20 // larger answers are not a device list
	logoutTimeout  = 5 * time.Second
)

var (
	randTokenRe    = regexp.MustCompile(`^[0-9a-fA-F]{16,128}$`)
	ontTokenRe     = regexp.MustCompile(`id="onttoken"[^>]*value="([0-9a-fA-F]+)"`)
	lockLeftRe     = regexp.MustCompile(`var\s+LockLeftTime\s*=\s*'?(\d+)`)
	failStatRe     = regexp.MustCompile(`var\s+FailStat\s*=\s*'?(\d+)`)
	loginTimesRe   = regexp.MustCompile(`var\s+LoginTimes\s*=\s*'?(\d+)`)
	productTypeRe  = regexp.MustCompile(`var\s+ProductType\s*=\s*'([^']*)'`)
	isRealmacRe    = regexp.MustCompile(`var\s+isRealmac\s*=\s*'([^']*)'`)
	userDevArrayRe = regexp.MustCompile(`var\s+UserDevinfo\s*=\s*new\s+Array\(`)
)

type hg8145v5 struct {
	cfg     Config
	base    url.URL
	addr    netip.Addr
	prefix  netip.Prefix
	rt      http.RoundTripper
	timeout time.Duration
}

func newHG8145V5(cfg Config, rt http.RoundTripper) *hg8145v5 {
	u, _ := url.Parse(cfg.URL) // validated by New
	return &hg8145v5{cfg: cfg, base: url.URL{Scheme: u.Scheme, Host: u.Host}, addr: cfg.Address(),
		prefix: cfg.Prefix(), rt: rt, timeout: DefaultTimeout}
}

func (h *hg8145v5) Model() string        { return ModelHG8145V5 }
func (h *hg8145v5) Address() netip.Addr  { return h.addr }
func (h *hg8145v5) Prefix() netip.Prefix { return h.prefix }

// errUnreachable wraps transport failures (connection refused, timeout, cancellation).
var errUnreachable = errors.New("router unreachable")

// Read logs in, reads the device list and logs out. It never retries a login.
func (h *hg8145v5) Read(ctx context.Context) Result {
	ctx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()
	client := &http.Client{Transport: h.rt, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse // never follow the router anywhere
	}}

	tok, err := h.randToken(ctx, client)
	if err != nil {
		return failed(err)
	}
	session, outcome := h.login(ctx, client, tok)
	if outcome != "" {
		return Result{Outcome: outcome}
	}
	defer h.logout(client, session) // also after errors and cancellation (SC-006)

	body, _, err := h.do(ctx, client, http.MethodGet, pathList, session, nil)
	if err != nil {
		return failed(err)
	}
	devs, err := parseHG8145V5(body)
	if err != nil {
		return failed(err)
	}
	return Filter(devs, h.prefix, time.Now().UTC())
}

func failed(err error) Result {
	if errors.Is(err, errUnreachable) {
		return Result{Outcome: contract.OutcomeUnreachable}
	}
	return Result{Outcome: contract.OutcomePageNotUnderstood}
}

func (h *hg8145v5) randToken(ctx context.Context, c *http.Client) (string, error) {
	body, _, err := h.do(ctx, c, http.MethodPost, pathRandToken, "", url.Values{})
	if err != nil {
		return "", err
	}
	tok := strings.TrimSpace(strings.TrimPrefix(string(body), "\ufeff"))
	if !randTokenRe.MatchString(tok) {
		return "", ErrPageNotUnderstood
	}
	return tok, nil
}

// login returns the session cookie value, or the outcome of a failed login.
func (h *hg8145v5) login(ctx context.Context, c *http.Client, tok string) (string, string) {
	form := url.Values{
		"UserName":     {h.cfg.Username},
		"PassWord":     {base64.StdEncoding.EncodeToString([]byte(h.cfg.Password.Reveal()))},
		"Language":     {"english"},
		"x.X_HW_Token": {tok},
	}
	_, res, err := h.do(ctx, c, http.MethodPost, pathLogin, preLoginCookie, form)
	if err != nil {
		return "", failed(err).Outcome
	}
	for _, ck := range res.Cookies() {
		if ck.Name == "Cookie" && strings.HasPrefix(ck.Value, "sid=") {
			return ck.Value, ""
		}
	}
	// Not logged in: the login page tells a wrong password from a lockout. Any other reply
	// (e.g. someone else is logged in) is reported as busy (research R1).
	page, _, err := h.do(ctx, c, http.MethodGet, pathLoginPage, preLoginCookie, nil)
	if err != nil {
		return "", failed(err).Outcome
	}
	switch {
	case number(lockLeftRe, page) > 0:
		return "", contract.OutcomeLocked
	case number(failStatRe, page) == 1 || number(loginTimesRe, page) > 0:
		return "", contract.OutcomeLoginRejected
	default:
		return "", contract.OutcomeSessionBusy
	}
}

func number(re *regexp.Regexp, b []byte) int {
	m := re.FindSubmatch(b)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(string(m[1]))
	return n
}

// logout ends the session with its own short deadline, so it runs even when the read's
// context is done. Without a token the router expires the session by itself.
func (h *hg8145v5) logout(c *http.Client, session string) {
	ctx, cancel := context.WithTimeout(context.Background(), logoutTimeout)
	defer cancel()
	page, _, err := h.do(ctx, c, http.MethodGet, pathIndex, session, nil)
	if err != nil {
		return
	}
	m := ontTokenRe.FindSubmatch(page)
	if m == nil {
		return
	}
	h.do(ctx, c, http.MethodPost, pathLogout, session, url.Values{"x.X_HW_Token": {string(m[1])}})
}

// do sends one request with the router's "Cookie" cookie (if any) and returns the body.
func (h *hg8145v5) do(ctx context.Context, c *http.Client, method, path, cookie string, form url.Values) ([]byte, *http.Response, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	target := h.base.String() + path
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, nil, ErrPageNotUnderstood
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != "" {
		req.Header.Set("Cookie", "Cookie="+cookie)
	}
	res, err := c.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %s %s", errUnreachable, method, path) // no credentials in errors
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, maxBody+1))
	if err != nil {
		return nil, nil, fmt.Errorf("%w: reading %s", errUnreachable, path)
	}
	if res.StatusCode != http.StatusOK || len(b) > maxBody {
		return nil, nil, ErrPageNotUnderstood
	}
	return b, res, nil
}

// parseHG8145V5 reads GetLanUserDevInfo.asp. The page defines three arrays and picks one by
// ProductType and isRealmac; the parser makes the same choice.
func parseHG8145V5(body []byte) ([]Device, error) {
	s := strings.TrimPrefix(string(body), "\ufeff")
	pt := productTypeRe.FindStringSubmatch(s)
	rm := isRealmacRe.FindStringSubmatch(s)
	arrays := userDevArrayRe.FindAllStringIndex(s, -1)
	if pt == nil || rm == nil || len(arrays) != 3 {
		return nil, ErrPageNotUnderstood
	}
	// if (ProductType != '2') { if (isRealmac == 1) {New} else {plain} } else {plain}
	sel, ctor, nargs := 1, "USERDevice", 16
	switch {
	case pt[1] == "2":
		sel = 2
	case rm[1] == "1":
		sel, ctor, nargs = 0, "USERDeviceNew", 17
	}
	p := &jsParser{s: s, i: arrays[sel][1]}
	var devs []Device
	for {
		p.skip(" \t\r\n,")
		if p.consume("null") {
			p.skip(" \t\r\n")
			if !p.consume(")") {
				return nil, ErrPageNotUnderstood
			}
			return devs, nil
		}
		if !p.consume("new ") {
			return nil, ErrPageNotUnderstood
		}
		p.skip(" ")
		if p.ident() != ctor || !p.consume("(") {
			return nil, ErrPageNotUnderstood
		}
		args, ok := p.stringArgs()
		if !ok || len(args) != nargs {
			return nil, ErrPageNotUnderstood
		}
		if d, ok := deviceFrom(args, ctor == "USERDeviceNew"); ok {
			devs = append(devs, d)
		}
	}
}

// deviceFrom maps USERDevice(Domain, IpAddr, MacAddr, Port, IpType, DevType, DevStatus,
// PortType, Time, HostName, …); USERDeviceNew has RealMacAddr after MacAddr. Entries without
// an IPv4 address or a valid MAC are skipped.
func deviceFrom(a []string, realMAC bool) (Device, bool) {
	ip, mac, port, status, host := a[1], a[2], a[3], a[6], a[9]
	if realMAC {
		mac, port, status, host = a[3], a[4], a[7], a[10]
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil || !addr.Is4() {
		return Device{}, false
	}
	mac = strings.ToLower(mac)
	if !contract.ValidMAC(mac) {
		return Device{}, false
	}
	switch port {
	case "", "--", "LAN0", "SSID0":
		port = ""
	}
	if len(port) > contract.MaxViaLen {
		port = ""
	}
	if host == "--" || len(host) > 253 {
		host = ""
	}
	return Device{IP: addr, MAC: mac, Hostname: host, Via: port, Online: strings.EqualFold(status, "Online")}, true
}

// jsParser reads the small JavaScript subset of the device list: new Ctor("…", …).
type jsParser struct {
	s string
	i int
}

func (p *jsParser) skip(set string) {
	for p.i < len(p.s) && strings.IndexByte(set, p.s[p.i]) >= 0 {
		p.i++
	}
}

func (p *jsParser) consume(tok string) bool {
	if strings.HasPrefix(p.s[p.i:], tok) {
		p.i += len(tok)
		return true
	}
	return false
}

func (p *jsParser) ident() string {
	start := p.i
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
			p.i++
			continue
		}
		break
	}
	return p.s[start:p.i]
}

// stringArgs reads "a","b",…) and decodes the escapes.
func (p *jsParser) stringArgs() ([]string, bool) {
	var args []string
	for {
		p.skip(" \t\r\n")
		if !p.consume(`"`) {
			return nil, false
		}
		var b strings.Builder
		for {
			if p.i >= len(p.s) {
				return nil, false
			}
			c := p.s[p.i]
			p.i++
			if c == '"' {
				break
			}
			if c != '\\' {
				b.WriteByte(c)
				continue
			}
			if p.i >= len(p.s) {
				return nil, false
			}
			e := p.s[p.i]
			p.i++
			if e == 'x' {
				if p.i+2 > len(p.s) {
					return nil, false
				}
				v, err := strconv.ParseUint(p.s[p.i:p.i+2], 16, 8)
				if err != nil {
					return nil, false
				}
				b.WriteByte(byte(v))
				p.i += 2
				continue
			}
			b.WriteByte(e)
		}
		args = append(args, b.String())
		p.skip(" \t\r\n")
		switch {
		case p.consume(","):
		case p.consume(")"):
			return args, true
		default:
			return nil, false
		}
	}
}
