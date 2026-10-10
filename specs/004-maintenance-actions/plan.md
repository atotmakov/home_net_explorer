# Implementation Plan: Maintenance Actions

**Branch**: `004-maintenance-actions` | **Date**: 2026-10-10 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/004-maintenance-actions/spec.md`

## Summary

A "Maintenance" card on the Settings page offers four owner actions: remove all devices, remove
all collectors, pause/resume the NAS built-in scanner, and drop all data. Each destructive action
needs a typed confirmation word and runs as one transaction serialized with ingest.

- **Remove all devices** deletes facts (all runs, all device-related user facts, router settings)
  and projections together, so a rebuild still equals the live state (Principle V). Subnet facts
  are kept and re-applied when a subnet is rediscovered. A reset point (`data_reset_at`) makes the
  server discard runs that started before it (spooled uploads, an in-flight NAS scan), answered
  with a new upload status `discarded`.
- **Remove all collectors** soft-deletes remote collectors (`collectors.deleted_at`, token
  cleared) so their history keeps its source; migration 0005 rebuilds `collectors` to make names
  unique among active collectors only. This needs a small addition to the migration runner to run
  a migration with foreign keys off.
- **Pause/resume** is a stored setting read by the scanner loop, which a channel wakes on change.
- **Drop all data** = remove all devices + subnet facts + all remote collectors + settings back to
  start-up defaults (unpaused), keeping the owner password and sessions.

## Technical Context

**Language/Version**: Go ≥ 1.26 (same module).

**Primary Dependencies**: none new.

**Storage**: SQLite. Migration `0005`: `collectors.deleted_at`, partial unique index on active
names (table rebuild with foreign keys off). New settings keys `builtin_paused`, `data_reset_at`.

**Testing**: `go test` in CI only (no local toolchain). Store tests with `storetest` (counts,
rebuild invariant), migration harness (`testdata/seed_4.sql`), ingest test for `discarded`,
scanner tests with the fake clock and a fake engine, integration tests over HTTP (confirmation,
session, cross-site, token refusal, drop-all keeps login), contract schema test.

**Target Platform**: server on the NAS (container). No collector change.

**Project Type**: additive feature on the existing web service.

**Performance Goals**: each action completes in one transaction; at home-network scale (≤ a few
thousand runs) well under the 1-minute target of SC-001.

**Constraints**:
- All-or-nothing actions serialized with ingest (FR-004).
- Rebuild invariant holds after every action (FR-007).
- Owner-only, cross-site protected, typed confirmation (FR-002, FR-003).
- No collector change required (new upload status is accepted by existing collectors).
- `VERSION` becomes `0.5`.

**Scale/Scope**: 1–3 collectors, hundreds of devices, thousands of runs.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-checked after Phase 1 design.*

| Principle | Gate | Pre-research | Post-design |
|-----------|------|--------------|-------------|
| **I. Local-Only & Private** | Data stays on the LAN | ✅ | ✅ No new outbound traffic; actions only delete or pause |
| **II. Test-First** | Tests first; network behind fakes | ✅ | ✅ Every action has store, web and integration tests written before the code (tasks); scanner tested with fake clock/engine |
| **III. Simplicity / YAGNI** | Justify new parts | ✅ | ✅ No new component or dependency. One migration, two settings, five routes. The migration-runner addition (foreign keys off) is the minimum SQLite needs to drop a UNIQUE constraint; see Complexity Tracking |
| **IV. Distributed Collection** | Collectors observe, server merges; versioned contract | ✅ | ✅ Additive v1 change (upload status `discarded`), accepted by existing collectors unchanged |
| **V. Faithful History** | Raw runs stored; projections reproducible; no silent loss; retention explicit | ✅ | ✅ Deletion is an explicit owner action with typed confirmation, logged with counts, never automatic, so it is neither silent loss nor a hidden retention rule. Facts and projections are deleted together and a rebuild equals the live state after every action (tested). Removing collectors keeps their history and its source collector (soft delete). Pre-reset runs are discarded at the door rather than stored and hidden, so no stored observation is ever ignored by the projections |
| **Deployment constraints** | Fresh volume starts; schema migrations have a path | ✅ | ✅ Migration 0005 with a seeded-data test; fresh volume unaffected |

**Result**: PASS.

## Project Structure

### Documentation (this feature)

```text
specs/004-maintenance-actions/
├── plan.md
├── research.md           # R1–R8
├── data-model.md         # migration 0005, settings, what each action removes, ingest rule
├── quickstart.md
├── contracts/
│   ├── api-changes.md    # upload status "discarded"; removed tokens; ping ignored_subnets source
│   └── web-ui-changes.md # Maintenance card, routes, notices, paused state, "(removed)"
├── checklists/requirements.md
└── tasks.md              # /speckit-tasks
```

### Source Code (changes)

```text
internal/store/store.go                          # MigrateTo: "-- hne:foreign-keys-off" migrations
internal/store/migrations/0005_collector_removal.sql
internal/store/testdata/seed_4.sql
internal/store/maintenance.go                    # RemoveAllDevices, RemoveAllCollectors, DropAllData (in a tx), counts
internal/store/collectors.go                     # deleted_at; ListCollectors/overviews skip removed; Removed flag
internal/store/devices_query.go                  # "(removed)" names; IgnoredSubnets from facts
internal/store/routers.go                        # RouterStatus name "(removed)" (if shown)
internal/auth/token.go                           # Authenticate: deleted_at IS NULL (token_hash is also cleared)
internal/ingest/ingest.go                        # reset point: discard pre-reset runs
internal/contract/v1.go                          # StatusDiscarded
internal/inventory/apply.go                      # new subnet row applies prior subnet facts
internal/app/app.go, scanner.go                  # pause state, wake channel, SettingDefaults to web
internal/web/server.go, maintenance.go           # routes, handlers, notice; Scanner interface: Paused/SetPaused
internal/web/pages.go                            # settings data: maintenance card, notice
internal/web/templates/settings.html, home.html, collectors.html
specs/001-lan-inventory-topology/contracts/collector-upload-api.yaml   # status enum + discarded
VERSION                                          # 0.5
```

**Structure Decision**: the deletions are store functions taking a `*sql.Tx`, called by web
handlers inside `Ingester.Do` (as feature 003 router saves are), so they are serialized with
ingest and testable without HTTP. Pause state lives in settings, owned by the app's `Scanner`,
which the web layer reaches through its existing `Scanner` interface.

## Complexity Tracking

| Addition | Why needed | Simpler alternative rejected because |
|----------|------------|---------------------------------------|
| Migration runner support for foreign-keys-off migrations | Dropping `UNIQUE (name)` on `collectors` needs a table rebuild, and `collectors` is referenced by four tables; SQLite only allows that with foreign keys off, which can't be switched inside a transaction | Renaming removed collectors to free their names: the allowed name characters leave no collision-free form and the original name would be lost from history |
