# Data Model: Maintenance Actions

Extends `specs/001-lan-inventory-topology/data-model.md` and `specs/003-router-ui-setup/data-model.md`.

## Migration 0005 (`0005_collector_removal.sql`, first line `-- hne:foreign-keys-off`)

`collectors` is rebuilt (SQLite 12-step procedure, foreign keys off, `foreign_key_check` before
commit) to:

| Column | Change |
|--------|--------|
| name | `UNIQUE` constraint removed from the table; replaced by `CREATE UNIQUE INDEX collectors_name_active ON collectors (name) WHERE deleted_at IS NULL`. The name CHECK is unchanged |
| deleted_at | **new**, TEXT NULL. Set when the collector is removed (research R3). Only `remote` collectors are ever removed |

All other columns and every row are copied unchanged, ids included, so references from
`collection_runs`, `subnets`, `sightings` and `events` stay valid. Migration test: seed
`testdata/seed_4.sql` (v4 data with a builtin, an active and a revoked remote collector and their
runs), migrate, check rows, ids, `deleted_at IS NULL`, `foreign_key_check` empty, and that a
second collector with an existing active name is still refused.

## Collector states

```text
active ──revoke──▶ revoked ──remove all collectors──▶ removed (deleted_at set, token_hash NULL)
   └──────────────remove all collectors──────────────▶ removed
removed ──remove all devices / drop all data (no runs left)──▶ row deleted
```

- **removed**: not listed, token refused (`invalid_token`), name free for a new collector, its
  runs and projections unchanged; shown as `<name> (removed)` where history names it.
- The `builtin` collector (`nas`) is never removed.

## Settings (new keys)

| Key | Value | Written by | Kept by "drop all data" |
|-----|-------|------------|-------------------------|
| `builtin_paused` | time the scanner was paused (FormatTime), or absent when running | Pause / Resume | no (deleted = running) |
| `data_reset_at` | server time (FormatTime) of the latest "remove all devices" or "drop all data" | those two actions | yes |

The owner password hash (feature 001) is also kept by "drop all data"; every other key is deleted
and the start-up defaults (`builtin_interval_seconds`, `offline_multiplier`) are written again.

## What each action removes

| Data | Remove all devices | Remove all collectors | Drop all data |
|------|--------------------|-----------------------|---------------|
| `collection_runs`, `run_subnets`, `run_sources` | all | kept | all |
| Projections (`devices`, `device_addresses`, `sightings`, `events`, `links`, `subnets`) | all | kept | all |
| `user_device_attrs`, `user_identity_alias`, `user_links`, `user_acks` | all | kept | all |
| `user_subnet_attrs` | **kept** | kept | all |
| `router_settings`, setting `router_rejected_builtin` | all | kept | all |
| Remote collectors | removed ones with no runs left are deleted | all marked removed | all deleted |
| Built-in collector row | kept | kept | kept |
| `sessions`, owner password | kept | kept | kept |
| Other settings | kept | kept | reset to defaults |
| `builtin_paused` | kept | kept | deleted (running) |
| `data_reset_at` | set to now | unchanged | set to now |

Every action runs in a single transaction inside `Ingester.Do` (all or nothing, FR-004;
serialized with ingest). After each, `RebuildTx` on the remaining facts yields the same
projections (FR-007), which the tests check.

## Counts reported (FR-005)

| Action | Counts |
|--------|--------|
| Remove all devices | `devices` (rows of `devices` not merged away), `runs` (collection_runs deleted) |
| Remove all collectors | `collectors` (remote collectors newly marked removed) |
| Drop all data | `devices`, `runs`, `collectors` (remote collectors deleted, removed or not) |

## Ingest rule (research R2)

```text
start_on_server = run.started_at − (run.sent_at − received_at)
if data_reset_at is set and start_on_server < data_reset_at:
    do not store the run; update collectors.last_report_at / last_clock_skew_ms / last_version;
    answer status "discarded"
```

## Subnet facts on rediscovery (research R4)

- Creating a `subnets` row applies, per field (`name`, `ignored`), the latest `user_subnet_attrs`
  value for that CIDR with `at < received_at` of the run that creates it.
- `IgnoredSubnets` = CIDRs whose latest `ignored` fact is `true`.
