# Contract change: web UI (access points)

Extends 003 `web-ui-changes.md` (router card) and 010 `web-ui-changes.md` (Map).

| Route | Behavior |
|-------|----------|
| `GET /devices/{id}` | For a device of type `access_point`: an **Access point** card like the Router card: model (`tplink-eap225`), address (one of the device's current addresses), username, password (shown only as "set"), Save, Remove, and per collector the latest read (outcome, clients read / left out, when). For any device whose latest sighting came from an access point, the address row says "via <AP name> · <SSID> (<band>)". |
| `POST /devices/{id}/access-point` | Saves the settings (same validation as `POST /devices/{id}/router`: model in the AP list, address a current private address, password required on first save). 303 back to the device page. 400 if the device type is not `access_point`. |
| `POST /devices/{id}/access-point/remove` | Deletes the settings. 303 back. |
| `POST /devices/{id}/attrs` (type change) | Changing the type away from `access_point` deletes its access point settings, as for routers. |
| `GET /collectors` | The per-collector source list also shows access point reads ("access point tplink-eap225 at <address>: ok, N clients"). |
| `GET /ui/map.json` | Port nodes and edges may now hang under an access point: `port-<ap dev id>-<via>`, edge `rp-<ap dev id>-<via>` from the AP, `pd-<dev>` to the client. Shape unchanged. |

Cross-site posts are refused as on every form (feature 007).
