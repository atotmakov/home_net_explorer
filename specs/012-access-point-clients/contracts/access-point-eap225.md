# Contract: TP-Link EAP225 (standalone) client list

Model id `tplink-eap225`. Read-only: the reader only logs in, reads and logs out (spec FR-004).
All requests go directly to `http://<address>` (no proxy), the whole read bounded by 10 s.

| Step | Request | Expected answer |
|------|---------|-----------------|
| 1 | `GET /data/managedMode.json` | JSON; `success:false, errCode:403` → `controller_managed` |
| 2 | `GET /` | login page; keep any cookie it sets (older firmware sets `COOKIE=`) |
| 3 | `POST /` form `username=<u>&password=<UPPER(hex(md5(p)))>`, header `Referer: http://<address>/` | JSON; session cookie (`JSESSIONID` on newer firmware) |
| 3a | if the login answer does not say success: `POST /data/login.json` `operation=read` | `error:1` → `login_rejected`; `error:3` or `retryChances:0` → `locked` |
| 4 | `GET /data/status.client.user.json?operation=load` (newer firmware), on 404 `GET /data/monitor.client.client.json?operation=load` (older firmware) | `{"success":true,"timeout":false,"data":[…]}`; `timeout:true` → `page_not_understood` |
| 5 | `GET /data/logout.json` (errors ignored) | — |

Client entry keys (case-insensitive, research R2, to be confirmed by `check` on the owner's units):
`MAC`, `IP`, `SSID`, `Radio` / `band`, `hostname` / `name`. MACs in `AA-BB-CC-DD-EE-FF` or colon
form are normalized to `aa:bb:cc:dd:ee:ff`. `Radio` `0` → `2.4 GHz`, `1` → `5 GHz`.

Credentials appear only in step 3's body; they are never logged, printed or put in an error.
