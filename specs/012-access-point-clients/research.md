# Research: Access Point Client Lists

## Probe of the owner's access points (2026-10-24, no login)

Unauthenticated requests from the desktop (192.168.1.33) to both EAP225s:

| | 192.168.0.5 | 192.168.0.10 |
|---|---|---|
| Server header | `TP-LINK HTTPD/1.0` | `TP-LINK HTTPD/1.0` |
| Session cookie | `JSESSIONID` (HttpOnly) | `COOKIE=<hex>` set on the login page |
| `GET /data/managedMode.json` | `{"success":true,"errCode":0}` (standalone) | same (standalone) |
| `POST /data/login.json operation=read` | `{"error":0,"dutMode":"EAP225","retryChances":30,…}` | `{"error":0,"dutMode":"EAP225"}` |
| Client list | `/data/status.client.user.json` → `{"timeout":true,…}` (needs login) | `/data/monitor.client.client.json` → `{"timeout":true,…}` (needs login) |
| The other path | 404 | 404 |

Both login pages log in the same way: `POST /` with form fields `username` and
`password = upper-case hex MD5 of the password`, answered with JSON `{"result": …}`; the session
cookie then authorizes `/data/*.json`. The newer firmware (.5) counts failed logins
(`retryChances`, 30 left) and locks the login (`error: 3`, `waitTime`).

## R1 Two firmware families, one model

- **Decision**: one model `tplink-eap225`. The reader logs in, then asks
  `/data/status.client.user.json?operation=load` and, on 404, `/data/monitor.client.client.json?
  operation=load`; it remembers which path worked for the rest of the read.
- **Rationale**: the owner has one of each; the login is identical; the owner should not have to
  know the firmware generation.
- **Alternatives**: two models (`…-v5`, `…-v2`): the owner would have to pick right, for no gain.

## R2 Client fields (to be confirmed on the real APs)

- **Decision**: parse the list tolerantly: a JSON object with `success`, `timeout` and an array
  `data` (or `clients`) of objects; per entry, keys matched case-insensitively: `MAC` (any of
  `AA-BB-…`, `aa:bb:…`, normalized to lower-case colon form), `IP`, `SSID`, `Radio` / `band`
  (`0` or "2.4GHz" → `2.4 GHz`, `1` or "5GHz" → `5 GHz`), `hostname` / `name`. Unknown keys are
  ignored. An answer with `timeout: true` after a successful login, or without a client array, is
  `page_not_understood`.
- **Rationale**: Principle IV requires the technique to be confirmed before the plan depends on
  it, and the field names are only known from the firmware's public scripts and community tools,
  not from the owner's units. The first PR therefore ships the reader plus `check` output that
  lists, per AP, the firmware path used, the number of clients and the **key names** of the first
  entry (never values). The owner configures the APs and runs `check`; fixtures are adjusted to
  the real key names before the Map work (tasks gate G1).
- **Alternatives**: asking the owner for the password to inspect pages by hand: refused (the
  password stays in the web UI, feature 003).

## R3 Outcomes

| Reader sees | Outcome |
|---|---|
| no TCP connection / HTTP error / timeout (10 s) | `unreachable` |
| `managedMode.json` `errCode: 403` | `controller_managed` (new) |
| login answer `result` false, `error: 1` | `login_rejected` |
| login `error: 3` or `retryChances: 0` | `locked` |
| list not parseable | `page_not_understood` |
| list parsed | `ok` |

`login_rejected` and `locked` are recorded as rejections (same marker and hash as routers,
feature 003 R8), so the AP is not tried again until its settings change or `check` runs.
After the read the reader calls `/data/logout.json` when it exists (errors ignored); the session
otherwise times out on the AP.

## R4 Settings: the router settings table, told apart by model

- **Decision**: access point settings are rows of `router_settings` with model `tplink-eap225`.
  `ListRouters` returns router models only (old collectors never see an AP); a new
  `ListAccessPoints` returns AP models. The login endpoint `GET /api/v1/routers/{id}/login`,
  the rejection markers and the built-in collector's `router_rejected_builtin` setting are shared.
- **Rationale**: the columns are identical (model, subnet, username, password); feature 003's
  storage, masking and clearing rules apply unchanged (spec FR-003). No new table.
- **Model ids** live in `internal/contract` (`RouterModels = []string{"huawei-hg8145v5"}`,
  `AccessPointModels = []string{"tplink-eap225"}`), so the store and web need no collector
  package; `router.Models()` and `accesspoint.Models()` return those slices.
- **Alternatives**: a separate `access_point_settings` table: duplicate code for identical rules.

## R5 Delivery to collectors

- **Decision**: `GET /api/v1/ping` gains `access_points` (same `RouterRef` shape). Collectors
  older than 0.12 ignore it (the ping answer is decoded leniently). The list is cached in
  `hne-collector.access-points` like the router cache. Access points are only configured in the
  web UI; `hne-collector.yaml` gets no `access_points` key.
- **Rationale**: backward compatible; one place to configure.

## R6 Upload contract: `ap_client` observations

- **Decision** (schema version stays 1, additive):
  - `Observation.method = "ap_client"` with required `mac`, `via` = `"<SSID> (<band>)"` (SSID cut
    so the whole fits 32 characters) and new field `access_point` = the AP's address.
  - `RunSource.type = "access_point"`, `subnet` = the AP's subnet, `online` = clients sent as
    observations, `offline` = clients left out (no IP, bad MAC, or an IP outside the run's
    non-skipped subnets: the existing rule that observations lie in a scanned subnet stays).
  - An access point adds **no** `subnets` entry: it never completes a subnet scan (spec FR-008).
  - New outcome `controller_managed`.
  - `MaxSources` 8 → 16 (routers + access points).
- **Rationale**: the server needs which AP saw the client (Principle V: the raw run must be enough
  to rebuild the map); the address is what the collector knows; the server resolves it to a
  device at view time, as it does for routers (010 R4).
- **Server first**: the 0.12 server accepts these runs; a 0.11 server would reject them, so the
  NAS is updated before the desktop collector (quickstart §1).

## R7 One connection per scan

- **Decision**: in the collector, an AP client observation **replaces** the router observation of
  the same IP in that run (the router's hostname is kept when the AP has none). If both APs list
  the same MAC, the AP read later in the scan wins (reads run in parallel; order is by AP address
  for determinism).
- **Rationale**: a sighting row is keyed on its (ip, mac, hostname, via) tuple; two observations
  of one device per scan with different `via` would start a new sighting row every scan
  (`inventory.upsertSighting`). It also implements spec FR-012.

## R8 Storage and the map

- **Decision**: migration 0008 adds `sightings.via_source TEXT NOT NULL DEFAULT ''` (the AP
  address) and makes it part of the sighting tuple. `latestVias` returns it. In `Topology`, a via
  with a `via_source` hangs under the device whose current address is that IP (port node
  `port-<ap device id>-<via>`, edge from the AP); one without stays under the router of the
  subnet (010 R6). Last known for offline devices and manual-link precedence are unchanged.
  The device page shows "via <AP name> · <SSID> (<band>)".
- **Rationale**: smallest change to the 010 view; history stays reproducible from payloads.

## R9 Tests

- `internal/collect/accesspoint`: an `httptest` fake EAP225 with both firmware layouts, login
  success / rejection / lock, controller mode, garbage pages; fixtures use documentation MACs
  (`00:00:5e:…`) and addresses; a `hwtest`-tagged test like `hg8145v5_hw_test.go`.
- contract: validation of `ap_client`, `access_point` sources, the new outcome (both sides,
  Principle II).
- collector engine: replacement of the router observation, no subnet entry, counts.
- store/topology: AP port nodes, via_source resolution, last known, precedence.
- web: AP card on the device page (password never shown), ping `access_points`.
- integration: fake AP + fake router → upload → map has phones under the AP.
