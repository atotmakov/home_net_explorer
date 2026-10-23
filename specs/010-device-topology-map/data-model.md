# Data Model: Device Topology Map

No schema change and no migration. The map is a view built at request time (research R1) from
existing tables.

## Inputs (existing)

| Table / source | Used for |
|----------------|----------|
| `devices` via `ListDevices(DeviceFilter{}, now)` | device set, name, type, MAC, vendor, `State`, `Age` (R2) |
| `device_addresses` (current, visible subnets) | group, bridges, gateway matching (R3–R5) |
| `subnets`, `SubnetStatus(now)` | group label, `stale` (R3) |
| `sightings.via` (latest non-empty per device) | port / Wi-Fi connection (R6) |
| `run_sources` (latest `ok` per subnet), `ListRouters` | router address of a subnet (R4a) |
| latest `collection_runs.payload_gz` per collector → `vantage` | route next hops (R4b), collector interfaces (R5) |
| `user_links` (latest row per `from_identity_key`) | manual links (R7) |

## View: Topology

```text
Topology
  Groups []Group
  Nodes  []Node      // devices and ports
  Edges  []Edge

Group   { ID "subnet-<id>" | "other"; Label; CIDR; Stale bool }
Node    { ID "dev-<id>" | "port-<routerDevID>-<via>"; Kind device|port; Parent group ID;
          // device only:
          DeviceID; Label; IP; IPs []string; MAC; Vendor; Type; State online|stale|offline;
          LastSeenAgo (text, feature 006 "ago"); GatewayOf []group ID }
Edge    { ID; Kind gateway|bridge|port|manual; Source node ID; Target node or group ID;
          Label (port name for port edges); Current bool (port edges: false = last known) }
```

## Rules

- Every device of the unfiltered Devices list is exactly one device node (FR-001); its `Parent` is
  the group of its first current address, else `other`.
- `gateway` edge: device → group, for each subnet it is the gateway of (R4).
- `bridge` edge: device → group, for each other visible subnet it has a current address in (R5).
- `port`: node per (router device, via); edges router → port (`port`, current) and port → device
  (`port`, `Current` = device state ≠ offline) (R6). Omitted when the router is not a device in
  the inventory or the device has a manual link.
- `manual` edge: device → device from the latest non-removed `user_links` row of the device; both
  ends resolved through merges; dropped if either end is not on the map (R7).
- Groups with no nodes are omitted, except subnets in a collector's latest run (R3).

## Manual link writes

`POST /devices/{id}/link` appends a `user_links` row: `from_identity_key` = the device's identity
key, `to_identity_key` = the target device's identity key, `removed` = 0, `at` = now; with an
empty `via` it appends a row with `removed` = 1 (append-only, Principle V). A device cannot link
to itself (400).
