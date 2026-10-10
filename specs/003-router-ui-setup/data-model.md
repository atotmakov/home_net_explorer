# Data Model: Router Setup from the Web UI

**Feature**: `003-router-ui-setup` | **Date**: 2026-10-10

Changes relative to features 001 and 002. Everything not listed is unchanged.

## Server (migration 0004)

### router_settings (owner configuration; mutable; not rebuilt)

| Column | Rules |
|--------|-------|
| id | INTEGER PRIMARY KEY; stable id used by collectors (`/api/v1/routers/{id}/login`) |
| identity_key | TEXT NOT NULL UNIQUE; the device's identity (one router per device) |
| model | TEXT NOT NULL; one of `router.Models()` (`huawei-hg8145v5`) |
| subnet | TEXT NOT NULL DEFAULT ''; preferred subnet CIDR for the address, '' = automatic |
| username | TEXT NOT NULL; 1–128 characters |
| password | TEXT NOT NULL; 1–128 characters; **plain text** (owner decision, TODO-SEC-1); never rendered |
| updated_at | TEXT NOT NULL; UTC RFC 3339 ms |

Rules:
- A row exists only while the device's type is "router"; saving another type deletes it.
- `Rebuild` doesn't read or clear this table.
- Resolution to a device: `devices.identity_key = identity_key`, following `merged_into` when the
  device was merged away.

### Resolved router (computed, not stored)

`ListRouters` returns, for each row whose device has a qualifying address (research R3):
`id`, `model`, `address` (current private IPv4 on the preferred subnet, else the most recent
current one), `subnet` (that address's subnet CIDR). Rows without one are excluded from the list
and reported on the device page ("no current private address").

### run_sources (rebuilt)

Same columns as feature 002; `outcome` CHECK adds `login_unavailable`.

## Contract (additive to v1)

### PingResponse

| Field | Change |
|-------|--------|
| routers | new, array of RouterRef (may be empty); never contains credentials |

### RouterRef

| Field | Rules |
|-------|-------|
| id | integer ≥ 1 |
| model | e.g. `huawei-hg8145v5` |
| address | private IPv4 |
| subnet | private CIDR /16–/30 containing `address` |

### RouterLogin (response of `GET /api/v1/routers/{id}/login`)

| Field | Rules |
|-------|-------|
| username | 1–128 characters |
| password | 1–128 characters |

### RunSource.outcome

Adds `login_unavailable`: the collector could not obtain the login from the server.

## Collector side

### hne-collector.json `routers[]` (feature 002 entry, relaxed)

| Field | Change |
|-------|--------|
| username, password | now optional **as a pair**: both empty = server-managed (informational, research R7); one without the other = config error |

### hne-collector.routers (cache, new)

The last `routers` list from ping (id, model, address, subnet), JSON, written with write-temp +
rename, mode 0600. Never contains credentials. Used only when ping fails.

### Rejection marker (feature 002)

For server-managed routers the hash covers model, address, subnet, username and password as
fetched from the server (research R6).
