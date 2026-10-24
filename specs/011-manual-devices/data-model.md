# Data Model: Manual Devices

## Migration 0009 (`internal/store/migrations/0009_manual_devices.sql`)

```sql
-- Schema v9 (feature 011): devices the owner adds by hand.

-- Owner facts (append-only, replayed by a rebuild like user_device_attrs).
CREATE TABLE user_manual_devices (
    id           INTEGER PRIMARY KEY,
    identity_key TEXT NOT NULL,                      -- manual:<uuid>
    field        TEXT NOT NULL CHECK (field IN ('add', 'mac', 'remove')),
    value        TEXT NOT NULL,                      -- mac: normalized MAC or ''; otherwise ''
    at           TEXT NOT NULL
);
CREATE INDEX user_manual_devices_key ON user_manual_devices (identity_key);

-- Projection flag.
ALTER TABLE devices ADD COLUMN manual INTEGER NOT NULL DEFAULT 0;
```

No data migration: existing devices are not manual.

## Entities

### Manual device (projection row in `devices`)

| Field | Value |
|-------|-------|
| `identity_key` | `manual:<uuid>` (fixed at add, research R2) |
| `identity_strength` | `strong` |
| `manual` | 1 |
| `status` | `online` (storage only; shown state is `manual`, R4) |
| `mac` | NULL or the normalized MAC from the latest `mac` fact |
| `manufacturer` | OUI vendor of the MAC, '' without |
| `first_seen`, `last_seen` | time of the `add` fact |
| `user_name`, `type`, `notes` | from `user_device_attrs`: name required at add, 1–100 characters after trimming; type one of `inventory.DeviceTypes` or ''; notes at most 2000 characters |
| addresses, sightings, events | none |

### Rules

- **Add** (FR-001): name required; MAC optional; when given it must parse as a 6-byte unicast MAC
  and must not be the MAC of a device that is not merged away (FR-008; the error names that
  device). One transaction: `add` fact, `mac` fact if any, then `name`, `type`, `notes` facts.
- **Edit MAC** (FR-007): same MAC rules (the device's own current MAC is allowed); '' clears it.
- **Remove** (FR-007, R5): only when `manual = 1`; `remove` fact plus `ClearUserLink` for each
  device linked to it; the projection row is deleted.
- **Merge by MAC** (FR-010, R3): when the applier sees MAC m on a scanned device and a manual row
  with `mac = m` exists, `mergeInto(manual, scanned)`.
- **Loops** (FR-009, R6): a link from A to B is refused when B's chain reaches A.
- **Shown state**: `manual` before any other rule; never online, stale or offline (FR-003).

## Rebuild

`loadUserFacts` adds `SELECT 'manual_device', 0, id, at, identity_key, field, value FROM
user_manual_devices` with order 0, so on equal times a device is added before its attributes are
applied. `ProjectionRules` does not change: existing data has no manual devices.
