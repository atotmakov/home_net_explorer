# Contract change: collector API (v1, additive)

Extends `specs/001-lan-inventory-topology/contracts/collector-upload-api.yaml`, 002
`upload-api-changes.md` and 003 `api-changes.md`. `schema_version` stays `1`.

## `POST /api/v1/collections`

### Observation

| Field | Change |
|-------|--------|
| `method` | new value `ap_client`: a device listed by an access point as connected |
| `via` | now also allowed for `ap_client` (required there): `"<SSID> (<band>)"`, ≤ 32 characters, band `2.4 GHz` or `5 GHz`, or `"<SSID>"` when the band is unknown |
| `access_point` | **new**, required for `ap_client`, forbidden otherwise: the private IPv4 address of the access point |

`mac` is required for `ap_client`. The existing rule holds: `ip` must lie in a non-skipped subnet
of the same run.

### RunSource

| Field | Change |
|-------|--------|
| `type` | new value `access_point` |
| `outcome` | new value `controller_managed` (the access point is run by a central controller and has no client list) |
| `online` | for `access_point`: clients sent as `ap_client` observations |
| `offline` | for `access_point`: clients left out (no IP, invalid MAC, or outside the run's scanned subnets) |

`MaxSources` 8 → 16. An access point read adds no `subnets` entry.

```json
{ "observed_at": "2026-10-24T10:00:03Z", "ip": "192.168.0.13", "mac": "00:00:5e:00:53:13",
  "hostname": "Pixel-9a", "hostname_source": "router", "method": "ap_client",
  "via": "Home (5 GHz)", "access_point": "192.168.0.5" }
```

```json
{ "type": "access_point", "model": "tplink-eap225", "address": "192.168.0.5",
  "subnet": "192.168.0.0/24", "outcome": "ok", "online": 6, "offline": 1 }
```

## `GET /api/v1/ping`

New field `access_points`: `[{ "id", "model", "address", "subnet" }]`, the access points configured
in the web UI (no credentials), same shape as `routers`. Older collectors ignore it.

## `GET /api/v1/routers/{id}/login`

Unchanged; also answers for access point ids (same settings table).
