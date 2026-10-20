# Data Model: Device Status Freshness

No schema change. Facts (collection runs, user facts) are unchanged.

## Stored device status (projection)

`devices.status` keeps its CHECK set; in practice only `online`, `offline`, `merged_away` occur.

| Transition | When (applied in ingest order, also by rebuild) |
|------------|--------------------------------------------------|
| → online (create) | first observation of a new identity |
| offline → online | an observation newer than `last_seen` (`came_online` event), unchanged |
| online → offline | complete, not ignored scan R of subnet S: device has a current address in S, was not seen by R, `last_seen < R.started_at`, and not current in another subnet whose latest complete scan (finished ≤ R) has `started_at ≤ last_seen` (`went_offline` at `R.finished_at`) — **new rule, research R1** |

## Shown state (computed at view time, never stored)

| State | Condition | Badge |
|-------|-----------|-------|
| `online` | stored online and `now − last_seen ≤ 2 × I` | green "online" |
| `stale` | stored online and `now − last_seen > 2 × I` | "online" in the normal text color |
| `offline` | stored offline | gray "offline" |

Every badge has the tooltip "last seen <age> ago" (`DeviceRow.Age = now − last_seen`, set for all
states), the age written as "2 days, 3 hours, 5 mins", "3 hours, 5 mins", "5 mins" or "less than
a minute".

I = `interval_seconds` of the most recent complete scan (by `finished_at`, any collector) of any
subnet holding one of the device's current addresses; else the built-in interval setting
(default 900 s).

## Settings

| Key | Change |
|-----|--------|
| `offline_multiplier` | removed from the UI and defaults; ignored if present |
| `projection_rules` | new, internal: version of derivation rules; `6` after the one-time rebuild |

## Subnet staleness (home page)

`SubnetInfo.Stale` = no complete scan of the subnet by any collector within 2 × that collector's
interval of now (was: multiplier × interval).
