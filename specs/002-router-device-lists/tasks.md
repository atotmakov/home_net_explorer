---

description: "Task list for Router Device Lists"
---

# Tasks: Router Device Lists

**Input**: Design documents from `specs/002-router-device-lists/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: REQUIRED. Constitution Principle II (Test-First, non-negotiable) applies. Every test task
comes before the implementation it covers, and the test MUST be run and seen to FAIL before that
implementation starts. Unit tests never touch the real network or the real router. They use the
fake HG8145V5 server from T011 and the anonymized capture from T001.

**Organization**: tasks are grouped by user story. Each story phase is a deliverable increment
that can be tested on its own.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on unfinished tasks)
- **[Story]**: US1–US3 from spec.md
- Paths are relative to the repository root (one Go module, see plan.md "Project Structure")

## Conventions for every task

- Everything from `specs/001-lan-inventory-topology/tasks.md` "Conventions for every task" still
  applies: module `github.com/atotmakov/home_net_explorer`, Go ≥ 1.26, UTC RFC 3339 timestamps
  with milliseconds, lower-case MACs `aa:bb:cc:dd:ee:ff`, and **no IPv4 range literals in non-test
  Go code** outside `internal/contract/private.go` (`make lint` enforces this). The router's
  default subnet is computed (`netip.PrefixFrom(addr, 24).Masked()`), never written as a literal.
- **No new dependencies** (plan.md): router access uses only the standard library (`net/http`,
  `net/http/cookiejar`, `encoding/base64`, `regexp`, `crypto/sha256`).
- **Toolchain**: nothing is built locally. Tests run in GitHub Actions (`ci.yml`: `test` on
  Linux, `test-windows` for `./internal/collect/...` and `./cmd/hne-collector/...`). "See it fail"
  means pushing the test commit and seeing the red CI run before pushing the implementation.
- **Secrets** (FR-006, SC-004): the router password and username never appear in a run, a log
  record, an error message, `check` output (text or `--json`), the spool or the server's
  database. The router's session id (`sid` cookie) and tokens are not logged either.
- **Read-only router access** (FR-004): the collector sends only the five requests in
  `contracts/router-hg8145v5.md`, makes exactly one login attempt per read, and never tries
  default credentials (FR-003).
- **Router address** must be a private RFC 1918 IPv4 address (FR-005). Tests that need a local
  fake router keep a private address in the config and redirect the connection with a test
  `http.RoundTripper` (T011), so the production check is never weakened for loopback.
- Router outcomes (data-model.md `RunSource.outcome`): `ok`, `unreachable`, `login_rejected`,
  `locked`, `session_busy`, `page_not_understood`, `skipped_after_rejection`.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: the fixture, the version and the new package

- [X] T001 Copy the anonymized HG8145V5 capture to
  `internal/collect/router/testdata/hg8145v5_getlanuserdevinfo.asp`, and add
  `internal/collect/router/testdata/*.asp -text` to `.gitattributes` so it keeps the router's
  bytes (UTF-8 BOM, CRLF). Content: `ProductType = '1'`, `isRealmac = '0'`, three arrays
  (`USERDevice` ×2 variants, `USERDeviceNew`) of 30 records. The selected `USERDevice` array has
  13 `Online` entries, MACs from `00:00:5e:…` (20) and locally-administered `02:00:5e:…` (10),
  hostnames `host-01`…`host-23` or empty, IPs `192.168.0.3`–`192.168.0.31` plus one static
  `0.0.0.0`, ports `LAN1`/`LAN2`, DevType `vendor-class`/`--`/empty. It contains no real MAC,
  hostname or model string
- [X] T002 Set `VERSION` to `0.3` (research R8), and write `internal/collect/router/doc.go`: the
  package comment says router sources are opt-in, read-only, configured only in the remote
  collector's `hne-collector.json`, and that credentials never leave the collector machine

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the additive v1 contract change and migration 0003, which every story builds on

**⚠️ CRITICAL**: no user story work can begin until this phase is complete

### Tests first

- [X] T003 [P] Add contract fixtures to `tests/contract/fixtures/` and register them in
  `tests/contract/schema_test.go`:
  - `valid_router.json`: subnets `192.168.1.0/24` (`arp`) and `192.168.0.0/24`
    (`router_table`, `complete: true`, `hosts_probed: 30`); `router_table` observations with
    `mac`, `hostname` + `hostname_source: "router"`, and `via` (`LAN1`, `SSID2`); one observation
    without hostname or via; `sources: [{type: "router", model: "huawei-hg8145v5", address:
    "192.168.0.1", subnet: "192.168.0.0/24", outcome: "ok", online: 14, offline: 16}]`.
  - `valid_router_failed.json`: `192.168.0.0/24` as `router_table` with `complete: false`,
    `hosts_probed: 0`, no observations in it, and a source with `outcome: "login_rejected"`,
    `online: 0`, `offline: 0`.
  - Schema-detectable invalid fixtures (add to `schemaInvalid`): `invalid_router_bad_outcome.json`
    (`outcome: "rebooted"`), `invalid_source_with_password.json` (a `sources[]` entry with an
    extra `password` field: rejected by `additionalProperties: false`, and also by
    `contract.Decode`), `invalid_via_too_long.json` (`via` of 33 characters).
  - `contract.Validate`-only invalid fixtures: `invalid_router_without_mac.json`
    (`router_table` observation, no MAC), `invalid_via_on_arp.json` (`via` on an `arp`
    observation), `invalid_router_public_address.json` (`sources[].address` `8.8.8.8`).
  - Extend `internal/contract/contracttest` with `TooManySources(t)` (9 sources), checked by
    both the schema test and `Validate`.
  - All existing `valid_*` fixtures must still pass unchanged (old collectors keep working).
- [X] T004 [P] Extend `internal/contract/validate_test.go` (`TestValidateFixtures` picks up the
  new fixtures). Add table cases:
  - `router_table` observations need a MAC; `via` is allowed only on `router_table`, at most 32
    characters; `hostname_source` accepts `router`.
  - A `router_table` subnet counts as scanned: its observations pass the "IP inside a scanned
    subnet" rule, exactly like `arp`.
  - `sources`: at most 8; `type` only `router`; `address` must be private IPv4; `subnet` must
    parse with `ParseSubnet` (private, /16–/30); `outcome` from the list in Conventions;
    `online`/`offline` ≥ 0 and both 0 unless `outcome` is `ok`; `model` ≤ 64 characters.
- [X] T005 [P] Write `internal/store/testdata/seed_2.sql` (sample rows at schema v2, including
  `sightings` and `run_subnets` rows) so `TestMigrationHarness` in `internal/store/store_test.go`
  checks 2 → 3 with row counts preserved. Add a test in `store_test.go`: after migration,
  existing sightings have `via = ''`, and `run_sources` exists with primary key
  `(collection_id, idx)`

### Implementation

- [X] T006 Edit the canonical contract
  `specs/001-lan-inventory-topology/contracts/collector-upload-api.yaml` exactly as in
  `contracts/upload-api-changes.md`: `SubnetScan.method` adds `router_table`;
  `Observation.method` adds `router_table`; `hostname_source` adds `router`; new
  `Observation.via` (`maxLength: 32`); optional `CollectionRun.sources` (`maxItems: 8`) of the
  new `RunSource` schema (`additionalProperties: false`, required `[type, model, address, subnet,
  outcome, online, offline]`). Keep `schema_version` 1. Add to the API description: "Additive
  change (feature 002): upgrade the server before collectors that send `router_table` or
  `sources`." The `contract-lint` CI job (redocly) must pass. Make T003 schema cases pass
- [X] T007 Extend `internal/contract/v1.go` and `internal/contract/validate.go`: constants
  `MethodRouterTable = "router_table"`, `ObsRouterTable = "router_table"`,
  `HostnameSourceRouter = "router"`, `SourceTypeRouter = "router"`, `MaxSources = 8`,
  `MaxViaLen = 32`, and the outcome constants (`OutcomeOK`, `OutcomeUnreachable`,
  `OutcomeLoginRejected`, `OutcomeLocked`, `OutcomeSessionBusy`, `OutcomePageNotUnderstood`,
  `OutcomeSkippedAfterRejection`); `Observation.Via string \`json:"via,omitempty"\``;
  `CollectionRun.Sources []RunSource \`json:"sources,omitempty"\``; type `RunSource` (`Type`,
  `Model`, `Address`, `Subnet`, `Outcome`, `Online`, `Offline`). The struct has **no credential
  fields**. Add the T004 rules to `Validate`. Make T003 and T004 pass, including
  `TestGoTypesRoundTrip`
- [X] T008 Write `internal/store/migrations/0003_router_sources.sql` (data-model.md "Server"):
  ```sql
  ALTER TABLE sightings ADD COLUMN via TEXT NOT NULL DEFAULT '';
  CREATE TABLE run_sources (
      collection_id TEXT NOT NULL REFERENCES collection_runs (collection_id),
      idx           INTEGER NOT NULL,
      type          TEXT NOT NULL CHECK (type IN ('router')),
      model         TEXT NOT NULL,
      address       TEXT NOT NULL,
      subnet        TEXT NOT NULL,
      outcome       TEXT NOT NULL CHECK (outcome IN ('ok', 'unreachable', 'login_rejected',
                        'locked', 'session_busy', 'page_not_understood', 'skipped_after_rejection')),
      online        INTEGER NOT NULL CHECK (online >= 0),
      offline       INTEGER NOT NULL CHECK (offline >= 0),
      PRIMARY KEY (collection_id, idx)
  );
  ```
  (`run_subnets` in `0001_init.sql` uses the same reference.) Start the file with a `-- Schema v3 (feature 002): …` comment like `0002`. Make T005 pass

**Checkpoint**: the server accepts old runs and runs with router data; the database is at v3.

---

## Phase 3: User Story 1 - See real devices behind the ISP router (Priority: P1) 🎯 MVP

**Goal**: a collector configured with the HG8145V5's address and login reads its device list at
every scan and reports the online devices as `router_table` observations with real MACs, so they
appear in the inventory like devices on directly attached subnets, weak IP-only records fold into
them, and offline detection on the router's subnet works.

**Independent Test**: configure the router in `hne-collector.json`, run `hne-collector scan
--once`: every device the router lists as online appears on **Devices** once, with its MAC
(quickstart.md §2 steps 3–5, SC-001, SC-002).

### Tests for User Story 1 ⚠️ write first, see them fail

- [X] T009 [P] [US1] Write `internal/collect/router/hg8145v5_parse_test.go` for
  `parseHG8145V5(body []byte) ([]Device, error)`:
  - The T001 fixture gives 30 devices, 13 with `Online: true`. MACs are lower-case; `\xNN` escapes
    are decoded (`192\x2e168\x2e0\x2e4` → `192.168.0.4`); the BOM and CRLF are tolerated.
  - `Port` maps to `Via`: `LAN1`/`LAN2`/`SSID2` kept; `LAN0`, `SSID0`, `--` and empty → `""`.
  - `HostName` empty or `--` → `Hostname ""`.
  - Variant selection like the page: a hand-made copy with `isRealmac = '1'` uses the
    `USERDeviceNew` array and takes `RealMacAddr` (the argument after `MacAddr`) as the MAC; a copy
    with `ProductType = '2'` picks that branch's array. Write the variants in the test (string
    edits of the fixture), not as new committed captures.
  - An array with only `null` gives 0 devices and no error.
  - Garbled input (HTML login page, truncated array, a record with the wrong argument count,
    missing `ProductType`) returns `ErrPageNotUnderstood`.
  - IPv6-only or empty `IpAddr` entries are skipped (IPv4 only, spec Assumptions).
- [X] T010 [P] [US1] Write `internal/collect/router/router_test.go`:
  - Registry: `New(Config{Model: "huawei-hg8145v5", …}, nil)` returns a `Source`; an unknown
    model returns an error naming the supported models (FR-002).
  - `Config.Validate()`: `url` must be `http`/`https` with a **private IPv4** host and optional
    port (FR-005), so `http://8.8.8.8`, `http://router.lan` and `http://127.0.0.1` are errors;
    `username` and `password` required; `subnet` optional, private, /16–/30, and when absent
    `Config.Prefix()` is the host /24 (`192.168.0.1` → `192.168.0.0/24`, `10.20.30.254` →
    `10.20.30.0/24`, research R5). No error message contains the password.
  - `Secret`: `fmt.Sprint`, `%v`, `%+v`, `%#v`, `json.Marshal` and `slog` (`LogValue`) of a
    `Secret` and of a whole `Config` never show the password; `Secret.Reveal()` returns it.
  - `Filter(devices, prefix)`: returns `Online` devices whose IP is inside `prefix` as
    observations; offline entries are dropped (FR-007), and so are entries outside the subnet
    such as `0.0.0.0` or `192.168.8.20` (FR-008). `Result.Online`/`Result.Offline` count the
    router's entries **inside the subnet** (the fixture: 13 / 16).
- [X] T011 [P] [US1] Write the fake router `internal/collect/router/routertest/fake.go`, plus
  `internal/collect/router/hg8145v5_test.go` that uses it.
  - The fake is an `httptest.Server` implementing `contracts/router-hg8145v5.md`:
    `POST /asp/GetRandCount.asp` (BOM + 48 hex), `POST /login.cgi` (checks the
    `Cookie=body:Language:english:id=-1` cookie, `UserName`, Base64 `PassWord`,
    `Language=english`, a fresh `x.X_HW_Token`), `GET /html/bbsp/common/GetLanUserDevInfo.asp`
    (serves the T001 fixture only with a valid `sid`), `GET /index.asp` (hidden
    `id="onttoken"`), and `POST /logout.cgi?RequestFile=html/logout.html`.
  - Scripted modes: `WrongPassword` (redirect to the login page whose `/` carries `FailStat`,
    `LoginTimes`), `Locked` (`LockLeftTime` > 0), `SessionBusy` (the "already logged in"
    message), `GarbledList`, `NoOntToken`, `Hang` (never answers).
  - It records every request. Any path or method outside the list fails the test (FR-004).
  - `routertest.Redirect(fake)` returns an `http.RoundTripper` that sends requests for any host
    to the fake, so configs keep a private address such as `http://192.168.0.1`.
  - Cases in `hg8145v5_test.go`:
    - A full read returns `OutcomeOK`, 13 online / 16 offline (in-subnet; the offline `0.0.0.0` entry is not
      counted) and 13 observations; the
      request sequence is exactly token → login → list → index → logout.
    - **Logout always follows a successful login**, also when the list is garbled
      (`page_not_understood`) or the context is cancelled after login (SC-006, US2 AS-4).
    - Wrong password → `login_rejected`; locked → `locked`; busy (and any other unrecognized
      login reply) → `session_busy`; garbled →
      `page_not_understood`; closed port → `unreachable`; `Hang` → `unreachable` within the
      read timeout (set to 200 ms in the test; production default 10 s, plan.md).
    - Exactly one login request per read, with the configured credentials only (FR-003).
    - Missing `onttoken` → no logout request, outcome still `ok`.
- [X] T012 [P] [US1] Write `internal/collect/engine_router_test.go` with a fake `router.Source`
  (add `FakeRouter` to `internal/collect/collecttest/fakes.go`: settable `Result`, records calls).
  The vantage has on-link `192.168.1.0/24` and `192.168.8.0/24`, and the router serves
  `192.168.0.0/24` (routed, research R4):
  - Read OK: `192.168.0.0/24` appears once as `router_table`, `complete: true`, `hosts_probed` =
    online + offline. It is **not** ICMP/TCP-probed: the fake presence prober gets no calls for
    it. Each online device is an observation with `method: router_table`, `mac`, `hostname` +
    `hostname_source: router` (when set), and `via` (when set). A device that would not answer
    pings is present (US1 AS-3).
  - `run.Sources` has one entry: model, address, subnet, `ok`, and the counts.
  - Read failed (each failure outcome) and the subnet is not an extra subnet:
    `192.168.0.0/24` is `router_table`, `complete: false`, `hosts_probed: 0`, with no
    observations. The rest of the run (ARP subnets) is unchanged, and `Scan` returns no error
    (FR-010).
  - A router that serves the on-link `192.168.1.0/24`: the run has **one** subnet entry for it
    (`arp`) holding both the ARP and the `router_table` observations, with **one observation per
    IP**: when ARP and the router both see an IP, only the `router_table` observation is kept
    (research R4).
  - Name resolution keeps router hostnames (`hostname_source` stays `router`); DNS/mDNS fill only
    observations without a hostname.
  - The router read runs before the probes and is bounded by its timeout (a hanging fake doesn't
    delay the scan by more than that timeout).
  - Every run produced here passes `contract.Validate`.
  - The router subnet counts toward `contract.MaxSubnets` (16).
- [X] T013 [P] [US1] Write `internal/inventory/apply_router_test.go` (use `inventorytest`
  helpers):
  - A `router_table` observation identifies the device by MAC exactly like `arp`. An earlier
    weak device `ip:192.168.0.0/24:192.168.0.4` (from an `icmp_tcp` run) with the same IP is
    folded into it (`merged_away`), keeping its sightings (US1 AS-2, SC-002).
  - `02:00:5e:…` MACs are flagged randomized; `00:00:5e:…` gets the OUI manufacturer when the
    OUI table has one.
  - Two MACs with the same hostname (`BEAR`) stay two devices.
  - `via` is part of the sighting tuple: `LAN1` then `LAN1` extends one sighting; `LAN1` then
    `LAN2` starts a new one. Three runs that each report the same device the same way give one
    sighting with `seen_count` 3. `Rebuild` reproduces the same sightings, including `via`
    (Principle V).
  - Offline (feature 001 research R7): repeated complete `router_table` scans without a device
    mark it offline after the same number of scans as an `arp` subnet (US1 AS-5, SC-003);
    `complete: false` router scans never count (FR-010).
- [X] T014 [P] [US1] Write `tests/integration/us1_router_test.go`. Through HTTP with a collector
  token:
  - Upload a `valid_routed.json`-style run for `192.168.0.0/24` (IP-only devices), then
    `valid_router.json`. `/devices` lists each online router device once with its MAC,
    manufacturer and hostname, and the earlier IP-only records are gone (merged).
  - The device page shows `via` next to the device's current address on the router's subnet
    (the device page has no sightings list yet; that is feature 001's history work).
  - The `router_table` subnet `192.168.0.0/24` is auto-created like any scanned subnet (it is in
    `new_subnets` of the first upload that carries it, FR-009).
  - Then upload `valid_router_failed.json`: devices are not marked offline by it.
- [X] T015 [P] [US1] Extend `cmd/hne-collector/main_test.go` (`TestConfigLoading` style) for
  `routers[]` (contracts/collector-config-and-cli.md):
  - A valid entry loads.
  - Unknown model, public or hostname URL, missing username or password, or a non-private or
    /31 `subnet` → config error, exit 2, and the error text never contains the password.
  - Add `routerTransport http.RoundTripper` to `environment` (nil in production). With it pointed
    at `routertest.Redirect(fake)`, `scan --once` uploads a run with the router's observations
    and `sources`, and the summary log line has `router=ok`.
  - A router failure keeps the exit code (0 when the upload succeeds, FR-010).
  - `--dry-run` prints the run with `router_table` observations.

### Implementation for User Story 1

- [X] T016 [US1] Write `internal/collect/router/router.go`:
  - `type Secret string` with `String()`/`GoString()` → `"***"`, `MarshalJSON` → `"\"***\""`,
    `LogValue()` → `"***"` and `Reveal() string`. Unmarshal from JSON as a normal string.
  - `type Config struct { Model, URL, Username string; Password Secret; Subnet string }` (JSON
    tags `model`, `url`, `username`, `password`, `subnet`) with `Validate()` and
    `Prefix() netip.Prefix`. Use `contract.IsPrivate` and `contract.ParseSubnet`; never put
    `Password` or `Username` in errors.
  - `type Device struct { IP netip.Addr; MAC, Hostname, Via string; Online bool }`.
  - `type Result struct { Outcome string; Online, Offline int; Observations []contract.Observation }`.
  - `type Source interface { Model() string; Address() netip.Addr; Prefix() netip.Prefix; Read(ctx context.Context) Result }`.
  - `New(cfg Config, rt http.RoundTripper) (Source, error)`: a model registry with one entry,
    `huawei-hg8145v5`. `DefaultTimeout = 10 * time.Second`.
  - `Filter(devices []Device, p netip.Prefix, at time.Time) Result`.
  - `ErrPageNotUnderstood`.

  Make T010 pass
- [X] T017 [US1] Write `internal/collect/router/hg8145v5.go`:
  - The parser `parseHG8145V5`: strip the BOM, decode `\xNN`, read `ProductType`/`isRealmac`,
    pick the array as the page does, and split the quoted arguments of `USERDevice`/
    `USERDeviceNew`. Wrong shape → `ErrPageNotUnderstood`.
  - The client: its own `http.Client` with a cookie jar and the given `RoundTripper`, the
    timeout from `DefaultTimeout` (overridable for tests), and no redirect following (inspect
    `login.cgi`'s body and cookie yourself).
  - Steps and failure mapping exactly as in `contracts/router-hg8145v5.md`. Logout runs in a
    `defer` after a successful login, using a fresh short context so it also runs after
    cancellation.
  - Errors are mapped to outcomes; nothing is logged at all from this package except via the
    returned `Result` (no bodies, cookies or tokens).

  Make T009 and T011 pass
- [X] T018 [US1] Extend `internal/collect/engine.go`:
  - Add `Routers []router.Source` to `Engine`.
  - In `Scan`, read all routers first (concurrently, each bounded by its own timeout), then plan
    the targets. A routed router subnet with an `ok` result is emitted as a `router_table`
    `SubnetScan` and removed from probing. A failed one is emitted as `router_table`,
    `complete: false` (the `extra_subnets` fallback comes in T032). An on-link router subnet
    stays `arp` and gets the router observations too.
  - Append `contract.RunSource` entries to `run.Sources` in config order.
  - In `resolveNames`, skip observations that already have a hostname.
  - Extend `Plan`/`PlannedSubnet` with `Router string` (`"<model> at <address>"`) for `check`.
  - Add `FakeRouter` to `internal/collect/collecttest/fakes.go`.

  Make T012 pass
- [X] T019 [US1] Extend `internal/inventory/apply.go` (`upsertSighting`): include `via` in the
  SELECT, the tuple comparison and the INSERT. Check that `rebuild.go` needs no change, because
  it replays stored runs through `Apply`. Add `Via` to `store.Address` (the `via` of the
  latest sighting of that device on that subnet, in `currentAddresses` in
  `internal/store/devices_query.go`) and show it next to the address in
  `internal/web/templates/device.html` when it is not empty. Make T013 and T014 pass
- [X] T020 [US1] Extend `cmd/hne-collector/main.go`:
  - `config.Routers []router.Config` (`json:"routers"`), validated in `loadConfig` (each entry
    `Validate()`, max 8, error prefix `routers[i]:` without values).
  - `environment.routerTransport`; build `router.New(cfg, env.routerTransport)` sources and set
    `engine.Routers` after `env.newEngine`.
  - Add `router=<outcome>` to the `summary` log line (several routers: comma-separated; none:
    omit).

  Make T015 pass
- [X] T021 [P] [US1] Write `internal/collect/router/hg8145v5_hw_test.go` (`//go:build hwtest`).
  It is skipped unless `HNE_HWTEST_ROUTER_URL`, `HNE_HWTEST_ROUTER_USER` and
  `HNE_HWTEST_ROUTER_PASS` are set. It reads the real router once, asserts `OutcomeOK`, at least
  one online device and lower-case MACs, logs only counts (never names, MACs or credentials),
  and then checks that a second read right after also succeeds (the session was released,
  SC-006)

**Checkpoint**: US1 works end to end with a fake router in CI. It can be shipped as the MVP:
deploy the server first, then the collector (quickstart.md §2 steps 1–5).

---

## Phase 4: User Story 2 - Keep the router login safe and visible (Priority: P2)

**Goal**: the router password provably never leaves the collector; a rejected login is never
retried until the config changes or `check` runs; `check` and the Collectors page show whether
router reads work.

**Independent Test**: run a scan with a marker password and search every output for it (nothing
found); break the password: `check` says "login rejected", the next scan skips the router, and
the Collectors page shows the failure (quickstart.md §2 step 7, SC-004, SC-005).

### Tests for User Story 2 ⚠️ write first, see them fail

- [ ] T022 [P] [US2] Write `cmd/hne-collector/secret_test.go` (SC-004). Configure a router with
  username `user-<random>` and password `pw-<random marker>` against the fake router (via
  `routerTransport`). Run `check`, `check --json`, `scan --once`, `scan --once --dry-run`, a
  failing scan (fake `WrongPassword`) and one `run` iteration (cancel after the first scan).
  Assert that neither marker appears in stdout, stderr, `hne-collector.log*`, any file under
  `spool/` (including `rejected/`), the `hne-collector.router-rejected` marker file (it holds a
  hash only), or any request body received by the fake upload server. Also load a config with a
  bad router entry and assert the config error doesn't echo the password
- [ ] T023 [P] [US2] Add rejection-marker and `check` tests to `cmd/hne-collector/main_test.go`
  (research R6, FR-011, FR-012, contracts/collector-config-and-cli.md):
  - `scan --once` with fake `WrongPassword` → outcome `login_rejected`, and
    `hne-collector.router-rejected` is written next to the config with the SHA-256 of
    (model, url, username, password, subnet).
  - The next `scan --once` makes **zero** requests to the fake router and reports
    `skipped_after_rejection` in `sources`.
  - Same for `locked`. `session_busy`, `unreachable` and `page_not_understood` write no marker and
    are retried next scan.
  - Changing the password in the config → the next scan tries again (hash differs).
  - `check` deletes the marker and makes exactly one read attempt. It prints the `Routers:` section
    with the exact phrases from the contract (`OK, N online / M offline devices listed`,
    `unreachable`, `login rejected (router skipped until its config changes or you run check)`,
    `locked by the router`, `busy (someone is logged into the router)`,
    `page not understood (firmware?)`), and lists the router subnet as
    `router huawei-hg8145v5 at 192.168.0.1`.
  - `check --json` logs one `router read` JSON record per router with `model`, `address`,
    `subnet`, `outcome`, `online`, `offline` only (`--json` means one JSON object per line).
  - Exit codes: a router failure never changes the `check` or `scan` exit code.
- [ ] T024 [P] [US2] Extend `internal/ingest/ingest_test.go`: ingesting `valid_router.json`
  stores one `run_sources` row per source (`idx` in order) inside the same transaction as
  `run_subnets`. A run without `sources` stores none, and a duplicate upload stores nothing new
- [ ] T025 [P] [US2] Write `tests/integration/us2_router_status_test.go`:
  - Collector `desktop` uploads `valid_router.json`: `/collectors` shows `huawei-hg8145v5 at
    192.168.0.1: OK, 14 online / 16 offline`.
  - It then uploads `valid_router_failed.json`: the page shows `login rejected` for the latest
    run only.
  - A collector that never sent sources shows `—`.
  - Then read the whole SQLite file and the raw stored runs and assert that no `password` or
    `username` key exists in any stored run (FR-006).

### Implementation for User Story 2

- [ ] T026 [US2] Extend `internal/ingest/ingest.go`: insert `run.Sources` into `run_sources` next
  to the `run_subnets` insert. Make T024 pass
- [ ] T027 [US2] Extend `internal/store/collectors.go`: add `Routers []RouterStatus` to
  `CollectorOverview` (`Model`, `Address`, `Outcome`, `Online`, `Offline`) from the
  `run_sources` rows of the collector's latest run (same "latest run" subquery as `Subnets`).
  Add a **Router (latest run)** column to `internal/web/templates/collectors.html`, using the
  owner-facing phrases for every outcome: `OK, N online / M offline`, `unreachable`,
  `login rejected`, `locked by the router`, `busy (someone is logged into the router)`,
  `page not understood (firmware?)`, `skipped: login was rejected, fix the password and run
  check`; `—` when there are none. Failure outcomes are styled as warnings like the skew flag. Make T025 pass
- [ ] T028 [US2] Extend `cmd/hne-collector/main.go` with the rejection marker:
  - `hne-collector.router-rejected` next to the config holds one hex SHA-256 per line.
  - `routerHash(cfg router.Config)` hashes model, url, username, `Password.Reveal()` and subnet,
    joined with `\x00`.
  - Before each scan, a router whose hash is listed is wrapped with `router.Skipped(src)` (add it
    to `router.go`: returns `OutcomeSkippedAfterRejection` without any I/O).
  - After a scan, add the hashes of `login_rejected`/`locked` routers (write-temp + rename).
  - Make T023's marker cases pass
- [ ] T029 [US2] Extend `check` in `cmd/hne-collector/main.go`:
  - Delete the marker, read each router once, and print the `Routers:` section. Show router
    subnets in `Subnets to scan:` from `PlannedSubnet.Router`.
  - Log a `router read` record per router (shown as JSON with `--json`; outcomes and counts only).
  - Record a rejection again if the `check` read is rejected.
  - Make T022 and the remaining T023 cases pass

**Checkpoint**: US1 and US2 work. Run quickstart.md §2 step 7 against the fake in CI, and
SC-004's search.

---

## Phase 5: User Story 3 - Scan routed subnets without losing auto-discovery (Priority: P3)

**Goal**: `extra_subnets` adds routed (or on-link) subnets to auto-discovery or to the fixed list.
For a router's subnet, extras provide the ICMP/TCP fallback when the router read fails.

**Independent Test**: add `192.168.0.0/24` to `extra_subnets`, run `check` and `scan --once`:
on-link subnets and `192.168.0.0/24` are all covered; a newly attached adapter is still picked up
(SC-007).

### Tests for User Story 3 ⚠️ write first, see them fail

- [ ] T030 [P] [US3] Write `internal/collect/engine_extra_test.go` (FR-015, FR-016, SC-007,
  research R4/R7). Add `Extra []netip.Prefix` to the `ScanOptions` used in the tests.
  - No `Targets`, `Extra = [192.168.0.0/24]`: the run covers every on-link subnet (`arp`) plus
    `192.168.0.0/24` (`icmp_tcp`).
  - An extra that is on-link (`192.168.1.0/24`) appears once, as `arp`.
  - Fixed `Targets` plus `Extra`: the union, each subnet once.
  - Changing the fake vantage to add `10.20.30.0/24` adds it to the next plan with extras still
    present (SC-007).
  - An ignored extra is reported `skipped`/`ignored`.
  - Extras and router subnets count toward `MaxSubnets`; when the list is cut, on-link subnets
    are kept first.
  - With a router on `192.168.0.0/24` and the same subnet as an extra: read `ok` → `router_table`
    only, with no ICMP/TCP probes; read failed → `icmp_tcp` probe for that subnet with
    `complete: false` (presence only, never drives offline detection, FR-010) and the source still
    reports the failure.
  - `Plan` reports the extra with its method and, for a router subnet, `Router` plus a fallback
    marker.
- [ ] T031 [P] [US3] Extend `cmd/hne-collector/main_test.go`:
  - `extra_subnets` loads from the file, and env `HNE_EXTRA_SUBNETS` (comma-separated) overrides
    it.
  - A public (`8.8.8.0/24`), too-wide (`10.0.0.0/8`) or too-narrow (`/31`) extra → config error,
    exit 2, with a clear message (US3 AS-3).
  - `check` lists extras: `192.168.0.0/24     routed: ICMP/TCP presence only, no MACs`, or with a
    router `router huawei-hg8145v5 at 192.168.0.1 (fallback: ICMP/TCP)` (FR-017).

### Implementation for User Story 3

- [ ] T032 [US3] Extend `internal/collect/engine.go`:
  - Add `ScanOptions.Extra`. `planTargets` builds the base list (auto-discovered or `Targets`),
    then adds each extra not already present: on-link → ARP target with its interface, otherwise
    `icmp_tcp`. Apply ignore and MaxSubnets as today.
  - R4 fallback: a router subnet that is also an extra is probed with ICMP/TCP only when its read
    failed, and that scan is reported `complete: false`.
  - When cutting to `MaxSubnets`, keep on-link subnets first.
  - `Plan` gets the same `Extra` and marks `Fallback bool` for a router subnet that is also an
    extra.

  Make T030 pass
- [ ] T033 [US3] Extend `cmd/hne-collector/main.go`:
  - `config.ExtraSubnets []string` (`json:"extra_subnets"`), parsed with `contract.ParseSubnet`
    into `cfg.extraPrefixes`, with error prefix `extra_subnets:`, plus the `HNE_EXTRA_SUBNETS`
    override.
  - Pass `Extra` to `Scan` and `Plan`.
  - Print the extra lines and `(fallback: ICMP/TCP)` in `check`.

  Make T031 pass

**Checkpoint**: all three stories work. A failed router read on a configured extra subnet still
gives presence data for that subnet.

---

## Phase 6: Polish & Cross-Cutting Concerns

- [ ] T034 [P] Update `specs/001-lan-inventory-topology/contracts/collector-cli.md` with
  `extra_subnets`, `HNE_EXTRA_SUBNETS`, `routers[]`, the `check` `Routers:` section, the
  `router=` summary field and the rejection-marker file, linking to
  `specs/002-router-device-lists/contracts/collector-config-and-cli.md`. Also update
  `specs/001-lan-inventory-topology/data-model.md` with pointers to the 002 additions
  (`run_sources`, `sightings.via`, `router_table`)
- [ ] T035 [P] Security pass on `internal/collect/router` and the collector:
  - Grep for any `slog`/`fmt` call that could print a `Config`, a response body, a cookie or a
    token.
  - Check that `http.Client` doesn't follow redirects to another host, that response bodies are
    capped (`io.LimitReader`, 1 MiB), and that only the five allowed requests exist (FR-004).
  - Check that the marker file and config are created with `0600` on Linux.
  - Fix anything found, with a test
- [ ] T036 Push the branch and get CI green: `make lint` (gofmt, vet, no subnet literals),
  `make test`, `make test-contract`, `make test-integration`, `test-windows`, `contract-lint`,
  image build and `deploy/smoke.sh`. The footer shows `0.3.<run>`
- [ ] T037 Run quickstart.md §2 on the real network:
  1. Deploy the server first (`.\deploy.ps1 -Pull`).
  2. Download the new Windows collector.
  3. Add `extra_subnets` and the router entry (new password).
  4. Run `check` and `scan --once`, and compare the router's *User Device Information* page with
     **Devices** (SC-001, SC-002).
  5. Log into the router right after a scan (SC-006).
  6. Run the wrong-password scenario (step 7, SC-005).
  7. Optionally run the `hwtest` (T021).

  Record the results and any firmware differences in `research.md` R1
- [ ] T038 Set `spec.md` **Status** to `Implemented` and add "Implementation notes" to this file
  for any decision made during implementation (like 001's tasks.md)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: T001 is done; T002 has no dependencies.
- **Foundational (Phase 2)**: depends on Setup. It blocks every story: the engine emits
  `router_table`/`sources` (needs T007), and the server stores `via`/`run_sources` (needs T008).
- **US1 (Phase 3)**: depends on Phase 2. It is the MVP.
- **US2 (Phase 4)**: depends on US1 (router sources, `environment.routerTransport`, the fake
  router).
- **US3 (Phase 5)**: depends on Phase 2. The plain `extra_subnets` planning (T030 first bullets,
  T031, T033) is independent of US1. The router fallback cases of T030/T032 need T018.
- **Polish (Phase 6)**: after the stories you ship.

### Story order

US1 → US2 → US3 in priority order. US3 can run in parallel with US2 once T018 is done.

### Within each story

Tests first (red in CI) → package code → engine → collector CLI / server → story checkpoint.

### Shared-file notes (tasks that are not [P] for this reason)

- `internal/collect/engine.go`: T018 → T032.
- `cmd/hne-collector/main.go`: T020 → T028 → T029 → T033.
- `cmd/hne-collector/main_test.go`: T015, T023, T031 add separate test functions; write them in
  that order to avoid conflicts.
- `internal/collect/collecttest/fakes.go`: T012 (test needs) and T018 (adds `FakeRouter`). Write
  `FakeRouter` together with T012 if the test can't compile without it.
- `internal/contract/v1.go` / `validate.go`: only T007.

---

## Parallel Examples

### Phase 2

```text
T003 contract fixtures  |  T004 validate tests  |  T005 seed_2 + migration test
then T006 (OpenAPI) and T007 (Go contract) in parallel, T008 (migration)
```

### User Story 1

```text
Tests together: T009 parser | T010 config/registry/secret | T011 fake router + protocol |
                T012 engine | T013 inventory | T014 integration | T015 CLI config
Then: T016 → T017 (router package) ; T019 (inventory, parallel with T016/T017) ;
      T018 (engine, after T016) → T020 (CLI) ; T021 any time after T017
```

### User Story 2

```text
Tests together: T022 secrets | T023 marker/check | T024 ingest | T025 Collectors page
Then: T026 → T027 (server)  in parallel with  T028 → T029 (collector)
```

### User Story 3

```text
Tests together: T030 engine extras | T031 CLI extras
Then: T032 → T033
```

---

## Implementation Strategy

### MVP first (User Story 1)

1. Phase 2 (contract + migration), deployed to the NAS first. Old collectors keep working.
2. US1. A new collector with a router entry fills in `192.168.0.0/24` with real MACs.
3. Stop and validate with quickstart.md §2 steps 1–6 before going on.

### Incremental delivery

1. US1 → ship (router devices in the inventory).
2. US2 → ship (lockout protection, `check` and Collectors-page visibility, proof that no secrets
   leak).
3. US3 → ship (`extra_subnets`, plus fallback presence when the router read fails).

Each step bumps only the CI run number. `VERSION` stays `0.3` for the whole feature.

---

## Notes

- [P] tasks touch different files and don't depend on unfinished tasks.
- Commit after each task or logical group. Test commits come before implementation commits so
  CI shows red, then green.
- Never commit a real router capture, MAC, hostname or password. Only the anonymized fixture
  (T001) and hand-made variants inside tests.
- Deploy order matters (research R3): server first, then collectors.
