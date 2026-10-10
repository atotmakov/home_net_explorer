# Implementation Plan: Router Setup from the Web UI

**Branch**: `003-router-ui-setup` | **Date**: 2026-10-10 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/003-router-ui-setup/spec.md`

## Summary

The owner configures routers on the device page: when a device's type is "router", a router
section asks for the model (from `router.Models()`), the username, the password and, if the device
has several current addresses, which subnet's address to use. The server keeps these settings in
a new `router_settings` table, with the password in plain text (owner decision; see the spec's
Security TODO).

Collectors learn the router list from the server at every scan, through the `ping` they already
make for ignored subnets (new `routers` field: id, model, address, subnet, no credentials). Before
reading a router, the collector fetches its login from a new authenticated endpoint
`GET /api/v1/routers/{id}/login`, uses it for that read only, and forgets it. Feature 002's
rejection marker now hashes the fetched login, so changing the password in the UI re-enables the
router. The NAS's built-in collector reads the same settings directly from the store.

New collector configs list the UI routers (model, URL) without credentials; `hne-collector.json`
entries with their own credentials keep working and win for their address.

## Technical Context

**Language/Version**: Go ≥ 1.26 (same module).

**Primary Dependencies**: none new.

**Storage**: SQLite. Migration `0004`: `router_settings` (owner configuration, mutable, not a
projection) and a rebuild of `run_sources` to allow the new outcome `login_unavailable`.

**Testing**: `go test` in CI only (no local toolchain). Store/web tests with `storetest`,
integration tests over HTTP, collector tests with the `routertest` fake router and the fake
server in `cmd/hne-collector`, a secret-leak test extended to pages, configs and the server log.

**Target Platform**: server on the NAS (container), collector on Windows/Linux.

**Project Type**: additive feature on the existing web service, API and CLI collector.

**Performance Goals**: one extra small HTTP request per UI router per scan; no effect on scan
time beyond feature 002's router read.

**Constraints**:
- Passwords are never rendered in any page or config download (FR-004, FR-007, SC-002).
- Logins only to active collector tokens; never to browser sessions (FR-009, SC-005).
- Collector keeps logins in memory only (FR-010).
- Rejection protection (FR-014) holds for UI routers.
- No new runtime dependencies; `VERSION` becomes `0.4`.

**Scale/Scope**: 1–3 routers, 1–3 collectors.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-checked after Phase 1 design.*

| Principle | Gate | Pre-research | Post-design |
|-----------|------|--------------|-------------|
| **I. Local-Only & Private** | Data stays on the LAN; collector traffic authenticated | ✅ | ✅ Router logins stay between the NAS and collectors on the LAN; the login endpoint needs a collector token. Plain-text storage and plain-HTTP transport are owner decisions recorded in the spec (TODO-SEC-1/2); no data leaves the LAN |
| **II. Test-First** | Tests first; network behind fakes | ✅ | ✅ Fake router and fake server; tests listed before each implementation in tasks |
| **III. Simplicity / YAGNI** | Justify new parts | ✅ | ✅ No new dependency or component. Router list rides on the existing ping; one new endpoint; one table |
| **IV. Distributed Collection** | Collectors observe, server merges; versioned contract | ✅ | ✅ Additive v1 changes (ping `routers`, new endpoint, new outcome), server upgraded first as in 002. Collectors stay stateless with respect to the inventory (they only cache the router list, no credentials) |
| **V. Faithful History** | Raw runs stored; projections reproducible | ✅ | ✅ `router_settings` is owner configuration, not a projection, so `Rebuild` neither reads nor clears it. Its history is not kept on purpose: old passwords must not accumulate (research R2) |
| **Deployment constraints** | No fixed subnets; credentials are configuration | ✅ | ✅ Router addresses come from discovered devices; router logins are owner configuration in the server |

**Result**: PASS. No Complexity Tracking entries. The reversal of feature 002 FR-006 is a spec
decision by the owner, not a constitution violation (Principle I is about leaving the LAN).

## Project Structure

### Documentation (this feature)

```text
specs/003-router-ui-setup/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── api-changes.md          # ping.routers, GET /api/v1/routers/{id}/login, outcome login_unavailable
│   ├── web-ui-changes.md       # device page router section, Collectors config
│   └── collector-changes.md    # server-managed routers, check output, precedence
├── checklists/requirements.md
└── tasks.md                    # /speckit-tasks
```

### Source Code (changes)

```text
internal/store/migrations/0004_router_settings.sql
internal/store/routers.go               # router_settings CRUD, ListRouters (resolved addresses), GetRouterLogin
internal/web/pages.go                   # device page: router section; type change removes settings
internal/web/templates/device.html
internal/web/api.go                     # ping.routers; GET /api/v1/routers/{id}/login
internal/web/collectors.go              # config JSON lists UI routers (no credentials)
internal/contract/v1.go, validate.go    # PingResponse.Routers, RouterRef, RouterLogin, OutcomeLoginUnavailable
internal/collect/router/router.go       # Config without credentials = server-managed; Remote source
internal/app/scanner.go                 # built-in collector reads UI routers from the store
cmd/hne-collector/main.go, routers.go   # router list from ping + cache, login fetch, marker hash, check
specs/001-lan-inventory-topology/contracts/collector-upload-api.yaml   # canonical contract edits
VERSION                                 # 0.4
```

**Structure Decision**: router settings are a store concern served by the web layer; the
collector side adds a `router.Remote` source that fetches its login through a function supplied
by the collector (HTTP for remote collectors, the store for the built-in one), so the router
package stays free of HTTP-to-server code.

## Complexity Tracking

No Constitution Check violations.
