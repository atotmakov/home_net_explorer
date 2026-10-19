---

description: "Task list for Device Status Freshness"
---

# Tasks: Device Status Freshness

**Input**: `specs/006-device-status-freshness/` (spec.md, plan.md, research.md, data-model.md,
contracts/web-ui-changes.md, quickstart.md)

**Tests**: REQUIRED (Principle II): tests first, red in CI, then implementation. CI only (no local
Go toolchain).

## Phase 1: Setup

- [ ] T001 Set `VERSION` to `0.7`

## Phase 2: Foundational (shown state in the store)

- [ ] T002 Tests in `internal/store/devices_query_test.go`: `TestDeviceState` — with an injected
  `now`: a stored-online device seen within 2 × I is `online`, older than 2 × I is `stale`, a
  stored-offline device is `offline`; I is the `interval_seconds` of the most recent complete scan
  (by `finished_at`, any collector) of a subnet holding one of the device's current addresses
  (two collectors with 900 s and 3600 s intervals: the later scan's interval is used); a device
  with no complete scan of its subnets uses the `builtin_interval_seconds` setting (default 900 s)
- [ ] T003 Implement in `internal/store/devices_query.go`: `DeviceRow.State` (`online` | `stale` |
  `offline`) and `DeviceRow.StaleFor` (age of `last_seen`), computed in `ListDevices`/`GetDevice`
  from a `now` argument (callers pass the request time); stored `online`/`new` → online or stale,
  `offline`/`new_offline` → offline. Make T002 pass

## Phase 3: User Story 1 - Status shows whether "online" is current (P1) 🎯 MVP

**Goal**: green "online" vs gray "online (stale)" on the Devices list and device page.
**Independent test**: upload a scan, view at once (green), view later with no scan (gray).

- [ ] T004 [P] [US1] Integration test `tests/integration/status_freshness_test.go`
  `TestStaleOnlineShown`: upload a scan seeing a device; `/devices` and `/devices/{id}` show the
  `s-online` badge "online"; with the server clock advanced past 2 × interval (test clock), the
  badge is `s-stale` "online (stale)" with `title="last seen … ago"`; a new upload makes it green
- [ ] T005 [US1] Templates `internal/web/templates/devices.html`, `device.html`: badge by
  `.State` (`s-online` "online", `s-stale` "online (stale)" + tooltip, `s-offline` "offline");
  `internal/web/pages.go` passes `now` from the server clock; CSS in `internal/web/static/` adds
  `.s-stale` (gray) and gives `.s-offline` a distinct, non-gray style. Make T004 pass

## Phase 4: User Story 2 - Offline means the latest scan did not see it (P1)

**Goal**: one missed complete scan makes a device offline; late uploads never do.
**Independent test**: scan sees device, next complete scan misses it → offline + event.

- [ ] T006 [P] [US2] Rewrite `internal/inventory/offline_test.go` for research R1:
  `TestOfflineAfterOneMissedScan` (`went_offline` at the missing scan's `finished_at`);
  `TestLateUploadDoesNotMarkOffline` (a scan that started before the device's `last_seen`, uploaded
  after a newer sighting, changes nothing); `TestIncompleteScansDontCount` and ignored/skipped
  subnets unchanged; `TestComesBackOnline`; `TestTwoCollectorsLatestScanDecides` (collector A
  misses the device after collector B saw it earlier → offline); `TestRebuildSameStatuses`
  (rebuild gives identical statuses and events); remove `TestOfflineNeedsEveryActiveCollector`,
  `TestMultiplierFromSettings`, `TestStaleSubnetKeepsStatus` (time-based rule gone)
- [ ] T007 [US2] Implement the rule in `internal/inventory/offline.go`: candidates = devices with
  a current address in the scanned subnet, status `online`/`new`, not seen by R, and
  `last_seen < R.started_at`; drop the time window, active-collector and multiplier logic and
  `OfflineMultiplier`/`DefaultOfflineMultiplier`. Make T006 pass
- [ ] T008 [P] [US2] Test `internal/app` `TestOneTimeRebuild`: a store with `projection_rules`
  unset (or < 6) and a device that the new rule makes offline is re-derived at `app.New` start-up
  and `projection_rules` = `6`; a second start does not rebuild again
- [ ] T009 [US2] Implement in `internal/app/app.go`: after migrations, if `projection_rules` < 6,
  run `Rebuild` under the ingester lock, log it, set `projection_rules` = `6`; add
  `store.SettingProjectionRules`; keep it out of maintenance "drop all" deletion (like the
  owner password and `data_reset_at`). Make T008 pass

## Phase 5: User Story 3 - Filters and counts follow the shown status (P2)

**Goal**: filter and home counts use online / stale / offline; the multiplier setting is gone.

- [ ] T010 [P] [US3] Tests: `internal/store/devices_query_test.go` `TestHomeCountsByState`
  (Counts.Online/Stale/Offline equal `ListDevices` with `Status` = each state) and
  `TestListDevicesFilterByState` (`stale` accepted; `new` returns everything as unknown);
  `TestSubnetStatusStale` (stale = no complete scan within 2 × interval); integration
  `TestStatusFilterAndCounts` in `tests/integration/status_freshness_test.go` (filter options are
  exactly online, stale, offline; home shows the three counts); update
  `tests/integration/us1_inventory_test.go`, `maint_everything_test.go` and
  `internal/store/maintenance_test.go` so they no longer post or expect `offline_multiplier`,
  and assert the Settings page has no `name="offline_multiplier"`
- [ ] T011 [US3] Implement: `store.HomeCounts(ctx, now)` with `Stale`; filter by `State` in
  `ListDevices`; `SubnetStatus` stale rule (2 × interval, no multiplier); `internal/web/server.go`
  `statuses` = online, stale, offline; `home.html` three counts; remove the multiplier field from
  `settings.html`, its validation and value in `internal/web/pages.go`, and its default in
  `internal/app/app.go` (`SettingDefaults`, `SetDefaultSetting`). Make T010 pass

## Phase 6: Polish

- [ ] T012 Docs: `specs/001-lan-inventory-topology/contracts/web-ui.md` (states, filter, counts,
  settings) and `data-model.md` (offline rule R7 replaced by 006 R1, stale state); note in 001
  spec/research where the multiplier is mentioned that feature 006 replaced it
- [ ] T013 CI green, merge to `main`, image built, deployed to ocean (standing instructions)
- [ ] T014 Real network (quickstart.md): green after a scan, gray after 30 min without scans,
  offline after one scan without the device, counts match the filter

## Dependencies

T001 → T002/T003 (state in the store) → US1 (T004–T005) and US2 (T006–T009) in parallel →
US3 (T010–T011, uses `State` and the new rule) → Polish.

## Parallel examples

- After T003: T004 (integration, US1) ∥ T006 (inventory, US2) ∥ T008 (app, US2).
- Within US3: the store tests and the integration test of T010 are independent files.

## Implementation strategy

MVP = Phase 2 + US1 (green/gray), deployable alone (offline still on the old rule). Then US2
(rule + one-time rebuild), then US3 (filter, counts, setting removal). Tests for each phase are
pushed first and must fail in CI before the implementation.
