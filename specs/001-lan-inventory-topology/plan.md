# Implementation Plan: LAN Inventory & Topology Explorer

**Branch**: `001-lan-inventory-topology` | **Date**: 2026-10-04 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/001-lan-inventory-topology/spec.md`

## Summary

This feature builds a self-hosted home-network inventory with two parts:

- **`hne-server`**: one Go binary that runs as a container on the NAS. It provides the web UI,
  the upload API, an embedded SQLite database, and a built-in collector for the NAS's own subnet.
- **`hne-collector`**: one Go binary that runs on the Windows desktop. It scans the subnets the
  desktop can see and uploads each run to the server.

How it works:

- **Discovery**: ARP sweeps (raw ARP on Linux; `SendARP` on Windows, which needs no admin rights
  or drivers), plus reverse DNS and mDNS for hostnames.
- **History**: uploaded runs are stored append-only as the source of truth. They are folded into
  fast projections that can be rebuilt (sightings, devices, events, links).
- **Map**: Cytoscape.js draws the map, inferring the topology at the subnet/gateway level from each
  collector's interfaces and routes, plus links the user sets by hand.

All UI assets are embedded in the binary, and the system makes no internet calls.

## Technical Context

**Language/Version**: Go ≥ 1.26 (one module, two binaries). Research R1.

**Primary Dependencies**:
- Standard library: `net/http` routing, `html/template`, `embed`
- `modernc.org/sqlite`: pure-Go SQLite
- `github.com/mdlayher/arp`: Linux raw ARP
- `golang.org/x/sys/windows`: IP Helper API
- `golang.org/x/net`: ICMP, `dns/dnsmessage` for mDNS
- `golang.org/x/crypto/bcrypt`
- Vendored front-end files: htmx and Cytoscape.js

**Storage**: SQLite (WAL) on the NAS data volume. Append-only runs plus rebuildable projections.
Research R5, [data-model.md](data-model.md).

**Testing**:
- `go test`, with unit tests next to the code.
- `tests/contract`: OpenAPI fixtures, checked on both sides.
- `tests/integration`: HTTP + temporary SQLite.
- `-tags hwtest` smoke tests run on real hardware only. Research R13.

**Target Platform**: the server runs on Linux in a container (amd64/arm64) on the NAS, with host
networking and `NET_RAW`. The collector runs on Windows 10/11 x64 (primary) and Linux.

**Project Type**: a web service with a server-rendered UI, plus a CLI collector.

**Performance Goals**:
- A /24 scan finishes and shows in the UI within 2 minutes (SC-002).
- A collector upload appears within 1 minute (SC-004).
- Pages load in under 2 seconds at 250 devices with 1 year of history (SC-007).

**Constraints**:
- No connections outside the home network (SC-008). PTR queries go only to an in-LAN resolver.
- No admin rights or drivers on the desktop.
- Single container image and single data volume.
- No fixed subnets: collectors discover private (RFC 1918) subnets at runtime (research R14).

**Scale/Scope**:
- About 250 devices, at most 16 subnets, about 2–3 collectors, one owner.
- About 350 MB of raw runs per year.
- About 10 UI pages.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-checked after Phase 1 design.*

| Principle | Gate | Pre-research | Post-design |
|-----------|------|--------------|-------------|
| **I. Local-Only & Private** | No telemetry or cloud; assets bundled; authenticated uploads; data stays on the LAN | ✅ No external services in scope | ✅ JS/OUI embedded (R9, R11). Bearer tokens stored hashed (R10). PTR queries only to an in-LAN resolver (R2). Integration test guards outbound connections (R13) |
| **II. Test-First** | Network behind interfaces; contract tests on both sides; tests before code | ✅ Planned | ✅ `Prober`/`Resolver`/`NeighborTable`/`RouteReader`/`Clock` interfaces. Contract fixtures in `tests/contract` used by both server and collector. tasks.md must order each test task before its implementation task |
| **III. Simplicity / YAGNI** | Justify every dependency or component; one process + local datastore | ✅ | ✅ One module. Two binaries (required by IV). SQLite in-process. No SPA build. No message broker. No built-in scheduler installer for Windows. Each dependency fills a gap the standard library can't (raw ARP, Windows IP Helper, SQLite, bcrypt) |
| **IV. Distributed Collection** | Server + collectors; collectors stateless and report observations; versioned contract; feasibility confirmed | ✅ | ✅ The built-in collector uses the same engine and the same ingest path. Collectors keep only a spool. `/api/v1` + `schema_version`. Feasibility confirmed on the actual desktop: two on-link interfaces, `SendARP` works without elevation (R3) |
| **V. Faithful History** | Observations with time, source, subnet; projections reproducible; explicit identity; no silent overwrite; explicit retention | ✅ | ✅ Append-only `collection_runs` + user facts keyed by `identity_key`. Rebuild replays scans and user facts in ingest order. Offline status decided from scan data only (R7). Monotonic device state for late uploads. Rebuild invariant is tested. Identity rules in R6. No retention: history kept indefinitely |
| **Deployment constraints** | Single image + data volume; config not hard-coded; nothing installed on the viewing machine | ✅ | ✅ `compose.yaml` with host networking. No subnet list: subnets are discovered at runtime, private ranges only (R14, constitution v1.1.0). Collector is one downloadable file |

**Result**: PASS. There are no violations, so Complexity Tracking is empty.

## Project Structure

### Documentation (this feature)

```text
specs/001-lan-inventory-topology/
├── plan.md              # This file
├── research.md          # Phase 0: decisions R1–R13
├── data-model.md        # Phase 1: facts, projections, state machine
├── quickstart.md        # Phase 1: end-to-end validation guide
├── contracts/
│   ├── collector-upload-api.yaml   # OpenAPI for /api/v1 (collector ↔ server)
│   ├── collector-cli.md            # hne-collector commands, config, exit codes
│   └── web-ui.md                   # owner-facing routes and map JSON
└── tasks.md             # Phase 2 (/speckit-tasks; not created here)
```

### Source Code (repository root)

```text
go.mod
cmd/
├── hne-server/          # main: config, open store, start HTTP + built-in collector loop
└── hne-collector/       # main: CLI (check | scan | run | version)

internal/
├── contract/            # v1 upload types, validation, schema_version constants (shared)
├── collect/             # scan engine: plans subnets, runs probes in parallel, builds a CollectionRun
│   ├── probe.go         #   Prober / NeighborTable / RouteReader / Resolver interfaces
│   ├── arp_linux.go     #   raw ARP + /proc/net/arp
│   ├── arp_windows.go   #   SendARP + GetIpNetTable2
│   ├── icmp_tcp.go      #   fallback probe for routed subnets
│   ├── routes_*.go      #   vantage report per OS
│   └── names.go         #   reverse DNS (LAN-only resolver) + mDNS
├── upload/              # collector side: HTTP client, spool, backoff
├── ingest/              # server side: validate → store run → fold into projections
├── inventory/           # identity, merge/split, sightings, events, offline rule, link inference
├── store/               # SQLite open/migrate, queries, rebuild
├── auth/                # owner password/session, collector tokens
├── oui/                 # embedded IEEE OUI table + lookup
└── web/                 # handlers, templates/, static/ (htmx, cytoscape, css) via embed

tests/
├── contract/            # fixtures/*.json + tests against collector-upload-api.yaml
└── integration/         # server end-to-end with temporary SQLite and scripted runs

deploy/
├── Dockerfile           # multi-stage → static binary on scratch/distroless
└── compose.yaml         # host network, NET_RAW, ./data:/data

tools/
└── gen-oui/             # release-time OUI table refresh (go generate)
```

**Structure Decision**: a single Go module, with shared logic in `internal/` and two thin `cmd/`
entry points. The server uses `internal/collect` for its built-in scanner, so both vantage points
run the same discovery code. Unit tests sit next to the code in `internal/`, following Go
convention. Cross-component contract and integration tests are in `tests/`.

## Open items (do not block tasks)

1. **NAS model/OS**: only affects the compose installation notes in the quickstart.

Resolved 2026-10-05: the subnet question. The application does not depend on any subnet
(research R14). Retention was removed from this feature (FR-024). The memory target was dropped.

## Complexity Tracking

No Constitution Check violations. Two deployable binaries are required by Principle IV, not a
deviation.
