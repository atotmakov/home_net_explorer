# Web UI Changes: Device Status Freshness

Changes to `specs/001-lan-inventory-topology/contracts/web-ui.md`.

## Devices list (`/devices`)

- Status column: badge `online` (green), `online (stale)` (gray, `title="last seen <age> ago"`),
  or `offline` (own style, distinct from gray). Text is always present (FR-009).
- Status filter (`status=`): accepted values `online`, `stale`, `offline` (option labels
  "online", "online (stale)", "offline"); anything else is ignored (shows all). `new`, `new_offline` are no longer offered.

## Device page (`/devices/{id}`)

- The heading badge uses the same three states and tooltip.

## Home page (`/`)

- Counts: `online`, `stale`, `offline` (was: online, offline). Each equals the number of devices
  the Devices filter returns for that state (SC-003).
- Subnets table "stale" badge: no complete scan within 2 × the collector's interval.

## Settings (`/settings`)

- The field "Mark a device offline after this many missed scans" is removed; a submitted
  `offline_multiplier` is ignored.
