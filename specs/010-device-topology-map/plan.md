# Implementation Plan: Device Topology Map

**Branch**: `010-device-topology-map` | **Date**: 2026-10-23 | **Spec**: [spec.md](spec.md)

## Summary

Build the Map tab that 001 planned but never delivered. `GET /ui/map.json` returns, computed at
request time, every device of the unfiltered Devices list grouped by subnet with its feature-006
state, plus the connections already in the data: subnet gateways (router reads and route next
hops), devices in two subnets, router ports / Wi-Fi interfaces (current, or last known for offline
devices) and owner-set "connected via" links. `GET /map` draws it with the vendored Cytoscape,
starts on "All devices", filters "Online only" client-side, and shows a details panel. The device
page gets a "Connected via" select.

## Technical Context

**Language/Version**: Go ≥ 1.26; plain JavaScript for one page script.
**Primary Dependencies**: none new. Cytoscape 3.34.3 and htmx 2.0.11 are already vendored in
`internal/web/static/vendor`.
**Storage**: SQLite; no schema change. Reads `devices`, `device_addresses`, `subnets`,
`sightings.via`, `run_sources`, latest `collection_runs.payload_gz` per collector, `user_links`;
writes `user_links` (append-only).
**Testing**: CI only: store tests (topology rules), web tests (JSON shape, link endpoint, page),
integration test (fixtures → map, manual link survives 3 scans), 250-device test; page logic
(elements, Online-only filter, details panel) in a pure module `static/map-model.js` tested with
`node --test` in a new CI job (research R9); the 250-device draw time (SC-002) and toggle time
(SC-003) are measured on the real network (quickstart §7).
**Target Platform**: server container on the NAS; any modern desktop browser on the LAN.
**Performance Goals**: map JSON for 250 devices < 500 ms on CI; page drawn < 2 s (SC-002);
toggle < 1 s (SC-003).
**Constraints**: no internet at runtime (Principle I); no new probing of routers or switches
when the map is opened (FR-011).
**Scale/Scope**: tens of devices today (28), up to 250.
**Version**: `0.11`.

## Design decisions

See [research.md](research.md): R1 view-time topology (the unused `links` table stays unused); R2
device set = `ListDevices` unfiltered; R3 grouping and the "Other" group; R4 gateways from router
reads and route next hops; R5 two-subnet devices and collector interfaces; R6 port nodes from
`sightings.via`, last known for offline devices; R7 manual links in `user_links`, overriding
ports; R8 Cytoscape page with client-side filter and a JSON-fed details panel; R9 tests.

## Constitution Check

| Principle | Result |
|-----------|--------|
| I. Local-only | ✅ Script and library served from the server; no new outbound traffic; the map uses only stored data |
| II. Test-first | ✅ Store, web, integration and page-logic (`node --test`) tests are pushed red before each implementation in tasks.md; time from an injected `now`; no real network in tests. Only the Cytoscape/DOM glue in `map.js` (no decisions of its own) is checked by hand (quickstart.md) |
| III. Simplicity | ✅ No new dependency, no migration, no new stored projection; one store function, one JSON endpoint, one page, one form endpoint |
| IV. Distributed collection | ✅ No collector or upload-contract change; vantage and router reads of every collector feed the map |
| V. Faithful history | ✅ Topology derived from stored observations at view time; manual links are append-only user facts that survive scans and rebuilds |

**Result**: PASS (re-checked after design: unchanged).

## Project Structure

### Documentation (this feature)

```text
specs/010-device-topology-map/
├── spec.md
├── plan.md                       # this file
├── research.md                   # R1–R9
├── data-model.md                 # Topology view, rules, link writes
├── quickstart.md                 # real-network validation
├── contracts/web-ui-changes.md   # /map, /ui/map.json, POST /devices/{id}/link
└── checklists/requirements.md
```

### Source Code (repository root)

```text
internal/store/topology.go          Topology(ctx, now): groups, nodes, edges (R2–R7)
internal/store/topology_test.go
internal/store/userlinks.go         SetUserLink / ClearUserLink, latest-link lookup, DeviceLinkOptions (R7)
internal/web/map.go                 GET /map, GET /ui/map.json, POST /devices/{id}/link
internal/web/map_test.go
internal/web/templates/map.html     page, switch, legend, details panel
internal/web/templates/device.html  "Connected via" select
internal/web/static/map-model.js    pure ES module: toElements, visibleIds (Online only), details (R8)
internal/web/static/map.js          ES module glue: fetch JSON, Cytoscape, switch, tooltip, panel DOM
internal/web/jstest/map-model.test.mjs  node --test tests of map-model.js (outside static/, not served)
.github/workflows/ci.yml            job test-js: node --test internal/web/jstest/; build needs it
internal/web/static/app.css         map layout, legend, panel
internal/web/server.go              routes
tests/integration/us_map_test.go    fixtures → map; manual link survives a scan; 250-device timing
specs/001-.../contracts/web-ui.md   Map rows point to 010; 001 tasks T084–T090 marked replaced
VERSION                             0.11
```

**Structure Decision**: existing single Go module; the map is a store view plus a web page, like
the Devices list. No new packages.

## Complexity Tracking

| Addition | Why needed | Simpler alternative rejected because |
|----------|------------|--------------------------------------|
| CI job `test-js` running `node --test` (Node 22 via `actions/setup-node`, CI only; no npm packages, no `package.json`, no lockfile; nothing added to the image) | Principle II: the Online-only filter, element building and details panel are decisions that need red-first tests | Manual checks only would violate Principle II; testing the JS through Go (an embedded JS engine) would add a runtime dependency |

No new runtime dependency, component or schema change.
