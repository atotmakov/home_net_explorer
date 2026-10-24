# Implementation Plan: Manual Devices

**Branch**: `011-manual-devices` | **Date**: 2026-10-24 | **Spec**: [spec.md](spec.md)

## Summary

Let the owner add equipment that scans cannot see (the unmanaged TL-SL1218P switch) as a device
by hand, wire it with the existing "Connected via" links, and see it on the Devices list and the
Map. A manual device is an append-only owner fact (`user_manual_devices`) replayed by a rebuild;
its projection is a `devices` row with `manual = 1` and the shown state `manual`. Name, type and
notes reuse `user_device_attrs`; links reuse `user_links`. A scan that later sees its MAC merges it
into the scanned device.

## Technical Context

**Language/Version**: Go ≥ 1.26 (server only; the collector is unchanged).
**Primary Dependencies**: none new.
**Storage**: SQLite; migration 0009 adds `user_manual_devices` (facts) and `devices.manual`.
**Testing**: CI only. Store, inventory (rebuild, merge by MAC), web handler and integration tests;
`map-model.js` tests in `internal/web/jstest`.
**Target Platform**: server container on the NAS.
**Project Type**: existing Go monorepo (server + collector).
**Performance Goals**: no measurable change; `TestMapPerformance` stays green.
**Constraints**: no contract change; no CIDR literals outside tests (lint); cross-site check on
every new post.
**Scale/Scope**: a handful of manual devices.
**Version**: `0.14`.

## Constitution Check

| Principle | Result |
|-----------|--------|
| I. Local-only | ✅ Nothing leaves the server; no new outbound traffic |
| II. Test-first | ✅ Each step starts with failing tests (store, inventory, web, JS model, integration) |
| III. Simplicity | ✅ No dependency; one fact table and one column; reuses attrs, links, `mergeInto` and the maintenance pattern |
| IV. Distributed collection | ✅ Collectors and the upload contract are unchanged; no technique to confirm |
| V. Faithful history | ✅ Manual devices are facts with timestamps; a rebuild reproduces them (SC-003); "Remove all devices" deletes facts and projections together |

**Result**: PASS (re-checked after design: unchanged).

## Project Structure

### Documentation (this feature)

```text
specs/011-manual-devices/
├── spec.md
├── plan.md                    # this file
├── research.md                # R1–R9
├── data-model.md              # migration 0009, rules
├── quickstart.md              # real-network validation
├── contracts/web-ui-changes.md
└── checklists/requirements.md
```

### Source Code (repository root)

```text
internal/store/migrations/0009_manual_devices.sql   user_manual_devices, devices.manual
internal/inventory/manual.go                        AddManualDevice, SetManualMAC, RemoveManualDevice,
                                                    applyManualFact; validation (name, MAC, duplicate)
internal/inventory/rebuild.go                       manual facts in loadUserFacts / applyFact
internal/inventory/apply.go                         merge a manual device seen by MAC (R3)
internal/store/devices_query.go, freshness.go       manual column; StateManual; filters; Counts.Manual
internal/store/userlinks.go                         loop check; option ranking (switches)
internal/store/topology.go                          manual node state and group by link chain
internal/store/maintenance.go                       delete manual facts; count manual devices
internal/web/pages.go (+ manual.go)                 /devices/new, POST /devices, /mac, /remove; loop error
internal/web/templates/                             devices.html, device.html, device_new.html, home.html,
                                                    settings.html (counts)
internal/web/static/map-model.js, map.js            manual class and text; "Online only" rule
internal/web/jstest/                                map-model tests
tests/integration/us_manual_test.go                 add → wire → map → rebuild → remove
VERSION                                             0.14
```

**Structure Decision**: server-only change; one new inventory file beside `facts.go`, everything
else extends existing files.

## Complexity Tracking

| Addition | Why needed | Simpler alternative rejected because |
|----------|------------|--------------------------------------|
| Fact table `user_manual_devices` | A manual device has no run; without a fact a rebuild loses it (Principle V) | Storing it only in `devices` (a projection) is wiped by every rebuild |
| `devices.manual` column | Lists, counts and the Map must tell manual devices apart | A new `status` value needs a `devices` table rebuild to change its CHECK |
