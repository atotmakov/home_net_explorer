# Contract: Web UI and owner endpoints

The server renders HTML pages, and htmx loads parts of pages from routes under `/ui/...`. Every
route except `/login`, `/setup`, `/static/*`, and `/api/v1/*` requires an owner session (FR-030).
A request without a session is redirected to `/login`, or to `/setup` if no password has been set
yet.

## Pages

| Route | Purpose | Spec |
|-------|---------|------|
| `GET /setup`, `POST /setup` | First-run screen to set the owner password. Disabled once a password exists | FR-030 |
| `GET /login`, `POST /login`, `POST /logout` | Owner session. Login is rate-limited to 5 attempts per minute | FR-030 |
| `GET /` | Home: counts (online/offline/new), "joined in last 7 days", newly discovered subnets, stale subnets, collectors with clock skew | SC-009, SC-010 |
| `GET /devices` | Inventory table. Query params: `q`, `subnet`, `status`, `manufacturer`, `sort`, `dir`, `seen_after`, `seen_before` | FR-017, US1 |
| `GET /devices/{id}` | Device detail: addresses, user fields, full history (sightings + events), the collectors that saw it | FR-022, US3-4 |
| `POST /devices/{id}/attrs` | Set name, notes, or type. Saving a type other than `router` removes the device's router settings (feature 003) | FR-014 |
| `POST /devices/{id}/router`, `POST /devices/{id}/router/remove` | Router card of a device of type `router`: model, username, password (never shown again; empty keeps it), subnet. See `specs/003-router-ui-setup/contracts/web-ui-changes.md` | 003 FR-001 – FR-006 |
| `POST /devices/{id}/ack` | Acknowledge a new device | FR-023 |
| `POST /devices/{id}/merge` (`into={id}`) / `POST /devices/{id}/split` | Manual identity merge and split | FR-015 |
| `POST /devices/{id}/link` (`via={id}`) / `DELETE /devices/{id}/link` | Manual "connected via" link | FR-026 |
| `GET /timeline` | Event timeline. Query params: `from`, `to`, `type` (repeatable), `device`, `subnet` | FR-021, US3-5 |
| `GET /map` | Topology page | FR-025, US4 |
| `GET /ui/map.json?scope=online\|all` | Graph data for the map: compound subnet nodes, device nodes, and edges labeled by link kind and source | FR-025, US4-4 |
| `GET /collectors` | Collector list with subnets, last report, skew, and status. Shows a create form and download links | FR-012 |
| `POST /collectors` | Create a remote collector. Responds **once** with the token and a downloadable `hne-collector.json`, which also lists the routers set up in the UI without credentials (feature 003) | FR-008 |
| `POST /collectors/{id}/revoke` | Revoke a token | FR-008 |
| `POST /scan` | Start an on-demand scan by the built-in `nas` collector. Returns 202, and the page polls for status | FR-004, US1-1 |
| `GET /settings`, `POST /settings` | Subnets: the list discovered automatically (with "new" notices), rename, ignore/unignore, and skipped too-large subnets. Also the built-in scan interval and offline multiplier | FR-005, FR-006, FR-018 |
| `GET /export` | Downloads `hne-export-<date>.json.gz`: devices, user facts, events, sightings, and raw runs | FR-032 |
| `GET /downloads/hne-collector-{os}-{arch}[.exe]` | The collector binary built with this server | US2 |
| `GET /healthz` | No auth. Returns `200 ok` once the database is open (used by the container healthcheck) | Deployment |

## Map JSON shape (`/ui/map.json`)

```json
{
  "nodes": [
    { "id": "subnet-1", "kind": "subnet", "label": "192.168.1.0/24", "stale": false },
    { "id": "dev-42", "kind": "device", "parent": "subnet-1", "label": "nas", "status": "online",
      "type": "nas", "is_gateway": false }
  ],
  "edges": [
    { "id": "l-7", "source": "dev-3", "target": "subnet-2", "kind": "bridge", "origin": "inferred" },
    { "id": "l-9", "source": "dev-42", "target": "dev-5", "kind": "manual", "origin": "user" }
  ]
}
```

A device with addresses on several subnets appears once. It is placed in its first subnet, and
`bridge` edges connect it to the others.
