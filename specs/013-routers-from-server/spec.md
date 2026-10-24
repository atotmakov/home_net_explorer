# Feature Specification: Routers Only From the Server

**Feature Branch**: `013-routers-from-server`

**Created**: 2026-10-24

**Status**: Implemented

**Input**: Owner request: "do this change, router should work the same way as access point"
(after feature 012 delivered access points to collectors only through the server, while routers
could still be listed in `hne-collector.yaml`).

## Context

Feature 002 configured routers in the collector's config file, with their login. Feature 003
moved router setup to the web UI but kept the file: the generated config listed every UI router
without a login "for information", and an entry with its own login still won over the UI.
Feature 012 configures access points only in the web UI: collectors learn them from `ping`, fetch
each login right before the read and cache the list (without logins) for scans while the server
is down. The file's router entries add nothing to that: without the server there is no login,
and after the first contact the cache is used. This feature makes routers work the same way.

## Requirements

- **FR-001**: Routers MUST be set up only on their device page in the web UI. Collectors MUST get
  them from `ping` at each scan (cached in `hne-collector.routers` while the server is down) and
  fetch each login from the server right before the read, as for access points (012 FR-003).
- **FR-002**: The config shown and downloaded when a collector is created MUST NOT contain a
  `routers` setting (replaces 003 FR-007).
- **FR-003**: A `routers` section in an existing `hne-collector.yaml` (or `hne-collector.json`)
  MUST still load, whatever its entries, and MUST NOT be used: no router is read from it, and a
  login in it is never sent anywhere (replaces 002 FR-001 to FR-003 configuration and 003 FR-013,
  "a config entry with credentials wins"). The collector MUST warn, at each start, that the
  section is no longer used, and, when an entry has a login, that the login must be entered on
  the router's device page instead.
- **FR-004**: The collector MUST NOT keep any value of that section in memory beyond the number
  of entries and whether one had a login, so no output, log, file or upload can contain it
  (002 FR-006).
- **FR-005**: The rejection rule is unchanged: after a rejected or locked login the router is not
  tried again until its login changes in the web UI or the owner runs `check` (002 FR-011, 003
  FR-014).

## Compatibility

The owner's `hne-collector.yaml` lists the Huawei router without a login; it keeps working, with
the warning, and the `routers` lines can be deleted. A collector older than 0.13 also reads UI
routers from `ping` (feature 003), so the server change alone breaks nothing.

## Tests

- `cmd/hne-collector/router_test.go` `TestConfigRoutersIgnored`: entries with a login, without,
  invalid, or more than 8 load; none is read; the warning and the login hint are shown; the
  password appears nowhere; the same router set up in the web UI is read with the server's login.
- `cmd/hne-collector/rejection_test.go`, `extra_test.go`, `secret_test.go`,
  `server_routers_test.go`: router scenarios run with UI routers; a file login never leaks.
- `cmd/hne-collector/main_test.go` `TestYAMLConfig`: the section loads; only its size is kept.
- `tests/integration/us2_router_config_test.go`: the generated config has no `routers`; `ping`
  lists the UI router; `us2_collector_test.go`: every remaining setting has its `#` comment.
