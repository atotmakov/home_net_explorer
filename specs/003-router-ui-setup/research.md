# Research: Router Setup from the Web UI

**Feature**: `003-router-ui-setup` | **Date**: 2026-10-10

Builds on features 001 and 002. Owner decisions (spec Context and Clarifications): plain-text
passwords on the server, every collector gets every router, logins fetched before each read,
router list from the server at each scan.

## R1. How collectors learn the router list (FR-007a, FR-011)

- **Decision**: add `routers` to the existing `GET /api/v1/ping` response, which every collector
  already calls before each scan (feature 001, for `ignored_subnets`). Each entry: `id`, `model`,
  `address`, `subnet`. No credentials. The collector caches the last list in
  `hne-collector.routers` next to its config (like `hne-collector.ignored`) for scans while the
  server is unreachable.
- **Rationale**: no new request per scan, same lifecycle as ignored subnets, additive to v1.
- **Alternatives considered**: a separate `GET /api/v1/routers` (one more request per scan for
  the same freshness); reading the list only from the downloaded config (rejected by the owner,
  option B).

## R2. Where and how settings are stored (FR-003)

- **Decision**: a mutable table `router_settings` keyed by the device's `identity_key`, one row
  per router: `id`, `identity_key`, `model`, `subnet` (preferred subnet, '' = automatic),
  `username`, `password` (plain text), `updated_at`. Not append-only and not a projection:
  `Rebuild` doesn't touch it.
- **Rationale**: owner facts in feature 001 are append-only for history, but an append-only log
  would keep every old router password forever; a single row that is replaced or deleted keeps
  only the current one. Keying by `identity_key` matches the owner's other device facts.
- **Merges/splits**: the router row follows the identity key; when a device is merged away, the
  row is resolved to the surviving device through `devices.merged_into`.
- **Alternatives considered**: storing it in `user_device_attrs` (history of passwords, rejected);
  storing it in `settings` as JSON (no per-device integrity).

## R3. Router address (FR-002)

- **Decision**: computed when the list is built, not stored: the device's current address on the
  preferred subnet, or, when `subnet` is empty, its current address with the latest `as_of`. Only
  private addresses qualify (`contract.IsPrivateAddr`). A router with no qualifying address is
  left out of the list and the device page says why. The router's served subnet is that address's
  subnet (`device_addresses` → `subnets.cidr`), not a /24 guess.
- **Rationale**: "the UI follows the device's current address" (spec edge case) without a second
  source of truth.

## R4. Login delivery (FR-008 – FR-010)

- **Decision**: `GET /api/v1/routers/{id}/login`, bearer collector token (same `bearerCollector`
  check as uploads, revoked tokens refused), response `{"username": "...", "password": "..."}`
  with `Cache-Control: no-store`. 404 `router_not_found` when the router no longer exists.
  Browser sessions are never accepted on `/api/v1/*` (feature 001 rule), so FR-009's "not to
  browser sessions" holds by construction; a test checks it.
- **Collector side**: a `router.Remote` source wraps the model, address and prefix plus a
  `login func(ctx) (user string, pass Secret, err error)`. Its `Read` calls `login`, builds a
  normal feature 002 source in memory with those credentials, reads, and drops them. A failed
  fetch gives outcome `login_unavailable`.
- **Server logs**: request logging must never include response bodies (checked; the server logs
  method, path and status only), and the login endpoint is added to the secret-leak test.
- **Alternatives considered**: putting credentials in the ping response (sends every password at
  every ping even when no read follows); pushing credentials into `hne-collector.json` (rejected
  by the owner).

## R5. New outcome `login_unavailable` (FR-012)

- **Decision**: add `login_unavailable` to `RunSource.outcome` (additive v1) for "the collector
  could not obtain the router's login from the server" (unreachable server, router removed,
  refused). Migration 0004 rebuilds `run_sources` because its CHECK constraint lists outcomes.
  The Collectors page and `check` say "login unavailable from the server".

## R6. Rejection protection for UI routers (FR-014)

- **Decision**: the feature 002 marker hash for a remote router covers model, address, subnet,
  username and password **as fetched**. Before contacting the router, the collector fetches the
  login, computes the hash, and skips the router (outcome `skipped_after_rejection`) when the hash
  is in the marker. A password change in the UI changes the hash, so the next scan tries again;
  `check` still clears the marker.
- **Rationale**: no server-side state about rejections, and the router is never contacted with a
  login already known to be wrong.

## R7. Precedence and the config file (FR-007, FR-013)

- **Decision**:
  - `hne-collector.json` router entries **with** username and password: as in feature 002, and
    they win: a server router with the same address is not read by that collector.
  - Entries **without** username and password ("server-managed"): informational; valid only as a
    pair of empty fields (one without the other is a config error). They are used only when the
    collector has neither a ping answer nor a cached list (first run with the server down), and
    then fail with `login_unavailable` anyway.
  - New configs from the Collectors page list every UI router as `{model, url}`.
- **Alternatives considered**: rejecting credential-less entries (would make downloaded configs
  invalid for old collectors' parsers; the new collector accepts them).

## R8. Built-in collector (spec assumption)

- **Decision**: the NAS's built-in scan loop (`internal/app/scanner.go`) builds the same
  `router.Remote` sources from `store.ListRouters`, with the login read from the store. It reports
  `unreachable` when the NAS can't reach the router, like any collector.

## R9. Device page and type changes (FR-001, FR-004 – FR-006)

- **Decision**: the router section appears when the saved type is "router". Fields: model
  (select from `router.Models()`), username, password (`type=password`, never prefilled,
  `autocomplete=new-password`; empty keeps the stored one), subnet (select, shown only when the
  device has several current addresses), plus Save and Remove. The page shows "Password: set",
  the resolved address, and the latest read outcome for that address from `run_sources` across
  collectors. Saving a type other than "router" deletes the router row in the same transaction.
  Same session, Origin and form protections as the other device edits (feature 001).

## R10. Testing

- Store: CRUD, address resolution (preferred subnet, latest, non-private excluded, merged device).
- Web: device page never contains the password (render with a marker password, search the HTML);
  type change removes the row; Collectors config lists routers without credentials.
- API: ping `routers`; login endpoint with valid, unknown, revoked token, no token and a browser
  session cookie (SC-005); `Cache-Control: no-store`.
- Collector: list from ping, cache when the server is down, login fetched per read, marker with
  fetched login, precedence, `check` output; the feature 002 secret-leak test extended to
  server-managed routers (marker never in files, output, uploads).
- Integration: owner configures a router in the UI → existing collector (fake router) scans →
  devices appear (SC-001); password change takes effect next scan (SC-003).
