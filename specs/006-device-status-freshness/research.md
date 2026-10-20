# Research: Device Status Freshness

## R1 Offline after one missed complete scan (stored rule)

- **Decision**: after a complete, not ignored scan R of subnet S is applied, every device with a
  current address in S whose status is online, that R did not see, **whose `last_seen` is
  before R's `started_at`**, **and that is not current in another subnet whose latest complete
  scan saw it** (that subnet's latest complete scan, among those finished at or before R,
  started at or before the device's `last_seen`), becomes offline with a `went_offline` event at R's `finished_at`.
  The old conditions (time ≥ multiplier × interval; every active collector completed ≥ multiplier
  scans after `last_seen`) are removed.
- **Rationale**: the owner chose "offline as soon as the latest complete scan misses it". The
  `last_seen < R.started_at` guard makes late uploads harmless: a spooled scan that finished
  before a newer scan saw the device cannot mark it offline (the newer sighting is after R
  started). It reads only stored facts in ingest order, so a rebuild reaches the same result
  (Principle V).
- **Note** (implementation): "the other subnet's scan finished after `last_seen`" was the first
  wording; it is also true when that scan missed the device, so the check uses the scan's start.
- **Alternatives**: compute offline only at view time from the latest complete scan per subnet
  (rejected: history would not show "went offline", and the Devices page and History would
  disagree); keep the multiplier with N = 1 (rejected: the owner asked to remove the setting, and
  the "active collectors" condition would still delay offline when two collectors scan a subnet).

## R2 Stale ("online (stale)") is computed at view time

- **Decision**: a stored-online device is shown stale when `now − last_seen > 2 × I`, where I is
  the `interval_seconds` of the most recent complete scan (any collector) of any subnet in which
  the device has a current address. A device with no such scan (only incomplete or skipped scans)
  uses the built-in scan interval from Settings (default 900 s).
- **Rationale**: FR-002a/FR-005/FR-006. Time-based, so never stored (Principle V: no fact
  depends on the wall clock), same pattern as `SubnetInfo.Stale`.
- **Alternatives**: the interval of the run that last saw the device (rejected: a device last
  seen by a rarely-running collector would stay green longer than the subnet's normal scans
  justify; the subnet's latest scan is what "should have seen it").

## R3 Shown states and filter values

- **Decision**: `DeviceRow.State` ∈ {`online`, `stale`, `offline`} derived from the stored
  status (`online`/`new` → online or stale; `offline`/`new_offline` → offline). Badge text
  (revision 2026-10-20): "online" (green), "online" (normal text color) for stale, "offline"
  (gray), each with the tooltip "last seen … ago"; the filter labels stale "online (stale)". The filter offers
  online, stale, offline; `new`/`new_offline` are dropped from it (never produced since 001).
- **Rationale**: FR-001, FR-007, US3; the stored CHECK constraint stays as it is (no migration
  for statuses).

## R4 Remove the offline-multiplier setting

- **Decision**: drop the Settings field, its validation and its default; the app no longer
  writes `offline_multiplier`; an existing row is ignored (left in the table, harmless).
  Subnet staleness on the home page uses the same freshness rule (no complete scan within
  2 × that collector's interval) instead of the multiplier.
- **Rationale**: FR-010; one freshness rule for devices and subnets.

## R5 Re-derive existing data once

- **Decision**: a setting `projection_rules` holds the version of the derivation rules; at
  start-up, if it is below `6`, the server runs the existing full rebuild (under the ingester
  lock) and stores `6`. The rebuild replays all stored scans with the new offline rule.
- **Rationale**: Principle V ("derived state reproducible from stored observations"): without
  it, today's statuses and history were derived under the old rule and a later rebuild would
  change them silently. Home inventories are small; the rebuild takes well under a second.
- **Alternatives**: leave old data (rejected: inconsistent with the rule); a SQL migration
  (rejected: the derivation lives in Go).
