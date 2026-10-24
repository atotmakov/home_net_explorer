---

description: "Task list for Manual Devices"
---

# Tasks: Manual Devices

**Input**: `specs/011-manual-devices/` (spec.md, plan.md, research.md, data-model.md,
contracts/web-ui-changes.md, quickstart.md)

**Tests**: REQUIRED (constitution Principle II): tests first, pushed and red in CI, then the
implementation. CI only (no local Go). Time comes from `clock.Fake` / `it.T0`. Fixtures use
documentation MACs (`00:00:5e:00:53:xx`) and the 192.168.0.x example addresses already used by the
topology tests. No IPv4 CIDR literals in non-test `.go` files (lint). The collector and the upload
contract are unchanged.

## Format: `[ID] [P?] [Story] Description`

## Phase 1: Setup

- [X] T001 Set `VERSION` to `0.14`

## Phase 2: Foundational (storage and facts)

**Purpose**: the fact table, the projection flag and the replay, needed by every story.

- [X] T002 [P] Tests in `internal/inventory/manual_test.go`:
  - `AddManualDevice(ctx, tx, ManualDevice{Name, Type, MAC, Notes}, at)` returns an identity key
    `manual:<uuid>` and creates a `devices` row with `manual = 1`, `identity_strength = 'strong'`,
    `status = 'online'`, `first_seen = last_seen = at`, no addresses, the name/type/notes as
    `user_name`/`type`/`notes`, and the OUI vendor of the MAC
  - Validation: "name required, 1–100 characters after trimming"; "type one of
    `inventory.DeviceTypes` or ''"; "notes at most 2000 characters"; a MAC "must parse as a 6-byte
    unicast MAC" (dashes, upper case and no separators are accepted and stored normalized, e.g.
    `00:00:5e:00:53:01`); a MAC already held by a device that is not merged away is refused with an
    error naming that device (`ErrMACInUse{Name, IP}`)
  - `SetManualMAC` sets, changes and clears ('') the MAC, allowing the device's own MAC, and
    refuses it on a scanned device
  - `RemoveManualDevice` deletes the row, refuses a scanned device, and writes `ClearUserLink` for
    every device whose current link points to it
  - Rebuild (`inventory.Rebuild`) after add, rename, MAC change and a link reproduces the same
    rows; after remove the device stays gone
- [X] T003 [P] Test in `internal/store/migrate_internal_test.go`:
  schema version 9 has `user_manual_devices` (`field IN ('add', 'mac', 'remove')`) and
  `devices.manual` defaulting to 0
- [X] T004 Implement `internal/store/migrations/0009_manual_devices.sql` exactly as in
  data-model.md (table `user_manual_devices` with index, `ALTER TABLE devices ADD COLUMN manual
  INTEGER NOT NULL DEFAULT 0`). Make T003 pass
- [X] T005 Implement `internal/inventory/manual.go` (`ManualDevice`, `AddManualDevice`,
  `SetManualMAC`, `RemoveManualDevice`, `applyManualFact`, `ErrMACInUse`) and add the manual facts
  to `loadUserFacts`/`applyFact` in `internal/inventory/rebuild.go` with order 0 (a device is added
  before its attributes on equal times). Make T002 pass

**Checkpoint**: manual devices exist in storage and survive a rebuild.

## Phase 3: User Story 1 - Add a device that scans cannot see (P1) 🎯 MVP

**Goal**: the owner adds the switch; it is in the Devices list as "manual".
**Independent test**: add "TL-SL1218P" (switch); the Devices list shows it with "manual" and no
address; its device page opens; status filters do not list it.

- [X] T006 [P] [US1] Tests in `internal/store/devices_query_test.go`:
  - `ListDevices` returns a manual device with `State == store.StateManual` and no addresses
  - `DeviceFilter{Status: online|stale|offline}` and `DeviceFilter{Subnet: …}` never return it;
    no filter does
  - `HomeCounts` has `Manual = 1`; Online + Stale + Offline + Manual == Total (SC-004)
  - `DeviceRow.Manual` is true for it
- [X] T007 [P] [US1] Tests in `tests/integration/us_manual_test.go` `TestAddManualDevice`:
  - `GET /devices/new` has the form (name, type select, MAC, notes)
  - `POST /devices` with name "TL-SL1218P", type "switch" → 303 to `/devices/{id}`; the page says
    "Added by hand"
  - `/devices` lists it with the "manual" badge; `/devices?status=online` does not
  - Empty name → 400 "a name is required"; MAC "zz" → 400 "is not a MAC address"; the MAC of a
    scanned device → 400 naming that device; nothing saved in each case
  - The home page says "(1 added by hand)"
  - A post without the same-origin header is refused (feature 007)
- [X] T008 [US1] Implement in `internal/store/devices_query.go` and `internal/store/freshness.go`:
  `manual` in `deviceCols`/`scanDevice`, `DeviceRow.Manual`, `StateManual = "manual"` set first in
  `stateEval.apply`, filters and `Counts.Manual`. Make T006 pass
- [X] T009 [US1] Implement `internal/web/manual.go` (`GET /devices/new`, `POST /devices` through
  the ingester lock), routes in `internal/web/server.go`, template
  `internal/web/templates/device_new.html`, the "Add device" link and the "manual" badge in
  `devices.html`, "Added by hand on <date>" in `device.html`, "(M added by hand)" in `home.html`,
  `stateLabel("manual")`. Make T007 pass

**Checkpoint**: US1 complete and deployable on its own.

## Phase 4: User Story 2 - Wire the manual device into the network (P1)

**Goal**: router → switch → wired devices on the Map.
**Independent test**: switch via the router, two devices via the switch → the Map shows the chain
in 192.168.0.0/24, no LAN1 edge for the two devices.

- [X] T010 [P] [US2] Tests in `internal/store/topology_manual_test.go` `TestTopologyManualDevices`:
  - The manual node has `state: "manual"`, `last_seen_ago: ""`, `ips: []`
  - Its parent is the group of the device it connects via; via another manual device that connects
    via the router, both are in the router's group; connected to nothing → `other`
  - Edges `mu-<switch>` (switch → router) and `mu-<dev>` (device → switch); the devices' router
    port edges (`pd-<dev>`) are not drawn
  - `TestMapJSONShape` still passes
- [X] T011 [P] [US2] Tests in `internal/store/userlinks_test.go` (new):
  - `DeviceLinkOptions` lists the manual device; the order is routers, switches, access points,
    then by name (FR-004)
  - `LinkLoop(ctx, from, to)` reports a loop when to's chain reaches from (direct and two-step),
    and none otherwise
- [X] T012 [P] [US2] Tests in `internal/web/jstest/map-model.test.mjs`:
  - A `state: "manual"` node gets the class `manual`; details say "manual (added by hand)" and no
    "last seen"
  - `visibleIds(topo, true)` keeps a manual node when an online or stale device has a manual edge to
    it, directly or through another manual node, and hides it when only offline devices do or none
- [X] T013 [US2] Implement the topology in `internal/store/topology.go` (manual state, group by link
  chain with a visited set) and `LinkLoop` plus the ranking in `internal/store/userlinks.go`; the
  400 loop message in `internal/web/map.go` `handleDeviceLink` ("this would make a loop: <target>
  connects via <this device>"); the loop check and the link write run in one `Ingester.Do` call,
  so a concurrent edit cannot create a loop between check and write. Make T010 and T011 pass
- [X] T014 [US2] Implement `internal/web/static/map-model.js` (class, details text, "Online only"
  rule) and the dashed outline in `internal/web/static/map.js`. Make T012 pass

**Checkpoint**: SC-002 can be checked on the real network.

## Phase 5: User Story 3 - Edit and remove a manual device (P2)

**Goal**: rename, retype, change the MAC, remove.
**Independent test**: rename → the new name in the list and on the Map; remove → gone, linked devices
lose the link and are drawn from scan data again.

- [X] T015 [P] [US3] Tests in `tests/integration/us_manual_test.go` `TestEditRemoveManualDevice`:
  - Rename and retype through `POST /devices/{id}/attrs` show in `/devices` and `/ui/map.json`
  - `POST /devices/{id}/mac` sets and clears the MAC; a MAC in use → 400; on a scanned device → 404
  - `POST /devices/{id}/remove` without `confirm=remove` → 400 and the device stays; with it → 303
    to `/devices`, the device is gone from the list, the Map and the "Connected via" options; a
    device that was linked to it shows "not set" and is back on its router port on the Map
  - A scanned device's page has no Remove section and `POST …/remove` → 404 (FR-012)
- [X] T016 [P] [US3] Test in `internal/inventory/manual_test.go` `TestScanSeesManualMAC` (FR-010):
  a manual device with MAC m, then a scan sees m at 192.168.0.30 → one device with the owner's name,
  type, notes and link, a normal state, and the manual row merged away; a device whose "Connected
  via" pointed at the manual device now points at the merged device (its `mu-` edge on the Map
  targets it), and the merged device keeps the manual device's own link
- [X] T017 [US3] Implement `POST /devices/{id}/mac` and `POST /devices/{id}/remove` in
  `internal/web/manual.go`, each inside `Ingester.Do` (owner facts and projections change
  together, serialized with ingest and rebuild), the MAC field and Remove section in `device.html` (manual devices only),
  and the merge by MAC in `internal/inventory/apply.go` (after the scanned device is resolved: a row
  with `manual = 1 AND mac = ?` → `mergeInto`). Make T015 and T016 pass

## Phase 6: Polish & Cross-Cutting

- [X] T018 [P] Tests in `internal/store/maintenance_test.go` and the existing maintenance
  integration tests: "Remove all devices" and "Drop all data" delete `user_manual_devices`; a
  rebuild afterwards is empty; the result message counts the manual devices ("…, 1 added by hand")
- [X] T019 Implement in `internal/store/maintenance.go` (`DELETE FROM user_manual_devices`,
  `RemovalCounts.Manual`) and the message in `internal/web/maintenance.go`; update the "Remove all
  devices" text in `settings.html` to name "devices added by hand". Make T018 pass
- [X] T020 Integration test `TestManualDeviceSurvives` in `tests/integration/us_manual_test.go`:
  after three scans and `app.Rebuild` the manual device, its links and the Map are unchanged
  (SC-003); `TestMapPerformance` stays green
- [ ] T021 Real-network validation, quickstart §1–§7 after deploying 0.14: record the results here
  (counts only, no MACs); time the steps of §1 and §3 (SC-001: under 2 minutes)
- [X] T022 [P] Docs: in the feature 001 identity rules (`specs/001-lan-inventory-topology/
  data-model.md`, the `identity_key` row) add the `manual:<uuid>` key: set once when the owner adds
  a device, never derived from observations; a scan that sees the manual device's MAC merges it into
  the `mac:` device (011 research R3)

## Dependencies & Execution Order

- Phase 1 → Phase 2 (T004 before T005; T002/T003 together).
- US1 (T006–T009) needs Phase 2.
- US2 (T010–T014) needs Phase 2 and T008 (`StateManual`); independent of the US1 web pages.
- US3 (T015–T017) needs US1 (the pages) and T005.
- Polish after US3; T021 after deployment; T022 any time.

## Parallel Opportunities

- T002 and T003; T006 and T007; T010, T011 and T012; T015 and T016.
- US2's store/JS work can run beside US1's web pages once T008 is in.

## Implementation Strategy

1. **PR 1** (Phases 1–3): storage, facts, rebuild, add form, list, counts. Merge and deploy: the
   owner can add the switch.
2. **PR 2** (Phases 4–5): Map, links, loops, edit and remove.
3. **PR 3** (Phase 6): maintenance, survival test, real-network results.

Each PR: red tests pushed first, then the implementation, CI green, squash merge, deploy.
