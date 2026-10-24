# Data Model: Access Point Client Lists

## Schema change (migration 0008)

```sql
-- Feature 012: the access point that reported a sighting's Wi-Fi connection (research R8).
ALTER TABLE sightings ADD COLUMN via_source TEXT NOT NULL DEFAULT '';
```

`via_source` is the AP's IPv4 address for `ap_client` sightings, `''` otherwise. It is part of the
sighting tuple `(ip, mac, hostname, via, via_source)`: a move from one AP to the other starts a new
sighting row (as a move from LAN1 to LAN2 does, 002). A rebuild from stored runs fills it from the
payloads.

```sql
-- run_sources gains type 'access_point' and outcome 'controller_managed'. SQLite cannot alter a
-- CHECK constraint, so the table is rebuilt (as in 0004).
CREATE TABLE run_sources_v8 (
    collection_id TEXT NOT NULL REFERENCES collection_runs (collection_id),
    idx           INTEGER NOT NULL,
    type          TEXT NOT NULL CHECK (type IN ('router', 'access_point')),
    model         TEXT NOT NULL,
    address       TEXT NOT NULL,
    subnet        TEXT NOT NULL,
    outcome       TEXT NOT NULL CHECK (outcome IN ('ok', 'unreachable', 'login_rejected', 'locked',
                      'session_busy', 'page_not_understood', 'skipped_after_rejection',
                      'login_unavailable', 'controller_managed')),
    online        INTEGER NOT NULL CHECK (online >= 0),
    offline       INTEGER NOT NULL CHECK (offline >= 0),
    PRIMARY KEY (collection_id, idx)
);
INSERT INTO run_sources_v8 SELECT * FROM run_sources;
DROP TABLE run_sources;
ALTER TABLE run_sources_v8 RENAME TO run_sources;
```

`run_sources` is rebuilt to allow the new type and outcome; no other table changes. Access point
settings are `router_settings` rows whose `model` is an access point model (research R4).

## Entities

| Entity | Where | Fields / rules |
|--------|-------|----------------|
| Access point settings | `router_settings` | `model = "tplink-eap225"`; `subnet`, `username`, `password` (plain text, feature 003); allowed only while the device type is `access_point`; changing the type away or "Remove" deletes the row |
| Access point ref (to collectors) | ping `access_points[]` | `{id, model, address, subnet}`; address picked as for routers (`PickRouterAddress`); no credentials |
| Access point read | `run_sources` (`type = "access_point"`) | `model`, `address`, `subnet`, `outcome`, `online` = clients sent, `offline` = clients left out |
| Wi-Fi connection | `sightings` (`via`, `via_source`) from `ap_client` observations | `via = "<SSID> (<band>)"` ≤ 32 chars; `via_source` = AP address |

## Collector-side types (`internal/collect/accesspoint`)

```text
Client  { MAC string (lower-case colon form); IP netip.Addr (zero if none); SSID; Band "2.4 GHz"|"5 GHz"|""; Hostname }
Result  { Outcome; Clients []Client; Path string (firmware endpoint used); Keys []string (first entry's keys, for check) }
Source  { Model(); Address(); Prefix(); Read(ctx) Result }
```

The engine turns `Clients` into observations (research R6, R7):

- valid MAC and an IP inside a non-skipped subnet of the run → `ap_client` observation
  (`hostname_source = router` when the AP gives a name), replacing a router observation of the
  same IP;
- otherwise counted in `offline` and dropped.

## Rules

- An `ap_client` observation is a sighting (the device counts as seen at `observed_at`); it never
  marks a subnet complete and never marks a device offline (spec FR-008).
- Map (010 view): a device's latest via sighting with `via_source` = A is drawn under the device
  whose current address is A, through port node `port-<A device id>-<via>`; if no device has that
  address, the device is drawn without a port link. Manual links still win (010 R7).
- `ListRouters` returns only router models; `ListAccessPoints` only AP models; both resolve the
  address the same way.
- Saving or removing access point settings clears the built-in collector's rejection marker, as
  for routers.
