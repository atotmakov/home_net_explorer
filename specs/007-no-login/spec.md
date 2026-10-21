# Feature Specification: No Login for the Web UI

**Feature Branch**: `007-no-login`

**Created**: 2026-10-21

**Status**: Implemented

**Input**: Owner request: "also it is necessary to remove any password requirement"

## Context

Feature 001 (FR-030) put the web UI behind a single owner password. The owner, the only user, on
a home network, lost track of the password and wants no password at all. The UI becomes open to
anyone who can reach the server on the home network.

## Requirements

- **FR-001**: Every web UI page MUST open without a login, a first-run setup step or a cookie.
  Replaces 001 FR-030.
- **FR-002**: `GET /login` and `GET /setup` (old bookmarks) MUST redirect to `/`. There is no
  Log out button and no `POST /login`, `POST /setup` or `POST /logout`.
- **FR-003**: Cross-site form posts MUST still be refused (Origin / `Sec-Fetch-Site` checks of
  feature 001), so another web site cannot change settings or delete data through the owner's
  browser.
- **FR-004**: Collectors MUST still authenticate with their bearer tokens under `/api/v1/`
  (constitution, Principle I); this feature does not change collector authentication.
- **FR-005**: The stored owner password hash and the login sessions MUST be deleted (schema
  migration 7 drops the `sessions` table). "Drop all data" no longer has a login to keep.

## Consequences (accepted by the owner)

- Anyone on the home network who opens the UI can see the inventory, change device and router
  settings, add or remove collectors (and see a new collector's token) and run the maintenance
  actions (remove all devices, drop all data).
- Router passwords stay stored in plain text on the server (TODO-SEC in spec 003); they are
  still never shown in a page.

## Success Criteria

- **SC-001**: Opening `http://<nas>:8181/` shows the home page directly, with no password screen.
- **SC-002**: A cross-site POST is answered 403 and changes nothing; an upload without a valid
  collector token is answered 401.

## Tests

- `internal/web/access_test.go`: pages open, no cookies, `/login` and `/setup` redirect home,
  cross-origin POST refused, `/api/v1/ping` without a token is 401.
- `internal/store/store_test.go` `TestMigration0007`: sessions table and password hash removed,
  other settings kept.
- Integration tests run without a login step; `TestUS1RouterEditsRefuseCrossSite`.
- `deploy/smoke.sh`: fresh volume, home page opens, a collector added survives a restart.
