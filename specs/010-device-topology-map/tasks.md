---

description: "Task list for Device Topology Map"
---

# Tasks: Device Topology Map

**Input**: `specs/010-device-topology-map/` (spec.md, plan.md, research.md, data-model.md,
contracts/web-ui-changes.md, quickstart.md)

**Tests**: REQUIRED (constitution Principle II): tests first, pushed and red in CI, then the
implementation. CI only (no local Go toolchain). Time comes from an injected `now`
(`clock.Fake`); no real network in tests. Store tests seed data with
`internal/inventory/inventorytest` (`Harness.Ingest(Run{..., Vantage: ...})`, `ARP`, `Scan`, and
router-table observations as in `internal/inventory/apply_router_test.go`).

## Phase 1: Setup

- [ ] T001 Set `VERSION` to `0.11`

## Phase 2: Foundational (topology view and JSON endpoint)

**Purpose**: the `Topology` type, `store.Topology(ctx, now)` returning groups and device nodes
only, and `GET /ui/map.json` serving it in the contract shape. Every story builds on these.

- [ ] T002 [P] Test in `internal/web/map_test.go` `TestMapJSONShape`: on an empty store
  `GET /ui/map.json` is 200, `Content-Type: application/json`, `Cache-Control: no-store`, and
  decodes to `{"groups":[],"nodes":[],"edges":[]}` (arrays, never `null`); with one ingested device
  the node has exactly the keys of contracts/web-ui-changes.md (`id`, `kind`="device", `parent`,
  `device_id`, `label`, `ip`, `ips`, `mac`, `vendor`, `type`, `state`, `last_seen_ago`,
  `gateway_of`); unknown query parameters (`scope=online`) are ignored
- [ ] T003 Implement `internal/store/topology.go`: types `Topology{Groups []TopoGroup; Nodes
  []TopoNode; Edges []TopoEdge}` with JSON tags per the contract (`kind` ∈ `device`|`port` for
  nodes, `gateway`|`bridge`|`port`|`manual` for edges; `current` bool), and `Topology(ctx, now)`
  returning device nodes from `ListDevices(ctx, DeviceFilter{Now: now})` with empty slices for
  groups and edges; `internal/web/map.go`
  `handleMapJSON` (`writeJSON`, `Cache-Control: no-store`, `now` from `s.opts.Clock`); route
  `GET /ui/map.json` in `internal/web/server.go`. Make T002 pass

## Phase 3: User Story 1 - See every device in its place (P1) 🎯 MVP

**Goal**: all devices (online, stale, offline) grouped by subnet with Devices-list states.
**Independent test**: inventory with devices in three subnets in all three states → every device
once, right group, right state; `/map` page loads.

- [ ] T004 [P] [US1] Tests in `internal/store/topology_test.go`:
  `TestTopologyAllDevices` — ingest devices in 192.168.0.0/24, 192.168.1.0/24 and 192.168.8.0/24;
  make one offline (a later complete scan misses it) and one stale (`now` > 2 × interval after its
  last scan); `Topology(now)` has exactly the device ids of `ListDevices(DeviceFilter{})`, each
  once, with `state` equal to its `DeviceRow.State` and `last_seen_ago` equal to the feature-006
  `ago` text; `TestTopologyGroups` — group id `subnet-<id>`, `label` = subnet user name + " (" +
  CIDR + ")" when named, else the CIDR; `stale` from `SubnetStatus(now)`; a device's `parent` is
  the group of its first current address; a device with no current address on a visible subnet
  (its only subnet ignored) has parent `other` and the `other` group (label "Other") exists only
  then; a visible subnet with no devices has no group unless it is in a collector's latest run;
  merged-away devices never appear (FR-012)
- [ ] T005 [P] [US1] Tests in `internal/web/map_test.go` `TestMapPage`: `GET /map` is 200, marks
  the Map nav item current, contains `<div id="map"`, loads `/static/vendor/cytoscape.min.js` and
  `/static/map.js` (no `http://` or `https://` script sources), contains the switch with "All
  devices" checked by default and "Online only", the legend texts ("online", "offline", "gateway",
  "two subnets", "port / Wi-Fi", "last known", "manual"), and `<aside id="map-details"`; with an
  empty store the page shows "No devices yet"; `GET /static/map.js` is 200
- [ ] T006 [US1] Implement groups and device fields in `internal/store/topology.go` (R2, R3):
  label (user name, else hostname, else IP — same as the Devices list display name), `ip` =
  `PrimaryIP`, `ips` = all current addresses, `mac`, `vendor` (manufacturer), `type`, `state`,
  `last_seen_ago` (move/reuse the `ago` formatter so store and web share one implementation, e.g.
  export it from `internal/store` and call it from `internal/web/server.go`), groups from
  `SubnetStatus(now)` plus subnets of each collector's latest run, `other` group. Make T004 pass
- [ ] T007 [US1] Implement the page: `internal/web/map.go` `handleMap` renders
  `internal/web/templates/map.html` (layout, `Active: "map"`, empty-state text, switch, legend,
  `<div id="map">`, `<aside id="map-details">`); register the template in the page list and
  `GET /map` in `server.go`; add `internal/web/static/map.js`: fetch `/ui/map.json`, build
  Cytoscape elements (groups as compound parents, device nodes colored by `state` with the
  `--ok`/`--fg`/`--muted` CSS variable values read via `getComputedStyle`, offline nodes at lower
  opacity), `cose` layout, zoom/pan, hover tooltip "label · ip · mac · last seen … ago"; CSS for the
  map area (full width, height `calc(100vh - 12rem)`, min 480px), legend and panel in
  `internal/web/static/app.css`. Make T005 pass

**Checkpoint**: the Map tab shows every device in its subnet with the right status (US1).

## Phase 4: User Story 2 - See how devices are connected (P1)

**Goal**: gateway, two-subnet, port / Wi-Fi edges; last known for offline devices.
**Independent test**: router-table run with ports + desktop run with two interfaces → edges.

- [ ] T008 [P] [US2] Tests in `internal/store/topology_test.go`:
  `TestTopologyGateways` — (a) a router read for 192.168.0.0/24 with source address 192.168.0.1
  (outcome `ok`) makes the device holding 192.168.0.1 `gateway_of: ["subnet-<id>"]` and adds a
  `gateway` edge device → group; (b) a run whose vantage has route `{destination: "0.0.0.0/0",
  next_hop: "192.168.1.1"}` makes the device holding 192.168.1.1 gateway of 192.168.1.0/24; a
  next hop no device holds adds nothing; `TestTopologyBridges` — a device with current addresses in
  two subnets is one node (parent = first address's group) with a `bridge` edge to the other group;
  a collector whose latest vantage has interfaces in 192.168.1.0/24 and 192.168.8.0/24 with MAC M
  gets bridge edges for the device with MAC M; `TestTopologyPorts` — devices reported with `via`
  "LAN1", "LAN1" and "SSID-5G" by the router at 192.168.0.1 give port nodes `port-<routerDev>-LAN1`
  and `port-<routerDev>-SSID-5G` (kind `port`, parent = router's group, label = via), one edge
  router → port per port and one edge port → device per device, all `current: true`; after a later
  router read without one of them (device offline) its port edge has `current: false`; the most
  recent non-empty `via` wins after a move LAN1 → LAN2; no port nodes when the router address is
  not a device in the inventory
- [ ] T009 [US2] Implement in `internal/store/topology.go` (R4–R6): gateway sources (latest
  `run_sources` row per subnet with outcome `ok`, `ListRouters`, route next hops from the latest
  `collection_runs.payload_gz` of each collector — decode with the existing gzip/contract decode
  helpers), bridge edges (current addresses in other visible subnets; collector interfaces matched
  by MAC), port nodes/edges from each device's latest sighting with `via <> ''` (`current` =
  device state ≠ offline). Edge ids are stable strings (`gw-<dev>-<group>`, `br-<dev>-<group>`,
  `rp-<router>-<via>`, `pd-<dev>`). Make T008 pass
- [ ] T010 [US2] `internal/web/static/map.js`: draw edges by kind — gateway (thick), bridge
  (dashed, to the other group), port (thin; `current: false` dotted and muted), port nodes as small
  round labels; gateway devices with a distinct border; legend matches. Verified by quickstart §2

**Checkpoint**: US1 + US2 = the requested topology of all devices.

## Phase 5: User Story 3 - Focus on what is online (P2)

**Goal**: switch between All devices (default) and Online only, client-side.
**Independent test**: toggle hides offline devices and their edges; reload → All devices.

- [ ] T011 [US3] `internal/web/static/map.js`: the "Online only" switch hides nodes with
  `state: "offline"`, their edges, port nodes left with no visible device, and groups left empty;
  "All devices" shows everything; no refetch; state not persisted (page always opens on All
  devices, FR-004). Covered by T005 (default) and quickstart §3

## Phase 6: User Story 4 - Open a device from the map (P2)

**Goal**: click → details panel with a link to the device page.

- [ ] T012 [US4] `internal/web/static/map.js`: clicking a device fills `#map-details` with name,
  type, all IPs, MAC, vendor, status badge (same classes `badge s-<state>` and text as the Devices
  list: "online" / "online" / "offline"), "last seen … ago", connection (gateway of, port / Wi-Fi
  with "(last known)" when not current, manual link target) and a link "Open device page" to
  `/devices/<device_id>`; clicking the background or another device closes or replaces it. All
  text set with `textContent` (no HTML injection from hostnames). Covered by T002 (fields) and
  quickstart §4

## Phase 7: User Story 5 - Record a connection the scans cannot see (P3)

**Goal**: owner-set "connected via" links that survive scans and override ports.
**Independent test**: set link → manual edge; scan → still there; clear → gone.

- [ ] T013 [P] [US5] Tests in `internal/store/topology_test.go` `TestTopologyManualLinks`: after
  `SetUserLink(ctx, from, to, at)` the map has a `manual` edge `from → to` and no port edge for
  `from`; a later `ClearUserLink` removes it (rows are appended, never updated: the table has
  both rows); after `from` is merged into another device the edge starts at the surviving device;
  a link whose target is not on the map is dropped; links survive another ingest and
  `store.Rebuild`
- [ ] T014 [P] [US5] Tests in `internal/web/map_test.go` `TestDeviceLinkForm`: the device page has a
  `select name="via"` listing other devices (routers and access points first, the device itself
  absent) with the current link selected; `POST /devices/{id}/link via=<other id>` → 303 and the
  map JSON has the manual edge; `via=` clears it; `via=<id itself>` and an unknown id → 400;
  unknown device → 404; a cross-site POST (`Origin: http://evil.example`) → 403
- [ ] T015 [P] [US5] Integration test `tests/integration/us_map_test.go` `TestMapEndToEnd`: ingest
  `valid_full.json` (desktop, two interfaces) and `valid_router.json` (ports) through the upload
  API; `/ui/map.json` has every device of `/devices`, the router's port nodes and the desktop's
  bridge edges; set a manual link through the form, upload another run, the manual edge is still
  there; `/map` is 200
- [ ] T016 [US5] Implement `internal/store/userlinks.go` (`SetUserLink`, `ClearUserLink` append to
  `user_links` with identity keys and `at`; `latestUserLinks` = latest row per
  `from_identity_key`, resolved through `resolveDevice`), manual edges in
  `internal/store/topology.go` (id `mu-<from>`, port edge of `from` suppressed),
  `POST /devices/{id}/link` in `internal/web/map.go` + route, and the "Connected via" select with
  Save in `internal/web/templates/device.html` (data from a new `DeviceLinkOptions` query in the
  store). Make T013–T015 pass
- [ ] T017 [US5] `internal/web/static/map.js`: manual edges in their own color with an arrow;
  legend entry. Verified by quickstart §5

## Phase 8: Polish & Cross-Cutting

- [ ] T018 [P] Test `tests/integration/us_map_test.go` `TestMapPerformance`, skipped under
  `-short` like the existing SC-007 perf test (shared CI runners are noisy): ingest 250 devices
  in one generated run; `GET /ui/map.json` < 500 ms (SC-002 budget). In `-short` mode the same
  test still builds the 250-device map once and checks it has 250 device nodes
- [ ] T019 Docs: `specs/001-lan-inventory-topology/contracts/web-ui.md` Map rows and "Map JSON
  shape" point to `specs/010-device-topology-map/contracts/web-ui-changes.md`;
  `specs/001-lan-inventory-topology/tasks.md` T084–T090 marked "replaced by feature 010"
- [ ] T020 CI green, merge to `main`, image built, deployed to ocean (standing instructions)
- [ ] T021 Real network: run quickstart.md §1–§6 on ocean; record the counts and observations in
  this task

## Dependencies

T001 → T002/T003 (foundational) → US1 (T004–T007) → US2 (T008–T010) → US3 (T011) and US4 (T012)
in any order → US5 (T013–T017) → Polish (T018–T021). US3 and US4 touch only `map.js` and depend on
US1's page; US5's store and form work (T013, T014, T016) depends only on Phase 2 and can run in
parallel with US2–US4 if needed.

## Parallel examples

- Phase 2: T002 is the only test; T003 follows it.
- US1: T004 (store) ∥ T005 (web page), then T006 → T007.
- US5: T013 (store) ∥ T014 (web) ∥ T015 (integration), then T016.

## Implementation strategy

Tests of each phase are pushed first and must fail in CI before the implementation. MVP = Phase 2
+ US1 (every device on the map, by subnet and status), deployable alone. Then US2 (connections),
which completes the owner's request; US3/US4 are page-script increments; US5 last. One PR per
phase group is fine; merge and deploy after each green PR (standing instructions).
