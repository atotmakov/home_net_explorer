---

description: "Task list for Access Point Client Lists"
---

# Tasks: Access Point Client Lists

**Input**: `specs/012-access-point-clients/` (spec.md, plan.md, research.md, data-model.md,
contracts/upload-api-changes.md, contracts/access-point-eap225.md, contracts/web-ui-changes.md,
quickstart.md)

**Tests**: REQUIRED (constitution Principle II): tests first, pushed and red in CI, then the
implementation. CI only (no local Go). No real network: the EAP225 is an `httptest.Server`
(`internal/collect/accesspoint/eaptest`), time from `clock.Fake`. Fixtures use documentation MACs
(`00:00:5e:00:53:xx`) and the 192.168.0.x example addresses already used by router tests. No IPv4
CIDR literals in non-test `.go` files (lint).

**Gate G1** (research R2, Principle IV): after Phase 4's reader and `check` output are deployed, the
owner configures both APs and runs `hne-collector.exe check`; the reported key names are compared
with the fixtures (T019) before Phase 5 starts.

## Phase 1: Setup

- [X] T001 Set `VERSION` to `0.12`

## Phase 2: Foundational (contract and storage)

**Purpose**: the additive v1 contract (both sides use it) and the `via_source` column.

- [X] T002 [P] Tests in `internal/contract/validate_test.go` `TestAPClientObservation`: an
  `ap_client` observation is valid with `mac`, `via` and `access_point` (private IPv4) inside a
  scanned subnet; invalid without `mac`, without `via`, without `access_point`, with a public or
  malformed `access_point`, with `via` longer than 32; `access_point` on any other method is
  invalid; `TestAccessPointSource`: `type: "access_point"` valid with every existing outcome and
  the new `controller_managed`; unknown type still invalid; 16 sources valid, 17 invalid
- [X] T003 Implement in `internal/contract/v1.go` and `validate.go`: `ObsAPClient = "ap_client"`,
  `SourceTypeAccessPoint = "access_point"`, `OutcomeControllerManaged = "controller_managed"`
  (in `ValidOutcome`), `Observation.AccessPoint string \`json:"access_point,omitempty"\``,
  `MaxSources = 16`, `PingResponse.AccessPoints []RouterRef \`json:"access_points,omitempty"\``;
  validation per contracts/upload-api-changes.md ("`via` … ≤ 32 characters", "`mac` is required
  for `ap_client`", "`access_point` required for `ap_client`, forbidden otherwise"). Update the
  comments on `Via` and `RunSource`; add `RouterModels = []string{"huawei-hg8145v5"}` and
  `AccessPointModels = []string{"tplink-eap225"}` (research R4). Make T002 pass
- [X] T004 [P] Test in `internal/inventory/apply_ap_test.go` `TestAPClientSighting`: ingest a run
  with an `ap_client` observation (via `Home (5 GHz)`, access_point 192.168.0.5): the device
  exists, its sighting has `via = "Home (5 GHz)"` and `via_source = "192.168.0.5"`; a second run with
  the same tuple extends the same sighting row (`seen_count` 2); a run from the other AP
  (192.168.0.10) starts a new row; the run adds no complete scan of the subnet (a device missing
  from the next AP-only run is not offline); `TestRebuildKeepsViaSource` in
  the same test (written there): after a rebuild the sighting has the same `via_source`;
  `TestAPSourceStored`: a run with a `RunSource{Type: "access_point", Outcome:
  "controller_managed"}` and one with `ok, 3/1` are stored (`status: stored`) and read back with
  their type; existing router rows survive the migration unchanged
- [X] T005 Add `internal/store/migrations/0008_access_points.sql` (the `sightings.via_source`
  column and the `run_sources` rebuild of data-model.md) and include `via_source` in the sighting
  tuple of `upsertSighting` in `internal/inventory/apply.go` (compare and insert
  `o.AccessPoint`). Make T004 pass

## Phase 3: User Story 1 - Configure an access point from its device page (P1)

**Goal**: the Access point card stores model and login like the Router card.
**Independent test**: device of type `access_point` → card offered; save → model, address,
username shown, password only "set"; other types → no card.

- [X] T006 [P] [US1] Tests in `internal/store/routers_test.go` `TestAccessPointSettings`:
  `SaveRouter` with model `tplink-eap225` stores a row; `ListRouters` does not return it;
  `ListAccessPoints` returns `{id, model, address, subnet}` with the address picked like routers;
  a router row is not in `ListAccessPoints`; `RouterLogin(id)` returns the AP's login
- [X] T007 [US1] Implement in `internal/store/routers.go`: `ListAccessPoints(ctx)` (shared query
  helper with `ListRouters`, filtered by model set) and model filtering in `ListRouters`
  (model sets from `contract.RouterModels` / `contract.AccessPointModels`). Make T006 pass
- [X] T008 [P] [US1] Tests in `tests/integration/us_ap_ui_test.go` (where the router card's tests live): `GET /devices/{id}` of a device
  with type `access_point` shows an "Access point" card with the model select (`tplink-eap225`),
  username, password field and the device's current address; no card for `router` or untyped
  devices (and no Router card for `access_point`); `POST /devices/{id}/access-point` saves (303 to
  the device page); the password never appears in any page afterwards ("set" shown); first save
  without password → 400 with the form error; an unknown model → 400; a device not of type
  `access_point` → 400; `POST /devices/{id}/access-point/remove` deletes; changing the type away
  via `POST /devices/{id}/attrs` deletes the settings; a cross-site post → 403
- [X] T009 [US1] Implement in `internal/web/routers.go` (shared code with the router card where
  the rules are identical), `internal/web/server.go` (routes `POST /devices/{id}/access-point`,
  `POST /devices/{id}/access-point/remove`), `internal/web/pages.go` and
  `internal/web/templates/device.html` (the card, with the latest reads per collector from
  `RouterStatus(address)`: outcome, "N clients, M left out", when). Make T008 pass

## Phase 4: User Story 2 - Read which devices use each access point (P1)

**Goal**: collectors read both firmware layouts and report `ap_client` observations.
**Independent test**: fake EAP225 with three clients → a run with three `ap_client` observations
and an `access_point` source `ok, 3 / 0`; the server shows "via <AP> · <SSID> (<band>)".

- [X] T010 [P] [US2] Fake EAP225 in `internal/collect/accesspoint/eaptest/fake.go`:
  `httptest.Server` with options `Firmware` (`New` = `/data/status.client.user.json`, `Old` =
  `/data/monitor.client.client.json`, the other path 404), `Managed` (managedMode `errCode: 403`),
  `Username`/`Password` (checks `password` = upper-case hex MD5), `Locked`, `Clients []Entry`,
  raw-body override for garbage pages; it records every request (path, whether credentials were
  sent) so tests can count login attempts; `JSESSIONID` cookie for `New`, `COOKIE=` for `Old`.
  Client entries built by `eaptest.Client` with keys `MAC` (`00-00-5E-00-53-13` form), `IP`,
  `SSID`, `Radio`, `hostname` (research R2); `Transport()` routes private addresses to the fake
- [X] T011 [P] [US2] Tests in `internal/collect/accesspoint/eap225_test.go` (contracts/
  access-point-eap225.md): both firmwares read OK with MACs normalized to `00:00:5e:00:53:13`,
  `Radio 0/1` → `2.4 GHz`/`5 GHz`, hostname kept, `Result.Path` names the endpoint, `Result.Keys`
  lists the first entry's keys; wrong password → `login_rejected`; locked → `locked`;
  managed → `controller_managed` and no login sent; `timeout:true` after login or a non-JSON list
  → `page_not_understood`; connection refused / slow server beyond 10 s → `unreachable`; logout is
  requested after a read; an entry without IP is returned with a zero IP; no error string or log
  line contains the username or password (as `router/security_test.go`)
- [X] T012 [US2] Implement `internal/collect/accesspoint/accesspoint.go` (`ModelEAP225 =
  "tplink-eap225"`, `Models()`, `Client`, `Result{Outcome, Clients, Path, Keys}`, `Source`
  interface, `DefaultTimeout = 10 * time.Second`, reuse `router.Secret`) and `eap225.go`
  (cookie jar, `Referer`, MD5 login, endpoint fallback on 404, tolerant case-insensitive parse,
  outcomes of research R3). Make T011 pass
- [X] T013 [P] [US2] Tests in `internal/collect/accesspoint/remote_test.go`: `NewRemote` fetches the
  login before each read; a login fetch error → `login_unavailable`; after `login_rejected` the
  same login hash is skipped (`skipped_after_rejection`, zero requests to the fake); a new
  password is tried again
- [X] T014 [US2] Implement `internal/collect/accesspoint/remote.go` reusing `router.LoginFunc`,
  `router.Rejections` and `router.LoginHash`. Make T013 pass
- [X] T015 [P] [US2] Tests in `internal/collect/engine_ap_test.go`: an engine with a fake router
  (192.168.0.0/24, LAN1 for every device) and two fake APs: clients with an IP in a scanned subnet
  become `ap_client` observations (`via` "SSID (5 GHz)", cut so the whole is ≤ 32, `access_point`
  = AP address) that **replace** the router observation of the same IP, keeping the router's
  hostname when the AP has none; a client on both APs keeps the one from the AP with the higher
  address (deterministic order); clients without IP, with an invalid MAC or outside the run's
  scanned subnets are dropped and counted in the source's `offline`; each AP adds a
  `RunSource{Type: "access_point", Online, Offline}` and **no** `subnets` entry; a failed AP read
  leaves the rest of the run unchanged; the run passes `contract.Validate`; with the router fake
  and both AP fakes each delaying their answer by 300 ms, the whole read phase takes under 600 ms
  (the reads overlap, SC-003)
- [X] T016 [US2] Implement in `internal/collect/engine.go`: `Engine.AccessPoints
  []accesspoint.Source`, read in the same parallel group as the routers (one goroutine per
  source, each bounded by its 10 s timeout), merge per research R7. Make T015 pass
- [X] T016a [US2] Test in `internal/store/topology_test.go` `TestAccessPointNotGateway` (an `ok`
  access point read of 192.168.0.0/24 does not make the AP a gateway; the router stays the
  gateway) and restrict the gateway query in `internal/store/topology.go` (`run_sources … WHERE
  outcome = 'ok'`) to `type = 'router'`. In PR 1, so the first live AP read cannot change the map
- [X] T017 [P] [US2] Tests: in `tests/integration/us_ap_ui_test.go` ping
  answers `access_points` with the configured APs and no credentials, and `routers` without them;
  in `cmd/hne-collector/accesspoints_test.go` the collector builds AP sources from the ping list,
  caches it in `hne-collector.access-points` (no credentials in the file) and uses the cache when
  the server is unreachable; `check` prints per AP "tplink-eap225 at <addr> (login from server):
  OK, N clients (endpoint status.client.user, keys: …)" with no MAC, IP, name or credential in the
  output; a rejected login in `check` is recorded and cleared as for routers; in
  `internal/app/scanner_ap_test.go` the built-in collector reads configured APs too
- [X] T018 [US2] Implement: `internal/web/api.go` (`resp.AccessPoints, err =
  Store.ListAccessPoints`), `cmd/hne-collector/accesspoints.go` (build, cache, `checkAccessPoints`
  next to `checkRouters`, phrases for every outcome incl. `controller_managed`),
  `internal/app/scanner.go` (built-in collector). Make T017 pass
- [X] T019 [US2] **Gate G1** (manual, after deploying T001–T018 as 0.12 with quickstart §1–§3):
  the owner runs `hne-collector.exe check` (needs T016a deployed); record in this file the endpoint and key names
  reported for 192.168.0.5 and 192.168.0.10; if they differ from T010's fixtures, update the
  fixtures and the parser (red test first) before Phase 5
  - Run 1 (2026-10-24, collector 0.13 on the desktop, PR #24 deployed as 0.12.131):
    - 192.168.0.5 (newer firmware): OK, endpoint `status.client.user`, keys `ActiveTime, Down,
      IP, MAC, RSSI, Radio, Rate, SSID, Up, hostname, key, limit, limit_download,
      limit_download_unit, limit_upload, limit_upload_unit`. The parsed keys (`MAC`, `IP`,
      `SSID`, `Radio`, `hostname`) are as assumed; the rest is ignored. No parser change.
    - 192.168.0.10 (older firmware): `page not understood`, login not rejected. The unit's own
      scripts (`js/su/data/proxy.js`, `store.js`, readable without a login) ask for lists with
      POST `operation=load`, not GET. Fix: the reader falls back to that POST, and `check` now
      says what an unreadable answer looked like (key names and flags only). Tests
      `TestReadPostOnlyFirmware`, `TestPageNotUnderstoodDetail`, `TestCheckShowsWhyNotUnderstood`.
    - Side finding: the desktop's own `home` interface is 3c:52:a1:29:75:20 (192.168.1.10), so
      the "TP-Link Systems" device at 192.168.1.10 is the desktop, not a router.
  - Run 2 (0.13.139, PR #26): 192.168.0.10 `unreachable`: the unit drops the connection on the
    POST fallback. The owner logged in to the unit's web page in a browser; its requests showed
    the login form sends `username` before `password` (the unit ignores a login in another
    order) and the list is `monitor.client.client` with keys `AccessPoint, ActiveTime, CCQ,
    DeviceName, Down, IP, MAC, Radio, Rate, SNR, SSID, Up`, `Radio` as text ("2.4GHz") and
    `timeout` as the string "false". Fix PR #27 (login field order, list format); PR #28 names the
    failing request of an unreachable access point in `check`.
  - Run 3 (PR #28): 192.168.0.10 unreachable, "POST /data/status.client.user.json: connection
    reset": once logged in the unit answers the newer firmware's path with something other than
    404. Fix PR #29: any answer other than 200 on a list path means "not this endpoint", no POST.
  - Run 4 (PR #29): 192.168.0.10 "status.client.user answered not JSON (0 bytes)": a 200 with an
    empty body. Fix PR #30: anything but a JSON object means "not this endpoint".
  - Run 5 (0.13.154, PR #30): both OK. 192.168.0.5: 2 clients, endpoint `status.client.user`;
    192.168.0.10: 2 clients, 0 without an IP address, endpoint `monitor.client.client`, keys as in
    run 2. A scan by the desktop collector stored both reads `ok`. **G1 passed.**
- [X] T020 [P] [US2] Tests in `internal/store/devices_query_test.go` and
  `internal/web/access_point_test.go`: a device whose latest sighting has `via_source` shows
  "via <AP display name> · Home (5 GHz)" on its device page (FR-013); an unknown AP address shows
  "via 192.168.0.5 · Home (5 GHz)"; the Collectors page lists "router huawei-hg8145v5 at
  192.168.0.1: ok, …" and "access point tplink-eap225 at 192.168.0.5: ok, 3 clients, 1 left out"
  for the same run
- [X] T021 [US2] Implement in `internal/store/devices_query.go`, `internal/web/pages.go`,
  `internal/web/templates/device.html`, `internal/web/collectors.go` and its template; select
  `type` in `internal/store/collectors.go` (latest run sources) and label it. Make T020 pass

## Phase 5: User Story 3 - Wi-Fi devices on the Map (P1)

**Goal**: Wi-Fi devices hang under their access point.
**Independent test**: two devices reported by AP A, one by AP B → each under the right AP's
"SSID (band)" port node; none under the router's LAN1.

- [X] T022 [P] [US3] Tests in `internal/store/topology_test.go` `TestTopologyAccessPoints`: port
  node `port-<ap dev id>-Home (5 GHz)` with parent = the AP's group, edge `rp-<ap>-<via>` from the
  AP (current) and `pd-<dev>` to each client; an offline client keeps its last AP with `current:
  false`; a device whose latest via is the router's LAN1 stays under the router; a manual link
  still replaces the AP port edge; an AP address that no device holds → client drawn without a
  port edge; an `ok` AP read does not make the AP a gateway (`gateway_of` empty); JSON shape
  unchanged (existing `TestMapJSONShape` still passes)
- [X] T023 [US3] Implement in `internal/store/topology.go`: `viaSighting.source` from
  `latestVias`; parent device = device with current address `source` when set, else the subnet's
  router (010 R6). Make T022 pass
- [X] T024 [P] [US3] Integration test `tests/integration/us_ap_test.go` `TestAccessPointEndToEnd`:
  server + collector engine with fake router and two fake EAP225s (one per firmware), configured
  through the web UI forms; after one scan `/ui/map.json` has the phones under their AP, the
  Collectors page shows both reads `ok`; after a scan where the phone moved AP the map shows the new
  AP (SC-005); with a wrong password the fake sees exactly one login attempt over three scans
  (SC-004)
- [X] T025 [US3] Make T024 pass (fix whatever it finds); keep `TestMapPerformance` green
  - T024 runs the built-in collector against the two fake access points without a fake
    router: the router path and the replacement of a router's observation by the access point's
    are covered by `TestTopologyAccessPoints` and `internal/collect/engine_ap_test.go`.

## Phase 6: Polish & Cross-Cutting

- [ ] T026 [P] Docs: cross-references from `specs/002-router-device-lists/contracts/
  upload-api-changes.md` and `specs/003-router-ui-setup/contracts/api-changes.md` to 012; the 010
  `contracts/web-ui-changes.md` note that ports may hang under access points; the security TODOs of
  spec 003 (TODO-SEC-1..5) now also cover AP passwords (one line there)
- [ ] T027 Hardware test `internal/collect/accesspoint/eap225_hw_test.go` (`//go:build hwtest`,
  `HNE_HWTEST_AP_URL/USER/PASS`, logs counts and key names only), like `hg8145v5_hw_test.go`
- [ ] T028 Real-network validation, quickstart §4–§7 (after deploying 0.12 and the new collector):
  record results in this file (counts only, no MACs)

## Dependencies & Execution Order

- Phase 1 → Phase 2 (T002/T003 before anything using the contract; T004/T005 before US2 storage).
- US1 (T006–T009) needs Phase 2 only.
- US2 (T010–T021) needs Phase 2; T017/T018 need T007 (`ListAccessPoints`); T019 (gate) needs
  T001–T018 deployed; T020/T021 need T005.
- US3 (T022–T025) needs T005 and **G1 (T019)**.
- Polish after US3; T027 any time after T012.

## Parallel Opportunities

- T002 and T004 (different packages); T006, T008, T010, T011 once Phase 2 is in.
- In US2: T010 + T011 + T013 (fake, reader tests, remote tests) together; T015 and T017 in
  parallel once T012/T014 exist.
- T022 and T024 together.

## Implementation Strategy

1. **PR 1** (Phases 1–4 up to T018, plus T016a): contract, storage, settings card, reader, engine, collector
   and `check`. Merge, deploy, download the collector, owner configures both APs → **gate G1**.
2. **PR 2** (T019 fixes if any, T020–T025): device page and Collectors text, the Map, integration
   test. Merge, deploy.
3. **PR 3** (Phase 6): docs, hardware test, real-network results.
