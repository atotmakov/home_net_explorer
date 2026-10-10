# Contract change: web UI

Extends `specs/001-lan-inventory-topology/contracts/web-ui.md`.

## Settings page (`GET /settings`): new "Maintenance" card

```text
Maintenance
  Built-in scanner: running            [Pause]
      (or) Built-in scanner: paused since 2026-10-10 10:15   [Resume]
      (omitted when the server runs with --no-builtin-scan)

  Remove all devices
    Deletes every device, all scan history from every collector and all your edits (names,
    types, notes, connections, merges, router settings). Collectors, settings and subnet names
    stay. Devices come back at the next scan.
    Type "devices" to confirm [          ]  [Remove all devices]

  Remove all collectors
    Deletes every remote collector; their tokens stop working at once. What they found stays.
    The NAS built-in collector stays.
    Type "collectors" to confirm [          ]  [Remove all collectors]

  Drop all data
    Deletes everything except your login: devices, history, edits, collectors, subnets and
    router settings; settings return to their defaults.
    Type "everything" to confirm [          ]  [Drop all data]
```

After a successful action, a notice at the top of the page, e.g.
`Removed 42 devices and 310 scan records.` / `Removed 3 collectors.` /
`Dropped all data: 42 devices, 310 scan records, 3 collectors.` /
`Built-in scanner paused.` / `Built-in scanner resumed.`

## Routes

All routes require the owner session and pass the cross-site checks of feature 001 (Origin /
`Sec-Fetch-Site`); without them nothing changes.

| Route | Form | Success | Errors |
|-------|------|---------|--------|
| `POST /maintenance/devices` | `confirm=devices` | 303 → `/settings?done=devices&devices=N&runs=N` | 400 "Type devices to confirm." (nothing removed) |
| `POST /maintenance/collectors` | `confirm=collectors` | 303 → `/settings?done=collectors&collectors=N` | 400 likewise |
| `POST /maintenance/everything` | `confirm=everything` | 303 → `/settings?done=everything&devices=N&runs=N&collectors=N` | 400 likewise |
| `POST /maintenance/pause` | none | 303 → `/settings?done=paused` | 404 when the built-in collector is disabled |
| `POST /maintenance/resume` | none | 303 → `/settings?done=resumed` | 404 likewise |

The confirmation word is compared after trimming spaces, case-insensitively. The notice is
rendered only from a known `done` value and integer counts; other query values are ignored.

## Home page (`GET /`) and `POST /scan`

- While paused, the built-in scan card shows "Paused" and a **Resume** button (form posting to
  `/maintenance/resume`) instead of **Scan now**.
- `POST /scan` while paused changes nothing and returns the scan-status fragment showing "Paused".

## Collectors page

- Removed collectors are not listed; the built-in collector's Status shows `paused` while paused.
- A name used by a removed collector can be used for a new one.

## Pages that name collectors in history

Subnet "Discovered by", the device page sightings and the subnet "By" column show a removed
collector as `<name> (removed)`.
