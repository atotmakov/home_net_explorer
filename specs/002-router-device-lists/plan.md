# Implementation Plan: Router Device Lists

**Branch**: `002-router-device-lists` | **Date**: 2026-10-09 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/002-router-device-lists/spec.md`

## Summary

Collectors gain an opt-in **router source**. With the owner's router login (kept in
`hne-collector.json`), the collector reads the router's device list:
1. log in to the router's web interface,
2. fetch the list,
3. log out.

It reports the online devices as `router_table` observations with real MACs, hostnames and ports.
This brings routed subnets, such as `192.168.0.0/24` behind the GL.iNet router, up to the same
quality as directly attached ones, and makes offline detection there accurate.

The first model is the Huawei HG8145V5. Its protocol was verified read-only against the owner's
router (research R1).

The feature also adds `extra_subnets`: routed subnets scanned in addition to auto-discovery, used
as the ICMP/TCP fallback when a router read fails.

On the server, the change is small:
- additive v1 contract fields,
- a `run_sources` table and `sightings.via` (migration 0003),
- a router column on the Collectors page.

Identity, merging and offline rules are unchanged.

## Technical Context

**Language/Version**: Go ≥ 1.26 (same module as feature 001).

**Primary Dependencies**: none new. Standard library only: `net/http` with a cookie jar,
`encoding/base64`, `regexp`.

**Storage**: SQLite. Migration `0003` adds `run_sources` and `sightings.via`.

**Testing**:
- `go test` in CI.
- A fake HG8145V5 HTTP server for the protocol.
- The anonymized capture as the parser fixture.
- A secret-leak test (SC-004).
- `-tags hwtest` for the real router.

**Target Platform**: the collector on Windows (primary) and Linux. The server is unchanged apart
from the contract and the UI.

**Project Type**: an additive feature on the existing web service and CLI collector.

**Performance Goals**: a router read takes under 5 s on the LAN (4 HTTP requests) and never
delays the rest of the scan by more than its timeout (10 s).

**Constraints**:
- Read-only router access.
- Credentials never leave the collector machine (FR-006).
- No login retries after a rejection (FR-011).
- No new runtime dependencies.

**Scale/Scope**: 1–3 routers per collector (contract maximum 8), and up to ~250 devices per list.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-checked after Phase 1 design.*

| Principle | Gate | Pre-research | Post-design |
|-----------|------|--------------|-------------|
| **I. Local-Only & Private** | Data stays on the LAN; no external calls | ✅ | ✅ The router address must be private (FR-005). The password is stored only in the collector config and redacted from logs and output, with a test (SC-004). The upload carries no credentials (contract has no such fields) |
| **II. Test-First** | Tests before code; network behind fakes | ✅ | ✅ Fake router server and anonymized fixture. The hardware test is tagged and opt-in. Tasks order each test before its implementation |
| **III. Simplicity / YAGNI** | Justify new parts | ✅ | ✅ No new dependency, one new package (`internal/collect/router`) with a model registry of one. No server-side polling. Subnet defaults to /24 instead of a second parser |
| **IV. Distributed Collection** | Collectors observe, server merges; versioned contract; feasibility | ✅ | ✅ Router reads are a collector-side observation source. The v1 change is additive, with deploy order documented (server first). Feasibility verified on the real router (research R1) |
| **V. Faithful History** | Raw runs stored; projections reproducible | ✅ | ✅ `sources` are stored in the raw run and in `run_sources` (derived at ingest, like `run_subnets`). `sightings.via` is rebuilt from runs. Migration 0003 is tested with the harness |
| **Deployment constraints** | No fixed subnets; single image | ✅ | ✅ The router subnet is derived or configured, never hard-coded. The image is unchanged apart from code |

Spec note: feature 001's FR-007 forbids logins *during discovery scans*. Router reads are a
separate, explicitly configured source that uses the owner's own credentials (spec 002, FR-003),
so there is no conflict.

**Result**: PASS. No Complexity Tracking entries.

## Project Structure

### Documentation (this feature)

```text
specs/002-router-device-lists/
├── plan.md
├── research.md          # R1–R9 (R1 verified on the real router)
├── data-model.md        # contract additions, run_sources, sightings.via, rejection marker
├── quickstart.md
├── contracts/
│   ├── upload-api-changes.md        # additive v1 edits to the canonical OpenAPI file
│   ├── collector-config-and-cli.md  # routers[], extra_subnets, check output
│   └── router-hg8145v5.md           # the router requests and field mapping
├── checklists/requirements.md
└── tasks.md             # /speckit-tasks
```

### Source Code (changes)

```text
internal/collect/router/             # NEW: router sources
├── router.go                        #   Source interface, Result/Outcome, model registry, Config (secret password)
├── hg8145v5.go                      #   login → list → logout; parser for USERDevice/USERDeviceNew
├── hg8145v5_test.go                 #   fixture + fake router server tests
├── hg8145v5_hw_test.go              #   //go:build hwtest
└── testdata/hg8145v5_getlanuserdevinfo.asp   # anonymized capture
internal/collect/engine.go           # Routers + Extra subnets; R4 merge/fallback rules; sources in the run
internal/contract/v1.go, validate.go # router_table, via, hostname_source router, sources
internal/store/migrations/0003_router_sources.sql
internal/ingest/ingest.go            # store run_sources
internal/inventory/apply.go          # via in the sighting tuple
internal/store/collectors.go         # latest router outcome per collector
internal/web/templates/collectors.html
cmd/hne-collector/main.go            # routers[], extra_subnets, rejection marker, check output
specs/001-lan-inventory-topology/contracts/collector-upload-api.yaml   # canonical contract edits
tests/contract/fixtures/valid_router.json, invalid_router_*.json
VERSION                              # 0.3
```

**Structure Decision**: router access lives in `internal/collect/router`, used by the collector
engine. Both binaries contain it, because the NAS's built-in collector shares the engine, but only
the remote collector's config file can enable it in this feature. There is no server-side router
setting, so the NAS never holds router credentials.

## Complexity Tracking

No Constitution Check violations.
