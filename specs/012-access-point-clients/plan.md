# Implementation Plan: Access Point Client Lists

**Branch**: `012-access-point-clients` | **Date**: 2026-10-24 | **Spec**: [spec.md](spec.md)

## Summary

Read the client lists of the owner's two TP-Link EAP225 access points (standalone mode, two
firmware generations) at each scan, the way features 002/003 read the Huawei router. The owner
sets model and login on the access point's device page (stored like router settings); collectors
get the list from `ping`, fetch the login before each read, and report each client as an
`ap_client` observation carrying its Wi-Fi network, band and the access point's address. The
server stores that on the sighting; the 010 Map then hangs Wi-Fi devices under their access point
instead of the router's LAN1 port.

## Technical Context

**Language/Version**: Go ≥ 1.26 (server and collector).
**Primary Dependencies**: none new (`crypto/md5`, `net/http/cookiejar` from the standard library).
**Storage**: SQLite; migration 0008 adds `sightings.via_source`. AP settings reuse
`router_settings` (research R4).
**Testing**: CI only. `httptest` fake EAP225 (both firmware layouts); contract tests on both sides;
engine, store/topology, web and integration tests; `hwtest`-tagged real-device test; real-unit
confirmation through `check` (gate G1).
**Target Platform**: server container on the NAS; collector on the Windows desktop (reaches
192.168.0.x) and the NAS built-in collector.
**Project Type**: existing Go monorepo (server + collector).
**Performance Goals**: both AP reads in parallel with the router read, each bounded by 10 s
(SC-003).
**Constraints**: read-only towards the APs; never retry a rejected login (the newer firmware locks
after its retry budget); credentials only in memory on collectors; no CIDR literals outside tests
(lint).
**Scale/Scope**: 2 access points, tens of clients.
**Version**: `0.12` (server and collector).

## Constitution Check

| Principle | Result |
|-----------|--------|
| I. Local-only | ✅ Collectors talk only to LAN access points and the configured server; nothing leaves the home network |
| II. Test-first | ✅ Every step has red tests first; the AP is faked with `httptest`; time from the injected clock |
| III. Simplicity | ✅ No new dependency or table; AP settings reuse the router settings rows, login endpoint and rejection markers; one new package for the reader |
| IV. Distributed collection | ✅ Additive v1 contract change (new method, source type, outcome, field), server deployed first. **Technique confirmation**: endpoints and login were confirmed on both units without a login (research probe); the client fields are confirmed by gate G1 (`check` on the owner's units) before the map work depends on them |
| V. Faithful history | ✅ The AP address is in the raw run and in `sightings.via_source`; a rebuild reproduces the map |

**Result**: PASS, with gate G1 in tasks.md (re-checked after design: unchanged).

## Project Structure

### Documentation (this feature)

```text
specs/012-access-point-clients/
├── spec.md
├── plan.md                          # this file
├── research.md                      # probe of the real APs, R1–R9
├── data-model.md                    # migration 0008, entities, rules
├── quickstart.md                    # real-network validation
├── contracts/
│   ├── upload-api-changes.md        # ap_client, access_point sources, ping access_points
│   ├── access-point-eap225.md       # login + client list of the EAP225
│   └── web-ui-changes.md            # Access point card, map ports under APs
└── checklists/requirements.md
```

### Source Code (repository root)

```text
internal/contract/v1.go, validate.go     ap_client, Observation.AccessPoint, SourceTypeAccessPoint,
                                         OutcomeControllerManaged, MaxSources 16, Ping.AccessPoints
internal/collect/accesspoint/            new: eap225.go (login, both endpoints, parse), accesspoint.go
                                         (Source, Result, Client, Models), remote.go (login from server,
                                         rejections), testdata/, *_test.go, eap225_hw_test.go (hwtest)
internal/collect/engine.go               read APs in parallel with routers; ap_client observations
                                         replace router observations of the same IP; RunSource rows
cmd/hne-collector/accesspoints.go        build APs from ping (cache hne-collector.access-points); check
                                         output (endpoint, client count, key names)
internal/app/                            built-in collector gets ListAccessPoints too
internal/store/migrations/0008_access_points.sql  via_source; run_sources rebuilt (type, outcome)
internal/inventory/apply.go              via_source in the sighting tuple
internal/store/routers.go                ListRouters = router models; ListAccessPoints; model sets
internal/store/topology.go               latestVias with via_source; ports under the AP device
internal/store/devices_query.go          address row: via + AP name
internal/web/routers.go, api.go          AP card save/remove; ping access_points
internal/web/templates/device.html       Access point card; "via <AP> · <SSID>"
internal/web/collectors.go + template    access point reads in the source list
tests/integration/us_ap_test.go          fake AP + router → upload → map
specs/002, 003 contracts                 cross-references to 012
VERSION                                  0.12
```

**Structure Decision**: one new collector package beside `internal/collect/router`, mirroring
it; everything else extends existing files.

## Complexity Tracking

| Addition | Why needed | Simpler alternative rejected because |
|----------|------------|--------------------------------------|
| Migration 0008 (`sightings.via_source`, `run_sources` constraints) | The map must know which AP reported a Wi-Fi connection, reproducibly from stored runs (Principle V) | Encoding the AP in `via` would mix two facts in one 32-character label and break the router port labels' meaning |
| Two firmware endpoints in one reader | The owner's two EAP225s run different firmware generations (research probe) | Two models would make the owner pick the firmware generation |
