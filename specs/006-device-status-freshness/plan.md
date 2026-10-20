# Implementation Plan: Device Status Freshness

**Branch**: `006-device-status-freshness` | **Date**: 2026-10-19 | **Spec**: [spec.md](spec.md)

## Summary

A device goes offline at the first complete scan of its subnet that misses it (stored rule,
replayed by rebuild). "Online" is shown green while `last_seen` is within two scan intervals of
now and "online" in the normal text color (stale) after that (computed at view time); offline
is gray; every badge has a "last seen … ago" tooltip (revision 2026-10-20). The Devices list, device page,
status filter and home counts use the three states. The "missed scans" setting is removed, and
existing data is re-derived once at start-up.

## Technical Context

**Language/Version**: Go ≥ 1.26. **Dependencies**: none new. **Storage**: SQLite, no schema
change; new internal setting `projection_rules`. **Testing**: CI only:
inventory unit tests (offline rule, late uploads, rebuild equality), store tests (state, interval
choice, counts), integration test (pages, filter, settings), app start-up test (one-time
rebuild). **Version**: `0.7`.

## Design decisions

See [research.md](research.md): R1 offline rule with the `last_seen < R.started_at` guard; R2
stale at view time with I from the subnet's latest complete scan; R3 shown states; R4 setting
removed, subnet staleness on the same rule; R5 one-time rebuild.

## Constitution Check

| Principle | Result |
|-----------|--------|
| I. Local-only | ✅ No new traffic or dependency |
| II. Test-first | ✅ Tests listed before implementation in tasks.md; time comes from an injected `now` |
| III. Simplicity | ✅ One rule simplified (offline), one view-time computation, no schema change |
| IV. Distributed collection | ✅ No contract or collector change; several collectors per subnet: the latest complete scan of any of them decides |
| V. Faithful history | ✅ Offline/online events derived from stored scans only, in ingest order; stale is never stored; existing derived data re-derived once under the new rule (R5) |

**Result**: PASS (re-checked after design: unchanged).

## Source changes

```text
internal/inventory/offline.go        new rule (R1); multiplier code removed
internal/inventory/offline_test.go   rewritten for the new rule
internal/store/devices_query.go      DeviceRow.State, freshness interval, filter, HomeCounts(now), SubnetStatus stale (R2–R4)
internal/app/app.go                  drop offline_multiplier default; one-time rebuild (R5)
internal/web/pages.go, server.go     statuses list, settings form without multiplier, counts
internal/web/templates/devices.html, device.html, home.html, settings.html
internal/web/static/*.css            s-stale (normal text color), s-offline (gray)
specs/001-.../contracts/web-ui.md, data-model.md (offline rule)
VERSION                              0.7
```

## Project Structure

Documentation: `specs/006-device-status-freshness/` (spec, plan, research, data-model,
contracts/web-ui-changes.md, quickstart, tasks). No new packages.
