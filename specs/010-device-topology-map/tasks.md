---

description: "Task list for Device Topology Map"
---

# Tasks: Device Topology Map

**Input**: `specs/010-device-topology-map/` (spec.md, plan.md, research.md, data-model.md,
contracts/web-ui-changes.md, quickstart.md)

**Tests**: REQUIRED (constitution Principle II): tests first, pushed and red in CI, then the
implementation. CI only (no local Go or Node toolchain). Time comes from an injected `now`
(`clock.Fake`); no real network in tests. Store tests seed data with
`internal/inventory/inventorytest` (`Harness.Ingest(Run{..., Vantage: ...})`, `ARP`, `Scan`, and
router-table observations as in `internal/inventory/apply_router_test.go`). Page logic is tested
with `node --test` on the pure module `internal/web/static/map-model.js` (research R8, R9); only
the Cytoscape/DOM glue in `map.js` is checked by hand (quickstart.md).

## Phase 1: Setup

- [X] T001 Set `VERSION` to `0.11`
- [X] T002 Add CI job `test-js` to `.github/workflows/ci.yml`: `runs-on: ubuntu-latest`, checkout
  at `needs.autofix.outputs.sha || github.sha` like the other test jobs, `actions/setup-node`
  with `node-version: 22` (CI only; no npm install, no `package.json`), then
  `node --test "internal/web/jstest/*.test.mjs"`; add `test-js` to `build`'s `needs`. Push it together with the first JS test (T007) so the job has a test to run

## Phase 2: Foundational (topology view and JSON endpoint)

**Purpose**: the `Topology` type, `store.Topology(ctx, now)` returning device nodes, and
`GET /ui/map.json` serving it in the contract shape. Every story builds on these.

- [X] T003 [P] Test in `internal/web/map_test.go` `TestMapJSONShape`: on an empty store
  `GET /ui/map.json` is 200, `Content-Type: application/json`, `Cache-Control: no-store`, and
  decodes to `{"groups":[],"nodes":[],"edges":[]}` (arrays, never `null`); with one ingested device
  the node has exactly the keys of contracts/web-ui-changes.md (`id`, `kind`="device", `parent`,
  `device_id`, `label`, `ip`, `ips`, `mac`, `vendor`, `type`, `state`, `last_seen_ago`,
  `gateway_of`); unknown query parameters (`scope=online`) are ignored
- [X] T004 Implement `internal/store/topology.go`: types `Topology{Groups []TopoGroup; Nodes
  []TopoNode; Edges []TopoEdge}` with JSON tags per the contract (node `kind` ∈ `device`|`port`;
  edge `kind` ∈ `gateway`|`bridge`|`port`|`manual`; `current` bool), and `Topology(ctx, now)`
  returning device nodes from `ListDevices(ctx, DeviceFilter{Now: now})` with empty slices for
  groups and edges; `internal/web/map.go` `handleMapJSON` (`writeJSON`, `Cache-Control:
  no-store`, `now` from `s.opts.Clock`); route `GET /ui/map.json` in `internal/web/server.go`.
  Make T003 pass

## Phase 3: User Story 1 - See every device in its place (P1) 🎯 MVP

**Goal**: all devices (online, stale, offline) grouped by subnet with Devices-list states.
**Independent test**: inventory with devices in three subnets in all three states → every device
once, right group, right state; `/map` page loads and draws them.

- [X] T005 [P] [US1] Tests in `internal/store/topology_test.go`:
  `TestTopologyAllDevices` — ingest devices in 192.168.0.0/24, 192.168.1.0/24 and 192.168.8.0/24;
  make one offline (a later complete scan misses it) and one stale (`now` > 2 × interval after its
  last scan); `Topology(now)` has exactly the device ids of `ListDevices(DeviceFilter{})`, each
  once, with `state` equal to its `DeviceRow.State` and `last_seen_ago` equal to the feature-006
  `ago` text; `label` equals `DeviceRow.DisplayName`; `TestTopologyGroups` — group id
  `subnet-<id>`, `label` = subnet user name + " (" + CIDR + ")" when named, else the CIDR; `stale`
  from `SubnetStatus(now)`; a device's `parent` is the group of its first current address; a
  device with no current address on a visible subnet (simulated by clearing `current`; devices seen
  only on ignored subnets are not on the Devices list, so not on the map) has parent `other`
  and the `other` group (label "Other") exists only then; a visible subnet with no devices has no
  group unless it is in a collector's latest run; merged-away devices never appear (FR-012)
- [X] T006 [P] [US1] Tests in `internal/web/map_test.go` `TestMapPage`: `GET /map` is 200, marks
  the Map nav item current, contains `<div id="map"`, loads `/static/vendor/cytoscape.min.js` and
  `<script type="module" src="/static/map.js">` (no `http://` or `https://` script sources),
  contains the switch with "All devices" checked by default and "Online only", the legend texts
  ("online", "offline", "gateway", "two subnets", "port / Wi-Fi", "last known", "manual"), and
  `<aside id="map-details"`; with an empty store the page shows "No devices yet";
  `GET /static/map.js` and `GET /static/map-model.js` are 200 with a JavaScript content type
- [X] T007 [P] [US1] Tests in `internal/web/jstest/map-model.test.mjs` (`node:test`,
  `node:assert/strict`, importing `../static/map-model.js`) `toElements groups and devices`: for
  a topology fixture with two groups and online, stale and offline devices, `toElements` returns
  one compound element per group (`data.id`, `data.label`) and one element per device with
  `data.parent` = its group, `data.label`, and classes `state-online` / `state-stale` /
  `state-offline` (offline also `dim`); the tooltip text of a device is
  "`label` · `ip` · `mac` · last seen `last_seen_ago` ago"; an empty topology gives `[]`
- [X] T008 [US1] Implement groups and device fields in `internal/store/topology.go` (R2, R3):
  `label` = `DisplayName`, `ip` = `PrimaryIP`, `ips` = all current addresses, `mac`, `vendor`
  (manufacturer), `type`, `state`, `last_seen_ago` (move the `ago` formatter from
  `internal/web/server.go` into `internal/store` as an exported function used by both), groups
  from `SubnetStatus(now)` plus subnets of each collector's latest run, `other` group. Make T005
  pass
- [X] T009 [US1] Implement the page: `internal/web/map.go` `handleMap` renders
  `internal/web/templates/map.html` (layout, `Active: "map"`, empty-state text, switch, legend,
  `<div id="map">`, `<aside id="map-details">`); register the template and `GET /map` in
  `server.go`; `internal/web/static/map-model.js` (ES module, no DOM access) exporting
  `toElements` and `tooltip`; `internal/web/static/map.js` (ES module) fetches `/ui/map.json`,
  creates Cytoscape with `toElements`, styles classes `state-online` (`--ok`), `state-stale`
  (`--fg`), `state-offline` (`--muted`) read with `getComputedStyle`, `dim` at lower opacity,
  `cose` layout, zoom/pan, hover tooltip; map area, legend and panel CSS in
  `internal/web/static/app.css` (map height `calc(100vh - 12rem)`, min 480px). Make T006 and
  T007 pass

**Checkpoint**: the Map tab shows every device in its subnet with the right status (US1).

## Phase 4: User Story 2 - See how devices are connected (P1)

**Goal**: gateway, two-subnet, port / Wi-Fi edges; last known for offline devices.
**Independent test**: router-table run with ports + desktop run with two interfaces → edges.

- [X] T010 [P] [US2] Tests in `internal/store/topology_test.go`:
  `TestTopologyGateways` — (a) a router read for 192.168.0.0/24 with source address 192.168.0.1
  (outcome `ok`) makes the device holding 192.168.0.1 `gateway_of: ["subnet-<id>"]` and adds a
  `gateway` edge device → group; (b) a run whose vantage has route `{destination: "0.0.0.0/0",
  next_hop: "192.168.1.1"}` makes the device holding 192.168.1.1 gateway of 192.168.1.0/24; a
  next hop no device holds adds nothing; `TestTopologyBridges` — a device with current addresses
  in two subnets is one node (parent = first address's group) with a `bridge` edge to the other
  group; a collector whose latest vantage has interfaces in 192.168.1.0/24 and 192.168.8.0/24
  with MAC M gets bridge edges for the device with MAC M; `TestTopologyPorts` — devices reported
  with `via` "LAN1", "LAN1" and "SSID1" by the router at 192.168.0.1 give port nodes
  `port-<routerDev>-LAN1` and `port-<routerDev>-SSID1` (kind `port`, parent = router's group,
  label = via), one edge router → port per port and one edge port → device per device, all
  `current: true`; after a later router read without one of them (device offline) its port edge
  has `current: false`; the most recent non-empty `via` wins after a move LAN1 → LAN2; no port
  nodes when the router address is not a device in the inventory
- [X] T011 [P] [US2] Tests in `internal/web/jstest/map-model.test.mjs` `toElements edges`: edges
  get classes `kind-gateway`, `kind-bridge`, `kind-port`, `kind-manual`; a port edge with
  `current: false` also gets `last-known`; port nodes get class `port`; a device with non-empty
  `gateway_of` gets class `gateway`; edge `data.source`/`data.target` equal the JSON ids
- [X] T012 [US2] Implement in `internal/store/topology.go` (R4–R6): gateway sources (latest
  `run_sources` row per subnet with outcome `ok`, `ListRouters`, route next hops from the latest
  `collection_runs.payload_gz` of each collector — gzip + `contract.Decode`), bridge edges
  (current addresses in other visible subnets; collector interfaces matched by MAC), port
  nodes/edges from each device's latest sighting with `via <> ''` (`current` = device state ≠
  offline). Stable edge ids: `gw-<dev>-<group>`, `br-<dev>-<group>`, `rp-<router>-<via>`,
  `pd-<dev>`. Make T010 pass
- [X] T013 [US2] Edges in `internal/web/static/map-model.js` (classes per T011) and their styles
  in `internal/web/static/map.js`: gateway thick, bridge dashed, port thin, `last-known` dotted
  and muted, port nodes small and round, gateway devices with a distinct border; legend matches.
  Make T011 pass

**Checkpoint**: US1 + US2 = the requested topology of all devices.

## Phase 5: User Story 3 - Focus on what is online (P2)

**Goal**: switch between All devices (default) and Online only, client-side.
**Independent test**: Online only hides offline devices and their edges; reload → All devices.

- [X] T014 [P] [US3] Tests in `internal/web/jstest/map-model.test.mjs` `visibleIds`: with
  `onlineOnly = false` every element id is visible; with `true`, offline devices, edges touching
  them, port nodes left with no visible device (and their router → port edge) and groups left with
  no visible node are hidden, while stale devices stay; the function does not mutate its input
- [X] T015 [US3] Implement `visibleIds(topology, onlineOnly)` in
  `internal/web/static/map-model.js`; in `map.js` the switch shows/hides elements by these ids
  without refetching; the page always opens on All devices (no stored preference, FR-004). Make
  T014 pass

## Phase 6: User Story 4 - Open a device from the map (P2)

**Goal**: click → details panel with a link to the device page.

- [X] T016 [P] [US4] Tests in `internal/web/jstest/map-model.test.mjs` `details`: for a device
  node, `details(topology, id)` returns the title (label), the status (`state`, text "online" for
  online and stale, "offline" for offline — as the Devices list), rows for type, IPs, MAC, vendor,
  "last seen … ago", "Gateway of" (group labels), "Connected through" (`<port> on <router label>`,
  with " (last known)" when not current) and "Connected via" (manual target label), and `href`
  `/devices/<device_id>`; rows with empty values are omitted; a hostname containing `<b>` is
  returned as plain text; a port node or unknown id returns `null`
- [X] T017 [US4] Implement `details` in `internal/web/static/map-model.js`; in `map.js` a device
  click renders it into `#map-details` with `textContent` only (status as `badge s-<state>`), a
  background click closes it, another device replaces it. Make T016 pass

## Phase 7: User Story 5 - Record a connection the scans cannot see (P3)

**Goal**: owner-set "connected via" links that survive scans and override ports.
**Independent test**: set link → manual edge; 3 scans → still there; clear → gone.

- [X] T018 [P] [US5] Tests in `internal/store/topology_test.go` `TestTopologyManualLinks`: after
  `SetUserLink(ctx, from, to, at)` the map has a `manual` edge `from → to` (id `mu-<from>`) and no
  port edge for `from`; a later `ClearUserLink` removes it (rows are appended, never updated: the
  table has both rows); after `from` is merged into another device the edge starts at the
  surviving device; a link whose target is not on the map is dropped; links survive 3 further
  ingests and `store.Rebuild`; `DeviceLinkOptions(ctx, id)` lists every other device on the map,
  type `router` and `access_point` first, then by label, and marks the current target
- [X] T019 [P] [US5] Tests in `internal/web/map_test.go` `TestDeviceLinkForm`: the device page has a
  `select name="via"` from `DeviceLinkOptions` (the device itself absent) with the current link
  selected; `POST /devices/{id}/link via=<other id>` → 303 to the same-origin `Referer`
  (`http://<host>/map`), or to `/devices/{id}` when the Referer is missing or cross-origin; the map
  JSON then has the manual edge; `via=` clears it; `via=<id itself>` and an unknown id → 400;
  unknown device → 404; a cross-site POST (`Origin: http://evil.example`) → 403
- [X] T020 [P] [US5] Integration test `tests/integration/us_map_test.go` `TestMapEndToEnd`: ingest
  `valid_full.json` (desktop, two interfaces) and `valid_router.json` (ports) through the upload
  API; `/ui/map.json` has every device of `/devices`, the router's port nodes and the desktop's
  bridge edges; set a manual link through the form, upload 3 more runs, the manual edge is still
  there (SC-005); the number of `run_sources` rows is the same before and after `GET /map` and
  `GET /ui/map.json` (the map reads no router, FR-011); `/map` is 200
- [X] T021 [P] [US5] Tests in `internal/web/jstest/map-model.test.mjs` `manual links`: a manual
  edge gets class `kind-manual` and `data.arrow = true`; `details` of its source shows "Connected
  via <target label>" and no "Connected through"
- [X] T022 [US5] Implement `internal/store/userlinks.go` (`SetUserLink`, `ClearUserLink` append to
  `user_links` with identity keys and `at`; `latestUserLinks` = latest row per
  `from_identity_key`, resolved through `resolveDevice`; `DeviceLinkOptions`), manual edges in
  `internal/store/topology.go` (port edge of `from` suppressed), `POST /devices/{id}/link` in
  `internal/web/map.go` + route, the "Connected via" select with Save in
  `internal/web/templates/device.html`. Make T018–T020 pass
- [X] T023 [US5] Manual edges in `internal/web/static/map-model.js` and their style in `map.js`
  (own color, target arrow); legend entry. Make T021 pass

## Phase 8: Polish & Cross-Cutting

- [X] T024 [P] Test `tests/integration/us_map_test.go` `TestMapPerformance`: ingest one generated
  run (collector `perf`) with 250 devices; in every mode the map has 250 device nodes; the
  timing assertion (`GET /ui/map.json` < 500 ms) is skipped under `-short`, like the existing
  SC-007 perf test; when `HNE_MAP_RUN_OUT` is set the test writes the generated run JSON there.
  In `.github/workflows/ci.yml` set `HNE_MAP_RUN_OUT=map-250-run.json` for the integration suite
  and upload it as artifact `map-250-run.json` (used by quickstart §7)
- [X] T025 Docs: `specs/001-lan-inventory-topology/contracts/web-ui.md` Map rows and "Map JSON
  shape" point to `specs/010-device-topology-map/contracts/web-ui-changes.md`;
  `specs/001-lan-inventory-topology/tasks.md` T084–T090 marked "replaced by feature 010"
- [ ] T026 CI green (including `test-js`), merge to `main`, image built, deployed to ocean
  (standing instructions)
- [ ] T027 Real network: run quickstart.md §1–§7 on ocean (including the 2 s draw and 1 s toggle
  timings); record the counts, timings and observations in this task

## Revision 2026-10-24 (real network)

- [ ] T028 [US1] The force layout (`cose`) drew the real map unreadable: subnet boxes stacked on
  each other, stretched, devices as dots. Test in `internal/web/jstest/map-model.test.mjs`
  `layout`: every node gets a position, no two share one, groups side by side without overlap,
  the gateway on its group's top row, each port below its router with its devices in the port's
  column below it, empty topology → `{}`. Implement `layout(topo)` in
  `internal/web/static/map-model.js` (groups left to right; per group: gateways on the top row,
  one column per port with its devices stacked under it, other devices in a grid) and draw with
  Cytoscape's `preset` layout in `map.js`, with labels truncated to the column width

## Dependencies

T001, T002 (with T007) → T003/T004 (foundational) → US1 (T005–T009) → US2 (T010–T013) → US3
(T014–T015) and US4 (T016–T017) in any order → US5 (T018–T023) → Polish (T024–T027). US5's store
and form work (T018, T019, T022) depends only on Phase 2 and can run in parallel with US2–US4.
All JS tests share `internal/web/jstest/map-model.test.mjs`, so they are added in story order;
each is still pushed red before its implementation.

## Parallel examples

- US1: T005 (store) ∥ T006 (web page) ∥ T007 (JS), then T008 → T009.
- US2: T010 (store) ∥ T011 (JS), then T012 ∥ T013.
- US5: T018 (store) ∥ T019 (web) ∥ T020 (integration) ∥ T021 (JS), then T022 → T023.

## Implementation strategy

Tests of each phase are pushed first and must fail in CI before the implementation. MVP = Phase 2
+ US1 (every device on the map, by subnet and status), deployable alone. Then US2 (connections),
which completes the owner's request; US3/US4 are page increments; US5 last. One PR per phase
group is fine; merge and deploy after each green PR (standing instructions).
