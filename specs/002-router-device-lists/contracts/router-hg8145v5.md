# Contract: Huawei HG8145V5 web interface (as used by the collector)

Observed on firmware variant `MGTS2` on 2026-10-09 (research R1). Read-only: the collector only
performs the requests below.

| Step | Request | Success | Failure mapping |
|------|---------|---------|-----------------|
| token | `POST /asp/GetRandCount.asp` | body: 48 hex chars (strip UTF-8 BOM) | connection error → `unreachable`; other body → `page_not_understood` |
| login | cookie `Cookie=body:Language:english:id=-1`; `POST /login.cgi` form `UserName`, `PassWord`=Base64(password), `Language=english`, `x.X_HW_Token` | `Set-Cookie: Cookie=sid=…:…:id=…`, body redirects to `index.asp` | redirect to login: fetch `/` and read `LockLeftTime` (>0 → `locked`) and `FailStat`/`LoginTimes` (→ `login_rejected`); any other reply (incl. the unverified "already logged in" message) → `session_busy` |
| list | `GET /html/bbsp/common/GetLanUserDevInfo.asp` with the session cookie | JS defining `var UserDevinfo = new Array(new USERDevice(…), …, null)` | no array / wrong arity → `page_not_understood` |
| token for logout | `GET /index.asp` → hidden input `id="onttoken"` | 48 hex chars | missing → skip explicit logout (router expires the session) |
| logout | `POST /logout.cgi?RequestFile=html/logout.html` form `x.X_HW_Token=<onttoken>` | any response | ignored (best effort) |

Record mapping (`USERDevice`, `isRealmac = 0`; `USERDeviceNew` shifts by one after `MacAddr`):

| Field | Use |
|-------|-----|
| IpAddr | observation `ip`; dropped if not inside the router's subnet (FR-008) |
| MacAddr | observation `mac` (lower-case); `RealMacAddr` preferred when `isRealmac = 1` |
| Port | observation `via` (`LAN1`, `SSID2`, …); empty for `--`, `LAN0`, `SSID0` |
| DevStatus | only `Online` entries become observations (FR-007) |
| HostName | observation `hostname` (`hostname_source = router`) unless empty or `--` |
| others | not used in this feature (DevType/DHCP vendor class noted for a future device-type hint) |

Strings are `\xNN`-escaped; decode before use. The list ends with `null`.
