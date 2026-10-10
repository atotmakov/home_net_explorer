# Research: Maintenance Actions

No open technical questions remained after the owner's decisions in the spec. These are the
design decisions taken while reading the code (features 001–003).

## R1. Deleting history without breaking the rebuild invariant (Principle V)

**Decision**: "Remove all devices" deletes facts and projections together in one transaction run
through `ingest.Ingester.Do` (serialized with ingest): `run_sources`, `run_subnets`,
`collection_runs`, `user_device_attrs`, `user_identity_alias`, `user_links`, `user_acks`,
`router_settings`, the setting `router_rejected_builtin`, then `store.ResetProjections`. Facts and
projections are emptied together, so "rebuild from facts" still equals the live projections (both
empty). `user_subnet_attrs` is kept (owner decision).

**Rationale**: Principle V requires derived state to be reproducible from stored observations.
Deleting only projections would let `hne-server rebuild` resurrect every device; deleting only
facts would leave projections that can't be reproduced. Doing both inside the ingest lock means
no upload interleaves with a half-done reset.

**Alternatives**: a "tombstone" fact that hides earlier runs (keeps raw history, which is what
Principle V prefers by default, but the owner asked for the data to be gone, and the database
would keep growing during repeated tests); deleting the database file (also deletes the owner).

## R2. Results collected before a reset and uploaded after it

**Decision**: store a **reset point** (setting `data_reset_at`, server time) when devices are
removed or all data is dropped. `Ingest` discards a run whose start, converted to server time,
is before the reset point: `started_at < data_reset_at`, using the run's own `started_at`
(collector clock) without skew correction. A discarded
run is not stored (no `collection_runs` row), its collector's `last_report_at` is still updated,
and the server answers `200` with the new upload status `discarded`. The setting survives every
later action, including "drop all data", so a late spooled upload is still discarded.

**Rationale**: collectors spool runs while the server is unreachable (feature 001) and the
built-in scan may be running during the reset; both would otherwise bring old devices back
(SC-002). Collectors already treat any `200`/`201` as accepted and drop the run from the spool
(`internal/upload/client.go`), so the new status needs no collector change. No skew correction is applied: collectors set `sent_at` once,
before spooling (`cmd/hne-collector/main.go`), so for a run resent hours later
`sent_at − received_at` measures the delay, not the clock error, and a "corrected" start would
land after the reset, letting exactly the spooled runs through (found by /speckit-analyze, U1).
Collectors run on LAN machines with synchronized clocks; a collector clock wrong by more than the
time between the reset and its next scan can misjudge one run, which is accepted.

**Alternatives**: reject with an error (the collector would keep resending or log it as
rejected); answer `duplicate` (wrong: the run was never stored, and the log would mislead).

## R3. Removing collectors while keeping their history

**Decision**: soft delete. Migration `0005` adds `collectors.deleted_at` and replaces the
table-level `UNIQUE (name)` with a partial unique index `ON collectors (name) WHERE deleted_at
IS NULL`, so a removed collector's name can be reused (FR-032) while its runs keep pointing at
it. Removal sets `deleted_at`, sets `revoked_at` if unset and clears `token_hash`, so
`auth.Authenticate` refuses the token with the existing `invalid_token` (FR-031).
`ListCollectors`, the Collectors page and the router/ping code skip deleted collectors; places
that show history by collector name (subnet "discovered by", device sightings, run status) show
`<name> (removed)`.

SQLite can't drop a `UNIQUE` constraint in place, and other tables reference `collectors`, so the
table is rebuilt with foreign keys switched off, following SQLite's documented 12-step procedure.
`PRAGMA foreign_keys` can't change inside a transaction, so the migration runner gets one
addition: a migration whose first line is `-- hne:foreign-keys-off` runs on a dedicated
connection with `foreign_keys=OFF`, checks `PRAGMA foreign_key_check` before committing, and
switches foreign keys back on.

When "remove all devices" later deletes all history, removed collectors with no runs left are
deleted for good, so they don't accumulate.

**Rationale**: `collection_runs.collector_id` (and projections) reference `collectors`; history
must keep its source collector (Principle V: observations recorded with their source collector).

**Alternatives**: rename removed collectors to free the name (the name charset leaves no
collision-free form, and the original name would be lost); reassign their runs to a placeholder
collector (rewrites history).

## R4. Keeping subnet names and ignore choices across "remove all devices"

**Decision**: two small changes so kept `user_subnet_attrs` take effect again when a subnet is
rediscovered:
- When the Applier creates a subnet row, it applies the latest `name` and `ignored` facts for that
  CIDR **made before the run was received** (`at < received_at`). Rebuild replays runs before
  facts on ties, so this gives the same result live and in a rebuild.
- `Store.IgnoredSubnets` (sent to collectors in `ping`) reads the latest `ignored` fact per CIDR
  from `user_subnet_attrs` rather than the `subnets` projection, so collectors skip an ignored
  subnet at the first scan after the reset, before it is rediscovered.

**Rationale**: today a subnet fact is applied with `UPDATE subnets … WHERE cidr = ?`, a no-op when
the subnet row doesn't exist yet, which is the situation right after the reset. Before this
feature a fact could only be written for an existing subnet, so the change doesn't alter existing
behavior.

**Alternatives**: delete subnet facts too (contradicts the owner's decision).

## R5. Pause / resume

**Decision**: the setting `builtin_paused` (the pause time, or absent when running) is the source of truth. The
Scanner gets `Paused(ctx) bool` and `SetPaused(ctx, bool) error`; `SetPaused` writes the setting
and wakes the loop through a `wake` channel. `Scanner.Run`:
- at start-up, skips the immediate scan when paused;
- while paused, waits only for `ctx`, `wake` or the trigger channel, without computing deadlines
  (no busy loop on a past deadline);
- after resume, scans at `lastStart + interval`, or at once if that time has passed (FR-025).

`Trigger` returns a new result "paused" when paused, so "Scan now" is refused; the scan-status
fragment shows "Paused" with a Resume button. A scan in progress is not interrupted (FR-021): the
pause only stops the next one. The `web.Scanner` interface gains `Paused` and `SetPaused`.

**Rationale**: a stored setting survives restarts and redeploys (FR-022) and needs no migration.
Waking the loop makes resume take effect without waiting for a long sleep to finish.

**Alternatives**: cancel the running scan on pause (loses a partial run, and the spec says let it
finish); an in-memory flag (the owner chose persistence).

## R6. Drop all data

**Decision**: one transaction in `Ingester.Do`: everything in R1, plus `user_subnet_attrs`,
**all** remote collectors (hard delete; their runs are already gone), `sessions` are **kept**, and
`settings` are deleted except the owner password hash and `data_reset_at`. The defaults are then
written again from the values the server was started with (`builtin_interval_seconds` from
`--scan-interval`/`HNE_SCAN_INTERVAL`, `offline_multiplier` 3), which the app passes to the web
layer as `web.Options.SettingDefaults`. Deleting `builtin_paused` un-pauses; the handler then calls
`Scanner.SetPaused(false)` to wake the loop. The built-in collector row is kept.

**Rationale**: matches a fresh install with the same owner (FR-042) without a restart.

## R7. Confirmation, protection and feedback

**Decision**: each destructive action is a `POST` form on the Settings page with a text field
`confirm`; the server compares it with the action's word (`devices`, `collectors`, `everything`)
and answers `400` with a message, changing nothing, when it differs. Pause and Resume need no
word. All routes use the owner-session and Origin/`Sec-Fetch-Site` protections every owner form
already gets (feature 001). On success the server redirects (`303`) to
`/settings?done=<action>&devices=N&runs=N&collectors=N`, and the page shows a message built only
from the action name and the integers (other query values are ignored). Each action logs one
`maintenance` line with its counts.

**Rationale**: typing a word makes accidental clicks harmless without JavaScript dialogs; the
redirect avoids re-submitting on reload.

**Alternatives**: a browser `confirm()` dialog (easy to click through, and doesn't protect a
replayed form post); a separate confirmation page (more templates for the same protection).

## R8. Version

**Decision**: `VERSION` becomes `0.5`. The API change is additive (a new upload status), so the
contract stays v1.
