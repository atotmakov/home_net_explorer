# Contract change: web UI (manual devices)

No change to the collector upload API (contract v1 unchanged).

| Route | Behavior |
|-------|----------|
| `GET /devices` | "Add device" link to `/devices/new`. Manual devices: "manual" badge in the status column, "—" for the address. The status filters online / stale / offline never list them; no filter does. |
| `GET /devices/new` | Form: Name (required), Type (select, optional), MAC (optional), Notes. |
| `POST /devices` | Creates the manual device. 303 to `/devices/{id}`. 400 re-renders the form with the message: "a name is required", "<value> is not a MAC address", "this MAC belongs to <device name> (<ip>)". |
| `GET /devices/{id}` | For a manual device: "Added by hand on <date>" instead of first/last seen; a MAC field with Save (`POST /devices/{id}/mac`); a Remove section ("Type `remove` to confirm"). Scanned devices have neither (FR-012). |
| `POST /devices/{id}/mac` | Sets or clears the MAC (same validation as add). 303 back; 400 with the message; 404 for a scanned device. |
| `POST /devices/{id}/remove` | Requires `confirm=remove`; otherwise 400 and nothing is removed. Removes the device and the links that point to it; 303 to `/devices`. 404 for a scanned device. |
| `POST /devices/{id}/link` | Unchanged, plus: 400 "this would make a loop: <target> connects via <this device>" when the target's chain reaches this device. Options list routers, switches, access points first. |
| `GET /` | The device total includes manual devices: "N devices (M added by hand)" when M > 0. |
| `GET /ui/map.json` | A manual device is a `device` node with `state: "manual"`, `last_seen_ago: ""`, `ips: []`, `parent` = the group of the device it connects via (following manual devices), else `other`. Shape unchanged. |
| `GET /map` | Manual nodes have a dashed outline; tooltip and details say "manual (added by hand)"; "Online only" keeps a manual node when an online or stale device connects via it, directly or through manual devices. |
| `POST /maintenance/devices`, `/maintenance/everything` | Also delete manual devices; the result says how many of the removed devices were added by hand. |

All posts keep the cross-site check (feature 007).
