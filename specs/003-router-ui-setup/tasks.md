---

description: "Task list for Router Setup from the Web UI"
---

# Tasks: Router Setup from the Web UI

**Input**: Design documents from `specs/003-router-ui-setup/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: REQUIRED (Constitution Principle II). Every test task comes before the implementation
it covers and must be seen failing in CI before that implementation is pushed.

**Organization**: tasks are grouped by user story (US1–US3 from spec.md).

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on unfinished tasks)
- **[Story]**: US1–US3 from spec.md
- Paths are relative to the repository root

## Conventions for every task

- Conventions of `specs/001-lan-inventory-topology/tasks.md` and
  `specs/002-router-device-lists/tasks.md` apply (Go ≥ 1.26, UTC RFC 3339 ms, lower-case MACs, no
  IPv4 range literals in non-test Go code outside `internal/contract/private.go`, no new
  dependencies, nothing built locally: tests run in GitHub Actions via the PR).
- **Secrets**: a router password stored in `router_settings` is never rendered in HTML (no
  `value=`, no data attribute, no script), never in a collector config, never logged by the
  server or the collector, never written by the collector to any file (config, spool, log,
  `hne-collector.routers`, rejection marker), never uploaded. Tests use unique marker passwords
  and search for them.
- **Password storage is plain text** (owner decision, spec Security TODO-SEC-1). Do not add
  encryption in this feature.
- Write files with shell-safe tooling: escape sequences such as `﻿` and `\x00` must stay
  escapes in Go source (use `[]byte{0}` for separators).
- Owner edits use the existing session + Origin protections of `internal/web` (feature 001).

---

## Phase 1: Setup

- [ ] T001 Set `VERSION` to `0.4` and add `hne-collector.routers` to `.gitignore`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the database table, the additive contract changes and the store API that every story
uses.

**⚠️ CRITICAL**: no user story work can begin until this phase is complete

### Tests first

- [ ] T002 [P] Write `internal/store/testdata/seed_3.sql` (sample rows at schema v3, including a
  `run_sources` row with outcome `ok`, a device with two current `device_addresses` on two
  subnets, and `user_device_attrs` type `router` for it). Add `TestMigration0004` to
  `internal/store/store_test.go`: after migrating 3 → 4 the `run_sources` row survives;
  `run_sources` accepts outcome `login_unavailable` and still rejects `rebooted`;
  `router_settings` exists with `identity_key` UNIQUE; add `router_settings` to `expectedTables`
- [ ] T003 [P] Contract tests:
  - `internal/contract/validate_test.go`: a `sources[]` entry with outcome `login_unavailable`
    (online/offline 0) is valid; with counts > 0 it is invalid.
  - `tests/contract/fixtures/valid_router_login_unavailable.json` (copy of
    `valid_router_failed.json` with outcome `login_unavailable` and a new `collection_id`),
    registered in `fixtureCodes`.
  - `tests/contract/schema_test.go`: new `TestPingAndLoginSchemas`: a `PingResponse` with
    `routers: [{id: 1, model: "huawei-hg8145v5", address: "192.168.0.1", subnet:
    "192.168.0.0/24"}]` validates against `PingResponse`; one with an extra `password` field in a
    router entry fails (`additionalProperties: false`); `{"username": "root", "password": "x"}`
    validates against `RouterLogin`; an empty `username` fails (`minLength: 1`)
- [ ] T004 [P] Write `internal/store/routers_test.go` (with `storetest` and
  `inventorytest` to create devices):
  - `SaveRouter(ctx, identityKey, RouterSettings{Model, Subnet, Username, Password}, at)` inserts,
    then replaces (same `id` kept); an empty `Password` keeps the stored one; an empty password
    with no stored one is an error.
  - `GetRouterSettings(identityKey)` returns model, subnet, username and `PasswordSet bool`, and
    **no password field at all** (the struct has none).
  - `DeleteRouter(identityKey)` removes the row.
  - `ListRouters` resolves the address (data-model "Resolved router"): preferred subnet when set;
    otherwise the current address on the subnet with the device's latest sighting `last_seen`
    (test: two subnets where the address that changed most recently is **not** the one seen most
    recently: the seen one wins); non-private addresses excluded; a
    device with no current address excluded; a device merged away resolves to the surviving
    device's addresses; result `id`, `model`, `address`, `subnet` (the address's subnet CIDR).
  - `RouterLogin(ctx, id)` returns username and password; unknown id returns `ErrNotFound`.
  - `RouterStatus(ctx, address)` returns the latest `run_sources` outcome per collector for that
    address (collector name, outcome, online, offline, run finished_at).
  - `inventory.Rebuild` leaves `router_settings` untouched.

### Implementation

- [ ] T005 Write `internal/store/migrations/0004_router_settings.sql`:
  - Rebuild `run_sources` (same columns as 0003; outcome CHECK adds `'login_unavailable'`;
    copy rows; drop; rename; PRIMARY KEY `(collection_id, idx)`).
  - `CREATE TABLE router_settings (id INTEGER PRIMARY KEY, identity_key TEXT NOT NULL UNIQUE,
    model TEXT NOT NULL, subnet TEXT NOT NULL DEFAULT '', username TEXT NOT NULL CHECK
    (length(username) BETWEEN 1 AND 128), password TEXT NOT NULL CHECK (length(password) BETWEEN
    1 AND 128), updated_at TEXT NOT NULL)` with a comment that the password is plain text by owner
    decision (spec TODO-SEC-1). Make T002 pass
- [ ] T006 Extend `internal/contract/v1.go`/`validate.go`: `OutcomeLoginUnavailable =
  "login_unavailable"` (in `ValidOutcome`); `PingResponse.Routers []RouterRef
  \`json:"routers,omitempty"\``; types `RouterRef{ID int64; Model, Address, Subnet string}` and
  `RouterLogin{Username, Password string}` with JSON tags from data-model.md. Edit the canonical
  `specs/001-lan-inventory-topology/contracts/collector-upload-api.yaml` exactly as in
  `contracts/api-changes.md` (schemas, the new path, the outcome, the deploy-order note). Make T003
  pass (the `contract-lint` job must stay green)
- [ ] T007 Write `internal/store/routers.go` (`RouterSettings`, `RouterView` without password,
  `SaveRouter`, `GetRouterSettings`, `DeleteRouter`, `ListRouters`, `RouterLogin`,
  `RouterStatus`). Use `contract.IsPrivateAddr` for addresses. Make T004 pass

**Checkpoint**: schema v4 and the store API exist; nothing is visible yet.

---

## Phase 3: User Story 1 - Configure a router from its device page (Priority: P1) 🎯 MVP

**Goal**: on a device of type "router", the owner sets model, username, password (and subnet when
the device has several addresses); the password is never shown again.

**Independent Test**: set type "router", fill the Router card, save; reopen: model, address and
username shown, "Password: set", password absent from the HTML.

### Tests for User Story 1 ⚠️ write first, see them fail

- [ ] T008 [P] [US1] Write `tests/integration/us1_router_ui_test.go` (owner logged in, device
  created by uploading `valid_router.json`, router device `192.168.0.4` with type set through
  `POST /devices/{id}/attrs`):
  - The device page shows the Router card only when the type is `router`, with a model select
    listing `huawei-hg8145v5`.
  - `POST /devices/{id}/router` with `model`, `username` `root`, `password` = a unique marker →
    303; the page then shows "Password: set", `root` and the address
    `192.168.0.4 (192.168.0.0/24)` (the full password sweep of all pages is T027).
  - Posting again with an empty password keeps it (`store.RouterLogin` still returns the marker);
    a new password replaces it.
  - Validation → 400 with a message: unknown model; empty username; no password when none is
    stored; username or password over 128 characters; a `subnet` that is not one of the device's
    current subnets.
  - Device with no current private address: the card says so and saving is refused.
  - `POST /devices/{id}/router/remove` deletes it; saving type `printer` via `/attrs` deletes it.
  - Without a session: 303 to login (no change); with a foreign `Origin`: 403 (feature 001
    protections).

### Implementation for User Story 1

- [ ] T009 [US1] Extend `internal/web/pages.go`: `deviceData` gets `Router *store.RouterView`,
  `RouterModels []string` (from `router.Models()`), `RouterSubnets []string` (the device's current
  subnets), `RouterStatus` (from `store.RouterStatus`, used by US2 too) and `RouterError string`.
  Add `POST /devices/{id}/router` and `POST /devices/{id}/router/remove` in
  `internal/web/server.go` (owner-only, like `/attrs`), writing through
  `s.opts.Ingester.Do` so they serialize with ingest. In `handleDeviceAttrs`, a `type` change away
  from `router` also calls `store.DeleteRouter` in the same transaction
- [ ] T010 [US1] Add the Router card to `internal/web/templates/device.html` exactly as in
  `contracts/web-ui-changes.md` (password input `type=password`, never prefilled,
  `autocomplete=new-password`; subnet select only when there are several subnets; latest read
  per collector; Remove button). Make T008 pass

**Checkpoint**: routers can be configured in the UI (not yet used by collectors).

---

## Phase 4: User Story 2 - Collectors use UI-configured routers (Priority: P1)

**Goal**: at each scan every collector (remote and built-in) learns the router list from the
server, fetches each login before the read and reads the router; new configs list the routers.

**Independent Test**: configure a router in the UI, run one scan of an existing collector (no new
download) against the fake router: its devices appear; change the password in the UI: the next
scan uses it.

### Tests for User Story 2 ⚠️ write first, see them fail

- [ ] T011 [P] [US2] Extend `tests/contract/server_upload_test.go` (or a new
  `tests/contract/router_api_test.go`): with one router in `router_settings` whose device has a
  current address, `GET /api/v1/ping` returns it in `routers` (schema-valid, no credential keys);
  `GET /api/v1/routers/{id}/login` with the collector token returns `RouterLogin` with the stored
  values and `Cache-Control: no-store`; unknown id → 404 `router_not_found` (schema `Error`)
- [ ] T012 [P] [US2] Extend `tests/integration/us2_collector_test.go`: creating a collector after
  a router is configured shows a config whose `routers` is
  `[{"model":"huawei-hg8145v5","url":"http://192.168.0.4"}]` with no `username`/`password` keys,
  and the page HTML does not contain the stored password
- [ ] T013 [P] [US2] Write `internal/collect/router/remote_test.go`: `NewRemote(model, address,
  prefix, login LoginFunc, rt)`; `Read` calls `login` once, then reads the `routertest` fake with
  those credentials (outcome `ok`, 13/16); a `login` error gives `login_unavailable` and no
  request to the router; the `Remote` value printed with `%v`/`%+v`/`%#v` and logged never shows
  the password; `Model/Address/Prefix` as given
- [ ] T014 [P] [US2] Write `cmd/hne-collector/server_routers_test.go` (fake server answers
  `routers` in ping and serves `/api/v1/routers/{id}/login`; fake router via `routerTransport`):
  - A config with **no** `routers` reads the server's router: upload has a `sources` entry `ok`
    and `router_table` observations; the fake server saw one login request per scan.
  - The router list is cached in `hne-collector.routers` (no credentials in it); with the server
    down, `scan --once` uses the cache, the read reports `login_unavailable`, the rest of the run
    is spooled (exit 5 as before).
  - Login endpoint 404 → `login_unavailable`.
  - A config entry `{model, url}` without credentials is accepted; one with only `username` is a
    config error (exit 2).
  - A config entry **with** credentials for the same address wins: no login request for that
    router, and the fake router sees the file's credentials.
  - `check` prints `huawei-hg8145v5 at 192.168.0.1 (login from server): OK, 13 online / 16
    offline devices listed` and `(login from config)` for file credentials; `login unavailable
    from the server` when the login can't be fetched.
- [ ] T015 [P] [US2] Extend `internal/app` tests (new `internal/app/scanner_router_test.go`): the
  built-in scanner with a router in `router_settings` and a `routertest` fake (transport injected
  through `app.Options`) reads it and stores a `run_sources` row `ok`. With the fake router in
  `WrongPassword` mode, two built-in scans make **one** login attempt and the second reports
  `skipped_after_rejection`; after `store.SaveRouter` with a new password the next scan tries
  again (FR-014 for the built-in collector, research R8)
- [ ] T016 [P] [US2] Write `tests/integration/us2_router_ui_flow_test.go` (SC-001, SC-003, server
  half; the collector half is T014, so SC-001 end to end is T014 + T016). `cmd/hne-collector` is a
  `main` package and can't be imported from `tests/`, so drive the flow over HTTP: ping returns
  the router, the login endpoint returns the credentials, an upload of a run with that router's `router_table` data
  makes its devices appear; after changing the password in the UI the login endpoint returns the
  new one
- [ ] T017 [P] [US2] Extend `cmd/hne-collector/rejection_test.go`: a server-managed router with a
  wrong password (fake router `WrongPassword`) → `login_rejected`, marker written; next scan: one
  login **fetch** from the server but **no** request to the router (`skipped_after_rejection`);
  the server then serves a different password → the next scan contacts the router again (no
  `check` needed); `check` still clears the marker

### Implementation for User Story 2

- [ ] T018 [US2] Add to `internal/web/api.go`: `routers` in `handlePing` from
  `store.ListRouters`; `GET /api/v1/routers/{id}/login` (bearer collector auth like ping, revoked
  refused, `Cache-Control: no-store`, 404 `router_not_found`), registered in
  `internal/web/server.go`. Never log the response. Make T011 pass
- [ ] T019 [US2] Extend `internal/web/collectors.go`: `collectorConfig` gets `Routers
  []configRouter \`json:"routers"\`` (`model`, `url` = `http://<address>`), filled from
  `store.ListRouters` when a collector is created. Make T012 pass
- [ ] T020 [US2] Write `internal/collect/router/remote.go` (`LoginFunc func(ctx) (username
  string, password Secret, err error)`, `NewRemote`, `Remote` implementing `Source`; the login is
  a local variable of `Read` only). Relax `Config.Validate` in `router.go`: username and password
  are required **as a pair**; both empty marks a server-managed entry (`Config.ServerManaged()`).
  Make T013 pass
- [ ] T021 [US2] Extend `cmd/hne-collector/main.go` and `routers.go`:
  - `upload.Client` gets `RouterLogin(ctx, id) (contract.RouterLogin, error)`; the ping result's
    `Routers` is kept for the scan and cached in `hne-collector.routers` (write-temp + rename,
    0600); on ping failure the cache is used.
  - Before each scan, build `engine.Routers`: file entries with credentials (feature 002) plus a
    `router.NewRemote` per server router whose address no credentialed file entry has; file
    entries without credentials are used only when there is neither a ping answer nor a cache.
  - `checkPhrase` adds `login unavailable from the server`; `check` prints `(login from server)` /
    `(login from config)` after the router name.
  - Make T014 pass
- [ ] T022 [US2] Add `router.LoginHash(model, address, subnet, username, password string) string`
  to `internal/collect/router/router.go` (SHA-256 over the fields separated by `[]byte{0}`), shared
  by the remote and built-in collectors. In `cmd/hne-collector/routers.go`, compute the marker hash
  of a server-managed router from model, address, subnet and the **fetched** username and password
  (research R6):
  the `Remote` source gets a `skip func(hash string) bool` hook (or the `LoginFunc` wrapper does
  the check) so a rejected login is never retried with the same credentials, and
  `recordRejections` records that hash. Make T017 pass
- [ ] T023 [US2] Extend `internal/app/scanner.go` (+ `app.Options.RouterTransport`): before each
  built-in scan, set `engine.Routers` to `router.NewRemote` sources from `store.ListRouters`,
  with a `LoginFunc` reading `store.RouterLogin`. Keep the built-in rejected login hashes in the
  `router_rejected_builtin` setting (research R8): check before contacting a router, add after
  `login_rejected`/`locked`, using `router.LoginHash` (T022). Make T015 and T016 pass
- [ ] T024 [US2] Add the phrase `login unavailable from the server` for `login_unavailable` to
  `routerOutcome` in `internal/web/server.go`, and show `deviceData.RouterStatus` in the Router
  card (latest outcome per collector)

**Checkpoint**: US1 + US2 deliver the feature end to end.

---

## Phase 5: User Story 3 - Keep the login safe and the router unlocked (Priority: P2)

**Goal**: verify that the password appears nowhere it shouldn't and that only collector tokens
get it. (Wrong-password protection is part of US2: T015, T017, T022, T023.)

**Independent Test**: marker-password search over pages, configs, collector files, output and
uploads finds nothing; requests without a valid collector token get 401.

### Tests for User Story 3 ⚠️ write first, see them fail

- [ ] T025 [P] [US3] Write `tests/contract/router_login_auth_test.go` (SC-005): the login
  endpoint returns 401 `invalid_token` for no token, an unknown token, a revoked collector's
  token, and a request carrying only the owner's browser session cookie; the body never contains
  the password
- [ ] T026 [P] [US3] Extend `cmd/hne-collector/secret_test.go`: a server-managed router whose
  server password is a unique marker; run `check`, `check --json`, `scan --once`, a rejected
  scan, a server-down scan and one `run` iteration; the marker is in no output, no file in the
  collector directory (including `hne-collector.routers` and the rejection marker) and no upload
- [ ] T027 [P] [US3] Extend `tests/integration/us1_router_ui_test.go`: with a marker password
  stored, fetch `/`, `/devices`, the device page, `/collectors` (including a newly created
  collector's config) and `/settings`: the marker appears in none; the server's log output
  (capture `app.Options.Log` into a buffer) doesn't contain it after a login fetch

### Implementation for User Story 3

- [ ] T028 [US3] Fix whatever T025, T026 and T027 reveal (expected: nothing beyond the code above);
  record any finding in the Implementation notes below

---

## Phase 6: Polish & Cross-Cutting Concerns

- [ ] T029 [P] Update `specs/001-lan-inventory-topology/contracts/collector-cli.md` and
  `specs/002-router-device-lists/contracts/collector-config-and-cli.md` with server-managed
  routers, `hne-collector.routers` and the `check` source labels; update
  `specs/001-lan-inventory-topology/contracts/web-ui.md` with the Router card and routes
- [ ] T030 Get CI green on the PR (lint, unit, contract, integration, Windows collector, build,
  image + smoke)
- [ ] T031 Run quickstart.md §2 on the real network (server first, then collector; UI router;
  wrong-password flow; password search) and record the results
- [ ] T032 Set spec.md **Status** to `Implemented`, add Implementation notes here, and remind the
  owner of the deferred Security TODOs (TODO-SEC-1..5)

---

## Dependencies & Execution Order

- **Setup (T001)** → **Foundational (T002–T007)** → stories.
- **US1 (T008–T010)** depends on Foundational only.
- **US2 (T011–T024)** depends on Foundational; its end-to-end test (T016) and the Router card
  status (T024) need US1's routes. It includes wrong-password protection for the remote (T017,
  T022) and built-in (T015, T023) collectors, so it can ship alone.
- **US3 (T025–T028)** depends on US2 and only adds verification (auth refusal, password sweeps).
- **Polish** after the stories.

### Shared-file notes

- `internal/web/server.go`: T009 (routes) → T018 (API route) → T024 (phrase).
- `cmd/hne-collector/routers.go`: T021 → T022.
- `internal/collect/router/router.go`: T020 → T022.

---

## Parallel Examples

```text
Phase 2 tests:  T002 | T003 | T004      then T005, T006 (parallel), T007
US2 tests:      T011 | T012 | T013 | T014 | T015 | T016 | T017
US2 code:       T018 → T019 ; T020 → T021 → T022 ; T023 (after T022) ; T024
US3 tests:      T025 | T026 | T027
```

---

## Implementation Strategy

1. Foundational, then US1: routers can be set up in the UI (shippable, harmless).
2. US2: collectors use them, with wrong-password protection on the remote and built-in
   collectors (deploy the server first, then collectors).
3. US3: security verification; ship.

---

## Notes

- Tests first, red in CI, then the implementation.
- The Security TODOs in spec.md are deliberately out of scope.
