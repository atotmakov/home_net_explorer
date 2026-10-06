# Quickstart & Validation: LAN Inventory & Topology Explorer

This guide shows how to prove the feature works end to end. Interfaces are described in
[contracts/](contracts/), and storage in [data-model.md](data-model.md).

## Prerequisites

| Where | Needs |
|-------|-------|
| Dev machine (Windows desktop) | Go ≥ 1.26, `make` or `go run ./tools/...` (neither Go nor Docker is installed yet) |
| Image build | Docker (Desktop or Buildx) on any machine, for the multi-arch image |
| NAS | Container runtime with compose that supports `network_mode: host` and `cap_add` |
| Network | Any private IPv4 subnets. Example used below: NAS on 192.168.1.0/24, desktop on 192.168.1.0/24 + 192.168.8.0/24. Nothing is configured with these values; they are discovered |

## 1. Automated checks (no network access needed)

```sh
go test ./...                      # unit tests: fakes for probes, resolver, clock
go test ./tests/contract/...       # upload API: fixtures vs collector-upload-api.yaml, both sides
go test ./tests/integration/...    # server + temp SQLite fed scripted runs over HTTP
```

**Expected**: all tests pass. The integration suite includes the following checks:
- **Rebuild invariant**: projections rebuilt from the stored runs match the ones built
  incrementally (data-model.md).
- **Local-only guard**: the server makes no connection outside the configured subnets (SC-008).
- **Duplicate upload**: uploading the same run twice creates no new events (FR-011).
- **Timing**: with 250 devices and one year of scripted history, `/devices`, `/timeline`, and
  `/ui/map.json` each respond in under 2 seconds (SC-007).

## 2. Run the server locally (smoke test)

```sh
go run ./cmd/hne-server --data ./tmp-data --listen :8080 --no-builtin-scan
```

Open `http://localhost:8080`. You should be sent to `/setup`. Set a password, log in, and
confirm that the empty inventory and the Collectors page both render.

## 3. Deploy to the NAS (User Story 1)

CI publishes `ghcr.io/atotmakov/home_net_explorer:latest` (multi-arch) on every merge to `main`.
Deploy it from the Windows desktop with the same flow as the cctv-ui project:

```powershell
Copy-Item .env.deploy.example .env.deploy   # once: set NAS_HOST, NAS_USER, SSH_KEY, IMAGE_PLATFORM, HNE_PORT
.\deploy.ps1
```

`deploy.ps1` pulls the image with crane, streams it to the NAS over SSH, runs `docker load`,
prepares `NAS_DIR/data` (owned by uid 65532), uploads `deploy/compose.yaml` as
`docker-compose.yml` (host networking, `NET_RAW`, `./data:/data`) and runs
`docker compose up -d`. It finishes with a `/healthz` check from the desktop.

1. Open `http://<nas-ip>:<HNE_PORT>`, run `/setup`, and log in.
2. Click **Scan now**. Nothing about subnets is configured beforehand.
3. In **Settings → Subnets**, confirm that the NAS's own subnet appeared automatically.

**Expected**: within 2 minutes the **Devices** list shows the devices on the NAS's subnet, with
IP, MAC, manufacturer, and hostname where available (SC-002). Check the list against the router's
DHCP client table: at least 95% of the active entries should be present (SC-003). The NAS's default
gateway should be marked as the gateway.

## 4. Add the desktop collector (User Story 2)

1. On **Collectors**, create the collector `desktop`. Download `hne-collector-windows-amd64.exe`
   and the generated `hne-collector.json` into one folder.
2. Run `hne-collector.exe check`. **Expected**: every private subnet the desktop is attached to is
   listed as on-link (any non-private or wider-than-/22 subnet is listed as skipped), and the ping
   result is OK.
3. Run `hne-collector.exe scan --once`. **Expected**: exit code 0, with a summary for both
   subnets.

**Expected in the UI**: within 1 minute, devices on the subnets only the desktop sees appear, and
those subnets show as newly discovered (SC-004, SC-010).
Devices on 192.168.1.0/24 that the NAS has also seen are **not** duplicated (SC-006). The
desktop's detail page shows both addresses.

**Negative checks**:
- Revoke the token and run `scan --once`. **Expected**: exit code 4. The UI shows no new data, and
  the run stays in `spool/`.
- Create a new token, stop the NAS container, and run `scan --once`. **Expected**: exit code 5
  (spooled). Start the container and run `scan --once` again. **Expected**: both runs are
  uploaded, and the history uses the original observation times.
- **New subnet**: attach the desktop to a network it has never been on (e.g., a phone hotspot or
  a USB Ethernet adapter on another router), then run `scan --once`. **Expected**: the subnet
  appears automatically with its devices, with no config change (SC-010). **Ignore** it in
  Settings, then run `scan --once` again. **Expected**: the run reports it as skipped, and no new
  devices appear for it.

## 5. Change tracking (User Story 3)

After a baseline has been running for at least 3 scan intervals:
1. Connect a device that has never joined the network. **Expected**: within one interval it
   appears as **new**, with a `new_device` event (SC-005).
2. Make a known device get a different IP (e.g., a DHCP reservation change). **Expected**: an
   `ip_changed` event showing the old and new values.
3. Power off a device. **Expected**: after 3 intervals it shows **offline**, with a `went_offline`
   event. Power it on again. **Expected**: a `came_online` event.
4. On the home page, answer "what joined in the last 7 days" (SC-009, under 30 seconds).

## 6. Topology map (User Story 4)

Open **Map**. **Expected**:
- Two subnet groups.
- Each device inside its subnet.
- Gateways highlighted.
- The desktop drawn as a bridge between the subnets.

Then set "connected via" for a device, pointing to an access point, and check that the edge
appears and is still there after the next scan. Toggle **online only** / **all** and confirm the
map changes.

## 7. Privacy check (SC-008)

While a scan runs, watch the NAS's outbound connections (e.g., the router's connection log or
`ss -tunp` on the NAS). **Expected**: no destination outside the home's private subnets, apart from
the LAN DNS resolver (a private address) and mDNS multicast (224.0.0.251). This holds even
if the NAS itself is configured with a public DNS server.
