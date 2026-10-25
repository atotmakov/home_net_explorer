# Research: Device Topology Map

All questions are resolved from the existing code and data; no NEEDS CLARIFICATION remain.

## R1 Topology is computed at view time, not stored

- **Decision**: `store.Topology(ctx, now)` builds the map from the inventory each time `/ui/map.json`
  is requested. The `links` projection table (schema 1, never written) stays unused.
- **Rationale**: every input already exists (devices, current addresses, sightings with `via`,
  run sources, the latest run's vantage, `user_links`). Computing at view time needs no migration,
  no change to ingest or rebuild, and is trivially reproducible from stored observations
  (Principle V), like the stale state of feature 006. The network is small (tens of devices, at
  most 250 per SC-002), so one request runs a handful of indexed queries.
- **Alternatives**: recompute `links` in `inventory.Apply` (001 T087): more code on the ingest path,
  needs a rebuild of existing data and a migration story, and still needs view-time status.

## R2 The device set is the Devices list

- **Decision**: map devices = `ListDevices(DeviceFilter{})` with the request's `now`; each device
  carries `State` (online / stale / offline) and `Age` from feature 006.
- **Rationale**: FR-001 and SC-001 demand exactly the unfiltered Devices list; reusing the same
  query makes them equal by construction (removed and `merged_away` devices are already excluded,
  FR-012).

## R3 Grouping

- **Decision**: a device's group is the subnet of its first current address (`PrimaryIP`, the
  order used on the Devices list). Devices with no current address on a visible subnet go to a
  group `other` ("Other"). Subnet groups are the visible (not ignored) subnets that hold at least
  one device, plus every subnet a collector scanned in its latest run (edge case "always shown").
  A group carries the subnet's name (user name, else CIDR) and `stale` from `SubnetStatus`.

## R4 Gateways

- **Decision**: a device is the gateway of subnet S when one of its current addresses in S is
  (a) the address of a router read for S (latest `run_sources` row with outcome `ok`, or a router
  set up in the UI, `ListRouters`), or (b) the `next_hop` of a route in the vantage of a
  collector's latest run, where the next hop lies in S. Vantage is read from the latest
  `collection_runs.payload_gz` of each collector (a few small gzip documents per request).
- **Rationale**: both sources are already collected; no new probing (FR-011, 001 FR-028).

## R5 Devices in two subnets

- **Decision**: a device with current addresses in more than one visible subnet appears once (R3)
  with a `bridge` edge to each other subnet. A collector whose latest vantage has interfaces in two
  subnets is matched to its device by interface MAC and gets the same edges.
- **Removed 2026-10-25** (owner): the collector rule hung the link on one of the PC's two cards
  while the other card was drawn as its own device in the other subnet. Each network card (MAC) is
  a device; no `bridge` edges are produced.

## R6 Router port / Wi-Fi

- **Decision**: for each device, take its most recent sighting with a non-empty `via`
  (router_table scans, feature 002). The router is the gateway device of that sighting's subnet
  found by R4(a). The map gets one `port` node per (router, via) in the router's group, an edge
  router → port, and an edge port → device. The device edge is `current: true` when the device
  is online or stale, `false` (drawn "last known", dashed) when it is offline.
- **Rationale**: matches FR-007; port nodes make "devices behind LAN1" visible as a cluster without
  a new data concept. If no router device is in the inventory, the port edges are omitted.

## R7 Manual links

- **Decision**: `user_links` (schema 1, append-only, keyed by identity keys, `removed` flag) holds
  the owner's links; the latest row per `from_identity_key` wins. Both ends are resolved through
  merges (`resolveDevice`). A manual link replaces the device's port edge (FR-009). New endpoint
  `POST /devices/{id}/link` with `via=<device id>` (empty `via` removes); the device page gets a
  "Connected via" select. Cross-site posts are refused as for every form (feature 007 FR-003).
- **Rationale**: user facts are inputs, not projections, so they survive scans and rebuilds; the
  table and its "drop all data" handling exist already.

## R8 Front end

- **Decision**: page logic lives in a pure ES module `static/map-model.js` with no DOM or
  Cytoscape access: `toElements(topology)` (groups, nodes, edges → Cytoscape element data with
  status, gateway, edge-kind and last-known classes), `visibleIds(topology, onlineOnly)` (ids to
  show: offline devices, their edges, ports left without a visible device and empty groups are
  hidden when `onlineOnly`) and `details(topology, nodeId)` (panel rows: label/value pairs and the
  device-page link, with "(last known)" for non-current ports and the manual target).
  `static/map.js` (`<script type="module">`) only wires these to the DOM and Cytoscape.
  `GET /map` renders `map.html`; `map.js` fetches `/ui/map.json` once and
  draws it with the vendored Cytoscape 3.34.3 (already in `static/vendor`, served locally), using
  compound nodes for groups and the built-in `cose` layout. The "All devices / Online only" switch
  hides offline nodes and their edges client-side (no refetch; SC-003). Hover shows a tooltip
  (name, IP, MAC, "last seen … ago"); click fills a side panel from the node data with a link to
  `/devices/{id}`. Colors use the existing status CSS variables.
- **Rationale**: no new dependency (Principle III); no CDN (Principle I). Client-side filtering is
  instant and keeps one endpoint. The side panel needs no extra server endpoint because the JSON
  carries the details (removes 001's `?partial=1` plan).
- **Alternatives**: server-rendered SVG (layout logic in Go, much more code); `scope=` refetch as
  in the 001 contract (slower, two code paths).

## R9 Testing

- Store: `internal/store/topology_test.go` seeds devices, addresses, sightings with `via`, run
  sources, runs with vantage and user links, and checks groups, gateways, bridges, ports, current
  vs last known, manual precedence, merged/removed exclusion, "other" group, count equals
  `ListDevices`.
- Web: `internal/web/map_test.go` checks the JSON shape of `/ui/map.json`, `/map` renders with the
  local script, `POST /devices/{id}/link` set/clear and cross-site refusal.
- Integration: `tests/integration/us_map_test.go` ingests fixtures (NAS run, desktop run with two
  interfaces, router_table run with ports), checks the map, sets a manual link, ingests another
  run, link still there.
- Performance: 250 seeded devices. The timing assertion (`/ui/map.json` < 500 ms) is skipped
  under `-short`, which CI always uses (noisy shared runners, as for the existing SC-007 test);
  in `-short` mode the test still checks the 250-device map is complete. SC-002 (drawn < 2 s) and
  SC-003 (toggle < 1 s) are measured in the browser on the real network (quickstart §7).
- Page logic: `internal/web/jstest/map-model.test.mjs` imports `../static/map-model.js` and runs
  with `node --test "internal/web/jstest/*.test.mjs"` (built-in runner, no packages) in a CI job
  `test-js` on `ubuntu-latest` with `actions/setup-node` pinned to Node 22, which detects ES module
  syntax in `.js` files (no `package.json` needed). The test file is outside
  `static/`, so it is neither embedded nor served. `build` needs `test-js`.
- Only the Cytoscape/DOM glue in `map.js` is checked by hand in quickstart.md; it makes no
  decisions of its own.
