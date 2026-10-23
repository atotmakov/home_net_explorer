# Quickstart: Device Topology Map

Validation on the real network after deployment (`.\deploy.ps1 -Pull`). Tests run in CI only.

## Prerequisites

- Server 0.11 or later on ocean (`http://192.168.1.100:8181`).
- The desktop collector (0.10+, `hne-collector.yaml`) has scanned recently, so 192.168.0.x
  (through the Huawei router, with ports) and 192.168.8.x are in the inventory.

## 1. All devices, right groups (US1, SC-001)

1. Open `/devices` with no filter; note the total and the online / stale / offline counts on `/`.
2. Open **Map**. Expected: groups for 192.168.0.x, 192.168.1.x and 192.168.8.x; the device total
   and the per-status counts match step 1; offline devices are gray, stale ones in the normal
   text color, online ones green.
3. Hover a device: name, IP, MAC, "last seen … ago".

## 2. Connections (US2)

1. The Huawei router (192.168.0.1) is marked as gateway of 192.168.0.x, with LAN / Wi-Fi port
   nodes and the devices behind each.
2. An offline device behind the router (for example Pixel-10a) hangs off its last port with a
   dashed "last known" edge.
3. DUNE (192.168.0.28 and 192.168.1.43) appears once, with a two-subnet edge to the other group.

## 3. Online only (US3, SC-003)

Switch to **Online only**: offline devices and their edges disappear at once, stale ones stay.
Switch back: all return. Reload the page: it opens on **All devices**.

## 4. Details (US4)

Click a device: the side panel shows its details and a link that opens `/devices/{id}`.

## 5. Manual link (US5, SC-005)

1. On a phone's device page set **Connected via** = the access point (or any device); Save.
2. The map shows a manual edge; the phone's port edge is gone.
3. After the next scans (or `scan --once` on the desktop) the edge is still there.
4. Clear **Connected via**: the edge disappears.

## 6. Offline browser

Disconnect the viewing PC from the internet (LAN only) and reload **Map**: it still draws.
