---

description: "Task list for Maintenance Actions"
---

# Tasks: Maintenance Actions

**Input**: Design documents from `specs/004-maintenance-actions/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: REQUIRED (Constitution Principle II). Every test task comes before the implementation
it covers and must be seen failing in CI before that implementation is pushed.

**Organization**: tasks are grouped by user story (US1–US4 from spec.md).

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on unfinished tasks)
- **[Story]**: US1–US4 from spec.md
- Paths are relative to the repository root

## Conventions for every task

- Conventions of `specs/001-lan-inventory-topology/tasks.md` … `specs/003-router-ui-setup/tasks.md`
  apply (Go ≥ 1.26, UTC RFC 3339 ms via `store.FormatTime`, no new dependencies, nothing built
  locally: tests run in GitHub Actions via the PR; pull `--rebase` before pushing because the
  `autofix` job commits gofmt fixes).
- **All-or-nothing, serialized with ingest**: every destructive action is a store function taking
  a `*sql.Tx`, called by the web handler inside `s.opts.Ingester.Do` (FR-004).
- **Rebuild invariant**: every store test of an action ends by running
  `inventory.RebuildTx` and comparing the projections with those before the rebuild (FR-007).
- Owner routes use the existing session + Origin/`Sec-Fetch-Site` protections of `internal/web`.
- Integration test files of this feature are prefixed `maint_` (the `us1_…` names belong to
  earlier features).
- Write files with shell-safe tooling: escape sequences must stay escapes in Go source.

## Implementation notes (recorded during /speckit-implement)

- /speckit-analyze finding U1 was fixed before coding: the discard rule compares the run's own
  `started_at` with the reset point, without skew correction, because collectors set `sent_at`
  before spooling (research R2). C1 (all-or-nothing test with a failing trigger), C2 (Settings
  shows the paused state) and I1 (interface change) were applied to the tasks.
- The store functions are named `RemovalCounts` (not `Counts`, which already exists for the home
  page). `store.SettingOwnerPassword` now names the owner password key used by `internal/auth`.
- The device page does not name collectors, so "(removed)" shows where history names one: the
  subnet "Discovered by" / "By" columns, skipped subnets and the device page's router reads.
- The scanner unit test does not cover "pause during a running scan" (no way to block the fake
  engine mid-scan); the code only stops the next scan, so the running one always completes.
- There is no README; the "back up the data volume first" note is on the Maintenance card and in
  `quickstart.md` §2.

---

## Phase 1: Setup

- [X] T001 Set `VERSION` to `0.5`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the Maintenance card, its notice and the confirmation helper that every story uses.

**⚠️ CRITICAL**: no user story work can begin until this phase is complete

### Tests first

- [X] T002 [P] Write `tests/integration/maint_card_test.go`: as the owner, `GET /settings` shows a
  section `id="maintenance"`; `GET /settings?done=devices&devices=4&runs=7` shows
  `Removed 4 devices and 7 scan records.`; `?done=collectors&collectors=2` shows
  `Removed 2 collectors.`; `?done=everything&devices=1&runs=2&collectors=3` shows
  `Dropped all data: 1 devices, 2 scan records, 3 collectors.`; `?done=paused` /
  `?done=resumed` show `Built-in scanner paused.` / `Built-in scanner resumed.`;
  `?done=<script>` and `?done=devices&devices=x` show no notice and never echo the value

### Implementation

- [X] T003 Add `internal/web/maintenance.go`: `maintenanceNotice(q url.Values) string` (only known
  `done` values; counts parsed with `strconv.Atoi`, a non-integer drops the notice),
  `confirmed(r, word) bool` (`strings.EqualFold(strings.TrimSpace(r.PostFormValue("confirm")),
  word)`), and `logMaintenance(action string, counts ...any)` writing one `slog` Info line
  `maintenance` with `action` and the counts. Extend `settingsData` in `internal/web/pages.go`
  with `Notice string`, `Paused bool`, `PausedAt time.Time`; add the "Maintenance" card to
  `internal/web/templates/settings.html` exactly as in `contracts/web-ui-changes.md` (forms post
  to the routes of later phases; the scanner line only when `BuiltinEnabled`). Make T002 pass

**Checkpoint**: the card is visible; its buttons don't work yet (404).

---

## Phase 3: User Story 1 - Start the inventory over (Priority: P1) 🎯 MVP

**Goal**: "Remove all devices" deletes devices, all history and all device edits; old results
never come back; subnet names and ignore choices survive.

**Independent Test**: with devices, history and edits, type `devices` and submit: Devices empty;
a rebuild is still empty; the next scan brings devices back without names; a run started before
the removal and uploaded afterwards is `discarded`.

### Tests for User Story 1 ⚠️ write first, see them fail

- [X] T004 [P] [US1] Write `internal/store/maintenance_test.go` `TestRemoveAllDevices` (with
  `storetest`, a real `inventory.Applier` and two ingested runs from two collectors, a
  `user_device_attrs` name and type, a `user_links`/`user_acks`/`user_identity_alias` row, a
  `router_settings` row, `router_rejected_builtin` set, a `user_subnet_attrs` name and
  `ignored=true`, an owner password and a session): `RemoveAllDevices(ctx, tx, at)` returns
  `Counts{Devices: <non-merged devices>, Runs: 2}`; afterwards `collection_runs`, `run_subnets`,
  `run_sources`, every projection table, `user_device_attrs`, `user_identity_alias`,
  `user_links`, `user_acks`, `router_settings` are empty and `router_rejected_builtin` is unset;
  `user_subnet_attrs`, `collectors`, `sessions`, the owner password and other settings are
  unchanged; setting `data_reset_at` equals `FormatTime(at)`; `RebuildTx` leaves everything
  empty. A second call returns zero counts
- [X] T005 [P] [US1] Write `internal/ingest/ingest_test.go` `TestIngestDiscardsRunsBeforeReset`:
  with `data_reset_at` = T, a run with `started_at` T−1 min, `sent_at` = `received_at` = T+1 min
  → status `discarded`, no `collection_runs` row, the Applier not called, the collector's
  `last_report_at` updated; a run started at T+1 s → `stored`; skew correction: a collector
  whose run was spooled (`started_at` = `sent_at` = T−3 h, `received_at` = T+1 min) →
  `discarded` (no skew correction, research R2); with no `data_reset_at` every run is stored
- [X] T006 [P] [US1] Write `internal/inventory/apply_test.go` `TestSubnetFactsApplyOnRediscovery`:
  a `user_subnet_attrs` name `"lab"` and `ignored=true` written before any run; a run that first
  sees that subnet creates it with name `lab` and `ignored=1` and folds no observation of it; a
  fact written **after** the run (`at` > `received_at`) is not applied at creation; a rebuild
  gives the same rows. In `internal/store/devices_query_test.go`: `IgnoredSubnets` returns a CIDR
  whose latest `ignored` fact is `true` even with no `subnets` row, and not one whose latest fact
  is `false`
- [X] T007 [P] [US1] Write `tests/contract/schema_test.go` `TestUploadResultDiscarded`:
  `{"collection_id": …, "status": "discarded", "clock_skew_ms": 0}` validates against
  `UploadResult`; `"status": "ignored"` fails
- [X] T008 [P] [US1] Write `tests/integration/maint_devices_test.go`: upload two runs, name a
  device, set up a router (feature 003 helpers); `POST /maintenance/devices` without `confirm`,
  with `confirm=device`, without a session and with `Origin: http://evil.example` → nothing
  removed (400 / login redirect / 403); with `confirm= Devices ` → 303 to
  `/settings?done=devices&devices=N&runs=2`; Devices page empty; collectors still listed and their
  tokens still upload (`stored`); uploading a run whose `started_at` is before the removal →
  200 `discarded`; uploading a new run → the device is back with no name; `ping` returns no
  `routers` and still lists the ignored subnet; the server log has one `maintenance` line with
  `action=devices`

### Implementation for User Story 1

- [X] T009 [US1] Add `StatusDiscarded = "discarded"` to `internal/contract/v1.go`; add
  `discarded` to the `UploadResult.status` enum in
  `specs/001-lan-inventory-topology/contracts/collector-upload-api.yaml` with the description of
  `contracts/api-changes.md`. Make T007 pass (`contract-lint` stays green)
- [X] T010 [US1] Write `internal/store/maintenance.go`: `type Counts struct{ Devices, Runs,
  Collectors int }`; `const SettingDataResetAt = "data_reset_at"`; `RemoveAllDevices(ctx, tx, at)
  (Counts, error)` deleting, in this order, `run_sources`, `run_subnets`, `collection_runs`,
  `user_device_attrs`, `user_identity_alias`, `user_links`, `user_acks`, `router_settings`, the
  setting `router_rejected_builtin`, then `ResetProjections`, and setting `data_reset_at` (deleting removed collectors is
  added by T023, once the column exists). Count devices with
  `status != 'merged_away'` and runs before deleting. Make T004 pass
- [X] T011 [US1] In `internal/ingest/ingest.go`, before inserting: read `data_reset_at` in the
  transaction; if set and `run.StartedAt < reset` (no skew correction), update the
  collector's `last_report_at`, `last_clock_skew_ms`, `last_version` and return status
  `discarded` without storing or applying. In `internal/web/api.go` answer `discarded` with 200.
  Make T005 pass
- [X] T012 [US1] In `internal/inventory/apply.go`, after `INSERT INTO subnets`, apply the latest
  `user_subnet_attrs` value per field for that CIDR with `at < received_at` (same SQL as
  `applySubnetAttr`); thread `receivedAt` through. Change `Store.IgnoredSubnets` in
  `internal/store/devices_query.go` to the CIDRs whose latest `ignored` fact is `"true"`. Make T006
  pass
- [X] T013 [US1] Add `POST /maintenance/devices` (`internal/web/server.go` route,
  `internal/web/maintenance.go` handler): `confirmed(r, "devices")` else re-render Settings 400
  with `Type devices to confirm.`; run `store.RemoveAllDevices` in `s.opts.Ingester.Do` with
  `s.opts.Clock.Now()`; log; redirect 303 to the notice URL. Make T008 pass

**Checkpoint**: the inventory can be started over; MVP usable for testing.

---

## Phase 4: User Story 2 - Pause and resume the NAS scanner (Priority: P1)

**Goal**: a stored pause that stops scheduled and on-demand built-in scans until resumed, across
restarts.

**Independent Test**: pause; advance the fake clock past several intervals: no scan; "Scan now"
refused; new `app.New` on the same data dir: still paused, no start-up scan; resume: a scan
within one interval.

### Tests for User Story 2 ⚠️ write first, see them fail

- [X] T014 [P] [US2] Write `internal/app/scanner_pause_test.go` (fake clock, fake engine counting
  scans, like `scanner_router_test.go`): `SetPaused(ctx, true)` stores `builtin_paused` =
  `FormatTime(now)`; with the clock advanced 5 intervals no scan runs; `Trigger()` returns
  `TriggerPaused` and starts nothing; a scan in progress when pausing completes and is ingested;
  closing and re-opening the app on the same `DataDir` with `Start` → no start-up scan, `Paused`
  true; `SetPaused(ctx, false)` → a scan starts at `lastStart + interval`, or at once if that is
  past; `Paused` false and the setting deleted
- [X] T015 [P] [US2] Write `tests/integration/maint_pause_test.go` (`envOpts{builtinScan: true}`):
  `POST /maintenance/pause` → 303 `?done=paused`; home page shows `Paused` and a Resume form and no
  "Scan now"; `POST /scan` returns the fragment with `Paused` and starts nothing; Collectors page
  shows `nas` with status `paused`; a remote collector's upload is still `stored`;
  Settings shows `paused since` and a Resume button;
  `POST /maintenance/resume` → `?done=resumed` and "Scan now" back; pause/resume without a
  session or cross-site change nothing; with the built-in collector disabled both routes answer
  404 and the card has no scanner line

### Implementation for User Story 2

- [X] T016 [US2] In `internal/app/scanner.go`: `const SettingBuiltinPaused = "builtin_paused"`;
  `Paused(ctx) (bool, time.Time)`; `SetPaused(ctx, bool) error` writing/deleting the setting and
  sending on a new buffered `wake` channel; `Run` skips the start-up scan when paused and, while
  paused, waits only on `ctx`, `wake` and `trigger` (no deadline); after resume it scans at
  `lastStart + interval` or at once. `Trigger` returns a `web.TriggerResult`
  (`TriggerStarted`, `TriggerRunning`, `TriggerPaused`) instead of `bool`. Create `wake` in
  `internal/app/app.go`. Make T014 pass
- [X] T017 [US2] Extend `web.Scanner` in `internal/web/server.go` with `Paused(ctx)` and
  `SetPaused(ctx, bool) error`, `ScanStatus.Paused`/`PausedAt`; update `handleScan` for
  `TriggerPaused`; add `POST /maintenance/pause` and `/maintenance/resume` (404 when
  `s.opts.Scanner == nil`, logged); home `scan-status` template in
  `internal/web/templates/home.html` shows "Paused since …" and a Resume form; Settings card
  shows running/paused; `internal/web/templates/collectors.html` shows `paused` for the builtin
  collector while paused. Update every existing `web.Scanner` fake and caller of `Trigger`
  (web and integration tests) to the new interface. Make T015 pass

**Checkpoint**: the NAS scanner can be paused and resumed; US1 still passes.

---

## Phase 5: User Story 3 - Remove all remote collectors (Priority: P2)

**Goal**: remote collectors disappear and stop working; their history stays and names them as
removed; names are reusable.

**Independent Test**: two remote collectors with uploads; type `collectors`: only `nas` listed;
old tokens refused everywhere; inventory unchanged; a collector with an old name can be created.

### Tests for User Story 3 ⚠️ write first, see them fail

- [X] T018 [P] [US3] Write `internal/store/testdata/seed_4.sql` (schema v4: builtin `nas`, an
  active and a revoked remote collector, a run of each remote collector with `run_subnets`, a
  subnet discovered by the active one, a device with a sighting) and `TestMigration0005` in
  `internal/store/store_test.go`: after 4 → 5 every row and id survives, `deleted_at` is NULL,
  `PRAGMA foreign_key_check` returns nothing, `PRAGMA foreign_keys` is on again; inserting a
  second active collector with an existing name fails with a UNIQUE error; after setting
  `deleted_at` on one, a new row with its name succeeds. Add a test in the same file that a
  migration whose first line is `-- hne:foreign-keys-off` runs with foreign keys off
  (`MigrateTo` on a test migration list) and is rolled back when `foreign_key_check` finds a
  dangling reference
- [X] T019 [P] [US3] Write `TestRemoveAllCollectors` in `internal/store/maintenance_test.go`:
  `RemoveAllCollectors(ctx, tx, at)` marks every remote collector (active and revoked) removed
  (`deleted_at` = at, `token_hash` NULL, `revoked_at` kept or set), returns
  `Counts{Collectors: 2}`; `nas` untouched; runs and projections unchanged and the rebuild
  invariant holds; `ListCollectors`/`CollectorOverviews` omit removed collectors; a second call
  returns 0; `SubnetStatus` "discovered by" and the device sightings collector name read
  `<name> (removed)`. In `internal/auth/token_test.go`: a removed collector's token →
  `ErrInvalidToken`; `CreateCollector` with a removed collector's name succeeds and a different
  id is returned; with an active collector's name still `ErrCollectorExists`. Extend
  `TestRemoveAllDevices`: removed collectors are deleted once their runs are gone
- [X] T020 [P] [US3] Write `tests/integration/maint_collectors_test.go`: two collectors upload;
  `POST /maintenance/collectors` with a wrong word, no session, cross-site → nothing changes;
  `confirm=collectors` → 303 `?done=collectors&collectors=2`; Collectors page lists only `nas`;
  each old token gets 401 `invalid_token` on upload, ping and router login (feature 003 router
  configured); the Devices page is identical to before; creating a collector named like a
  removed one succeeds and its token uploads; the device page shows `<old name> (removed)` for
  old sightings

### Implementation for User Story 3

- [X] T021 [US3] In `internal/store/store.go` `MigrateTo`: a migration whose SQL starts with
  `-- hne:foreign-keys-off` runs on a dedicated `*sql.Conn`: `PRAGMA foreign_keys = OFF`, begin,
  exec, `PRAGMA foreign_key_check` (any row → rollback with an error), set `user_version`,
  commit, `PRAGMA foreign_keys = ON`, close the conn (also on error). Others run as today
- [X] T022 [US3] Write `internal/store/migrations/0005_collector_removal.sql` (first line
  `-- hne:foreign-keys-off`): create `collectors_v5` with every column of `collectors` (same
  types, CHECKs and defaults, including `last_version`), **without** `UNIQUE` on `name`, plus
  `deleted_at TEXT`; copy all rows with ids; drop `collectors`; rename; `CREATE UNIQUE INDEX
  collectors_name_active ON collectors (name) WHERE deleted_at IS NULL`. Make T018 pass
- [X] T023 [US3] Implement `RemoveAllCollectors` in `internal/store/maintenance.go`
  (`UPDATE collectors SET deleted_at = ?, token_hash = NULL, revoked_at = COALESCE(revoked_at, ?)
  WHERE kind = 'remote' AND deleted_at IS NULL`); `collectorCols`/`Collector.Removed`;
  `ListCollectors`, `CollectorOverviews` and `EnsureCollector`/`CollectorByName` consider only
  `deleted_at IS NULL`; `auth.Authenticate` adds `deleted_at IS NULL`; the name joins in
  `internal/store/devices_query.go` and `internal/store/routers.go` render
  `c.name || CASE WHEN c.deleted_at IS NOT NULL THEN ' (removed)' ELSE '' END`; add
  `DELETE FROM collectors WHERE deleted_at IS NOT NULL` at the end of `RemoveAllDevices`. Make
  T019 pass
- [X] T024 [US3] Add `POST /maintenance/collectors` (word `collectors`) in
  `internal/web/maintenance.go`/`server.go`, inside `Ingester.Do`, logged, 303 with the notice.
  Make T020 pass

**Checkpoint**: collectors can be cleared without losing what they found.

---

## Phase 6: User Story 4 - Drop all data (Priority: P3)

**Goal**: a fresh server with the same owner, without a restart.

**Independent Test**: with everything set up and the scanner paused, type `everything`: owner
still logged in; all pages empty; default settings; scanner running; old tokens refused.

### Tests for User Story 4 ⚠️ write first, see them fail

- [X] T025 [P] [US4] Write `TestDropAllData` in `internal/store/maintenance_test.go`:
  `DropAllData(ctx, tx, at, defaults map[string]string)` returns devices, runs and deleted remote
  collectors (removed or not); afterwards everything of `TestRemoveAllDevices` is empty plus
  `user_subnet_attrs` and every remote collector row; `nas` kept; `sessions` and the owner
  password kept; every other setting deleted except `data_reset_at` (= at) and the given
  defaults (`builtin_interval_seconds`, `offline_multiplier`); `builtin_paused` gone; rebuild
  invariant holds
- [X] T026 [P] [US4] Write `tests/integration/maint_everything_test.go` (`builtinScan: true`):
  set up uploads, edits, a router, a renamed and an ignored subnet, interval 30 min, offline
  multiplier 5, pause; wrong word / no session / cross-site → nothing changes; `confirm=everything`
  → 303 `?done=everything&…`; the same session still opens `/devices` (no login redirect); logging
  out and in with the same password works; Settings shows the start-up interval and multiplier 3
  and no subnets; the scanner is running (home shows "Scan now") and the next scan ingests;
  old tokens get `invalid_token`; a pre-drop run uploaded with a new collector's token is
  `discarded`

### Implementation for User Story 4

- [X] T027 [US4] Implement `DropAllData` in `internal/store/maintenance.go` reusing
  `RemoveAllDevices`, then `DELETE FROM user_subnet_attrs`, `DELETE FROM collectors WHERE kind =
  'remote'`, `DELETE FROM settings WHERE key NOT IN (<owner password key>, 'data_reset_at')` (export
  the owner password key from `internal/auth` or move it to `store`), and insert the defaults.
  Make T025 pass
- [X] T028 [US4] Add `web.Options.SettingDefaults map[string]string`, filled in
  `internal/app/app.go` with the start-up `builtin_interval_seconds` and `offline_multiplier`;
  add `POST /maintenance/everything` (word `everything`) running `DropAllData` in
  `Ingester.Do`, then `Scanner.SetPaused(ctx, false)` when the scanner exists (wakes the loop),
  logged, 303 with the notice. Make T026 pass

**Checkpoint**: all four actions work.

---

## Phase 7: Polish & Cross-Cutting Concerns

- [X] T029 [P] Docs: add the Maintenance card, routes and paused state to
  `specs/001-lan-inventory-topology/contracts/web-ui.md`; mention `result=discarded` in
  `specs/001-lan-inventory-topology/contracts/collector-cli.md`; note "back up the data volume
  before maintenance actions" in the deployment notes (README or `deploy/` docs)
- [X] T030 Run the full CI (PR to `main`): all jobs green, including the image smoke test with a
  fresh volume
- [ ] T031 Real-network validation per `quickstart.md` §2 (owner)

---

## Dependencies & Execution Order

- **Setup (T001)** → **Foundational (T002–T003)** → user stories.
- **US1 (T004–T013)** and **US2 (T014–T017)** are independent of each other.
- **US3 (T018–T024)** is independent of US2; T019/T023 extend US1's `RemoveAllDevices`, so do
  US1 first.
- **US4 (T025–T028)** depends on US1 (`RemoveAllDevices`, reset point) and US2 (`SetPaused`);
  it hard-deletes remote collectors, so it doesn't need US3's migration.
- **Polish** after all stories.

### Shared-file notes

- `internal/store/maintenance.go` / `maintenance_test.go`: T004/T010 → T019/T023 → T025/T027
  (sequential).
- `internal/web/maintenance.go`, `server.go`: T003 → T013 → T017 → T024 → T028.
- `internal/web/templates/settings.html`: T003 → T017.

## Parallel Examples

- Phase 2 + US1 tests: T002, T004, T005, T006, T007, T008 are different files: write together and
  push once to see them all fail.
- US2 tests T014, T015 and US3 tests T018, T019, T020 can be written together with US1's
  implementation.

## Implementation Strategy

1. MVP: Setup, Foundational, US1 (remove all devices with the reset point): already enough for
   repeatable tests on the real network.
2. US2 (pause): isolate the desktop collector.
3. US3 (remove collectors), then US4 (drop all data).
4. Each story: push its tests, see CI red, implement, see CI green.

## Notes

- No collector change: existing collectors treat `discarded` (HTTP 200) as accepted.
- No undo: the quickstart tells the owner to back up the data volume first.
