# Contract change: web UI (Map)

Extends and replaces the Map rows of `specs/001-lan-inventory-topology/contracts/web-ui.md`.

| Route | Behavior |
|-------|----------|
| `GET /map` | Map page: the graph, an "All devices / Online only" switch (starts on All devices), a legend (status colors; gateway, two-subnet, port/Wi-Fi, last known, manual links) and a details panel. Loads `/static/vendor/cytoscape.min.js` and the ES module `/static/map.js` (which imports `/static/map-model.js`), local only. With no devices: "No devices yet — run a scan from the home page." |
| `GET /ui/map.json` | The topology as JSON (below). No query parameters (the 001 `scope` parameter is dropped; filtering is client-side). `Cache-Control: no-store` |
| `POST /devices/{id}/link` | Form field `via`: a device id sets "connected via", empty removes it. 303 to the `Referer` if same-origin, else `/devices/{id}`. 400 for an unknown target or a link to itself; 404 for an unknown device. Cross-site posts 403 |
| `GET /devices/{id}` | Adds a "Connected via" select (all other devices by name; routers and access points first) with Save, showing the current manual link |

## `/ui/map.json`

```json
{
  "groups": [
    { "id": "subnet-3", "label": "home (192.168.1.0/24)", "cidr": "192.168.1.0/24", "stale": false },
    { "id": "other", "label": "Other", "cidr": "", "stale": false }
  ],
  "nodes": [
    { "id": "dev-12", "kind": "device", "parent": "subnet-5", "device_id": 12,
      "label": "HG8145V5", "ip": "192.168.0.1", "ips": ["192.168.0.1"],
      "mac": "e0:…", "vendor": "Huawei", "type": "router",
      "state": "online", "last_seen_ago": "less than a minute", "gateway_of": ["subnet-5"] },
    { "id": "port-12-LAN1", "kind": "port", "parent": "subnet-5", "label": "LAN1" }
  ],
  "edges": [
    { "id": "e1", "kind": "gateway", "source": "dev-12", "target": "subnet-5", "current": true },
    { "id": "e2", "kind": "bridge",  "source": "dev-7",  "target": "subnet-3", "current": true },
    { "id": "e3", "kind": "port",    "source": "dev-12", "target": "port-12-LAN1", "label": "LAN1", "current": true },
    { "id": "e4", "kind": "port",    "source": "port-12-LAN1", "target": "dev-31", "label": "LAN1", "current": false },
    { "id": "e5", "kind": "manual",  "source": "dev-40", "target": "dev-22", "current": true }
  ]
}
```

- `state` ∈ `online`, `stale`, `offline` (feature 006); colors as on the Devices list.
- `current: false` only on port edges of offline devices ("last known").
- `bridge` is the spec's "two-subnet" link: a device with current addresses in more than one
  subnet, linked to each other subnet's group.
- `port` labels are what the router reports (Huawei: `LAN1`–`LAN4`, `SSID1`, `SSID2`, …).
- Every device of `GET /devices` (no filter) is exactly one `device` node.
