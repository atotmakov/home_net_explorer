# Contract change: web UI (open ports)

| Route | Change |
|-------|--------|
| `GET /devices/{id}` | New "Open ports" card: a table of open ports (port/proto, service or "unknown", address, last found open, collector), then "Previously open" (port/proto, service, last open, closed at), then "Last checked: <time> by <collector>". Texts instead of the table: "Ports not checked yet", "No open ports found", "Port checks are off for this device" / "… for its subnet", "Not reachable for port checks (only listed by a router)", "Answers on every port checked". For an offline device the open list is titled "Last known open ports". Manual devices (011) show no card. A form "Port checks: on/off" with Save. |
| `POST /devices/{id}/ports` | Form field `ports` = `on` \| `off`; stores the owner fact; 303 back. 404 unknown device; 400 bad value; cross-site 403. |
| `GET /settings` | New rows: "Port checks: all TCP ports and UDP services DNS, NTP, NetBIOS, SSDP, mDNS; once a day per device, new devices at once"; per subnet a "Port checks on/off" select beside Ignore. |
| `POST /subnets/{id}/ports` | Form field `ports` = `on` \| `off`; 303 to `/settings`. |
| `GET /` (home) | Beside "Scan now": "Check ports now" (POST `/scan/ports`). The scan status shows "Checking ports: n of m devices" while a port job runs. |
| `POST /scan/ports` | Starts the built-in collector's scan with a forced port job (research R8). 202 with the status partial; 409 if a port job is running; 503 if the built-in collector is disabled. |
| Device page, History list | Shows "Port 80/tcp opened" / "Port 80/tcp closed" events. |
