# Data Model: Router Device Lists

**Feature**: `002-router-device-lists` | **Date**: 2026-10-09

Changes relative to `specs/001-lan-inventory-topology/data-model.md`. Everything not listed here
is unchanged.

## Collector side (not uploaded)

### RouterSource (collector config `routers[]`)

| Field | Rules |
|-------|-------|
| model | required; one of the supported models: `huawei-hg8145v5` |
| url | required; `http(s)://<private IPv4>[:port]` (FR-005) |
| username | required |
| password | required; secret: never logged, printed or uploaded (FR-006) |
| subnet | optional private CIDR /16–/30; default: the router address with /24 (research R5) |

### Rejection marker (`hne-collector.router-rejected`)

Holds the SHA-256 of each rejected router's configuration (model, url, username, password,
subnet). While a router's current configuration hash is listed, scans skip it. `check` clears the
marker (research R6, FR-011).

## Upload contract (additive to v1, research R3)

### SubnetScan

`method` adds `router_table`. Rules: `complete = true` only for a successful read;
`hosts_probed` = number of router entries inside the router's subnet (online + offline;
entries outside the subnet, such as `0.0.0.0`, are not counted).

### Observation

| Field | Change |
|-------|--------|
| method | adds `router_table`; MAC required (like `arp`) |
| hostname_source | adds `router` |
| via | new, optional, ≤ 32 characters: router port or Wi-Fi interface (`LAN1`, `SSID2`) |

### RunSource (new optional array `sources`)

| Field | Rules |
|-------|-------|
| type | `router` |
| model | e.g. `huawei-hg8145v5` |
| address | the router's private IPv4 address |
| subnet | the subnet it serves |
| outcome | `ok`, `unreachable`, `login_rejected`, `locked`, `session_busy`, `page_not_understood`, `skipped_after_rejection` |
| online / offline | counts of router entries inside its subnet (0 unless `ok`) |

Validation: at most 8 sources per run; `address` private; `outcome` from the list. No credential
fields exist in the contract.

## Server (migration 0003)

### run_sources (fact, derived from the stored run)

`(collection_id, idx, type, model, address, subnet, outcome, online, offline)`, primary key
`(collection_id, idx)`. Used for the Collectors page's "router" column (FR-013): the outcome of
each collector's latest run.

### sightings (projection)

New column `via TEXT NOT NULL DEFAULT ''`, part of the sighting tuple (ip, mac, hostname, via):
a device moving from `LAN1` to `LAN2` starts a new sighting.

### Unchanged rules

- `router_table` observations identify devices by MAC, exactly like `arp`; weak devices fold into
  them (feature 001, research R6).
- A complete `router_table` scan counts as a completed scan of its subnet for the offline rule
  (feature 001, research R7).
- Router entries reported offline never become observations (FR-007); entries outside the
  router's subnet are dropped by the collector (FR-008).
