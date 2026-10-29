---

description: "Task list for Open Ports per Device"
---

# Tasks: Open Ports per Device

**Input**: `specs/015-device-open-ports/` (spec.md, plan.md, research.md, data-model.md,
contracts/upload-api-changes.md, contracts/collector-cli-changes.md, contracts/web-ui-changes.md,
quickstart.md)

**Tests**: REQUIRED (constitution Principle II): tests first, pushed and red in CI, then the
implementation. CI only (no local Go). No real network in unit tests: the checker's dialer and UDP
exchange are interfaces with fakes; time from `clock.Fake`. Fixtures use documentation MACs
(`00:00:5e:00:53:xx`) and the 192.168.1.x example addresses of existing tests. No IPv4 CIDR
literals in non-test `.go` files (lint).

**Gate G1** (plan, Principle IV): once Phase 3 is released, the owner's desktop runs
`hne-collector-windows-amd64.exe scan --once --ports --dry-run` and the NAS runs a forced check
(**Check ports now**); the open ports of the NAS and an Axis camera are compared with the known
services before Phases 4–5 start.

**PRs**: PR 1 = Phases 1–2; PR 2 = Phase 3 (US1, MVP); PR 3 = Phases 4–5 (US2, US3); PR 4 =
Phase 6 (T029 removal, docs, validation). Each PR: red tests commit, then implementation; deploy after merge.

## Phase 1: Setup

- [X] T001 Set `VERSION` to `0.20`
- [X] T002 [P] Amend `specs/001-lan-inventory-topology/spec.md`: FR-007 gets "(port checks: feature
  015, same limit)" and the "Port scanning and service/OS fingerprinting are out of scope" line
  gets "Port checks were added by feature 015; fingerprinting stays out of scope"

## Phase 2: Foundational (contract, storage, checker)

**Purpose**: the additive v1 contract both sides use, migration 0010, and the collector's checker.

- [X] T003 [P] Tests in `internal/contract/validate_test.go` `TestPortChecks`: a run with
  `subnets: []`, `observations: []` and one port check (ip 192.168.1.79, mac, complete, open
  80/tcp http table, 22/tcp ssh banner, 5353/udp mdns table) is valid; invalid: 257 checks; 513
  open entries; public or malformed `ip`; group MAC; `started_at` > `finished_at` or outside the run
  window; `port` 0 or 65536; `proto` other than `tcp`|`udp`; duplicate (port, proto); `service`
  longer than 32 or with control characters; `service_source` other than `table`|`banner`;
  `all_open` without `complete`. `TestPingPortScan`: `PingResponse` round-trips `port_scan`
  {enabled, due, excluded}; a response without it decodes with a nil `PortScan`
- [X] T004a [P] Contract tests in `tests/contract/`: add `fixtures/valid_port_run.json` (the example
  of contracts/upload-api-changes.md: `subnets: []`, `observations: []`, one complete check of
  192.168.1.79 with 80/tcp, 22/tcp banner, 5353/udp) and `fixtures/invalid_port_run.json` (port 0,
  duplicate 80/tcp); `collector_payload_test.go` (written with T011 in Phase 3, where the port job exists): a port run built by the collector's port job
  from fakes matches the valid fixture's shape and passes validation; `server_upload_test.go`: the
  valid fixture → 201 `stored`, the invalid one → 400 `validation_failed`, a run with presence data
  and port checks together → 201; `schema_test.go`: `GET /api/v1/ping` carries `port_scan` with
  `enabled`, `due` and `excluded` arrays
- [X] T004 Implement in `internal/contract/v1.go` and `validate.go`: `PortCheck{IP, MAC, StartedAt,
  FinishedAt, Complete, AllOpen, Open []OpenPort}`, `OpenPort{Port int, Proto, Service,
  ServiceSource string}`, `CollectionRun.PortChecks []PortCheck \`json:"port_checks,omitempty"\``,
  `PingResponse.PortScan *PortScan \`json:"port_scan,omitempty"\`` with `PortScan{Enabled bool, Due,
  Excluded []string}`; constants `MaxPortChecks = 256`, `MaxOpenPorts = 512`, `MaxServiceLen = 32`,
  `ProtoTCP`, `ProtoUDP`, `ServiceSourceTable`, `ServiceSourceBanner`; validation exactly as
  contracts/upload-api-changes.md. Update `specs/001-lan-inventory-topology/contracts/
  collector-upload-api.yaml` with the two optional fields. Make T003 and T004a pass
- [X] T005 [P] Test in `internal/store/migrate_test.go` (or the existing migration test)
  `TestMigration0010`: after migrating a database holding events, device attrs and subnet attrs
  from 0009, those rows are unchanged; `events` accepts `port_opened`/`port_closed`;
  `user_device_attrs` and `user_subnet_attrs` accept field `ports`; `port_checks`, `port_findings`,
  `device_ports` exist with the columns of data-model.md
- [X] T006 Add `internal/store/migrations/0010_ports.sql`: the three tables of data-model.md
  (`port_findings` PK (check_id, port, proto); `device_ports` PK (device_id, port, proto);
  `port_checks` index (device_id, finished_at)) and the rebuilds of `events`, `user_device_attrs`,
  `user_subnet_attrs` with the extended CHECKs, copying all rows. Make T005 pass
- [X] T007 [P] Tests in `internal/collect/ports/checker_test.go` with a fake dialer (open set,
  refused set, everything else times out) and a fake clock: `TestCheckTCP` — open {22, 80, 554}
  are reported, refused and silent ports are not, the check is `complete`; at most 128 dials in
  flight per device and 4 devices at once (the fake records the peak); `TestSilentDeviceShortTimeout`
  — when the first 1,024 ports all time out, later dials use 300 ms (research R2);
  `TestAllOpen` — 600 open ports → 512 listed, `all_open`, `complete`; `TestCancelled` — a context
  cancelled mid-sweep gives `complete: false` and keeps the ports found so far;
  `TestUDPProbes` — a fake UDP exchange answering on 53 and 1900 only gives 53/udp dns and
  1900/udp ssdp, and the requests sent are a DNS root-NS query, an NTP client request, a NetBIOS
  node status, a unicast SSDP M-SEARCH and an mDNS query; no request to 161; `TestBanner` — an open
  port whose fake connection sends `SSH-2.0-…` within 500 ms is named `ssh` with source `banner`,
  one that sends nothing keeps the table name; the banner text is not in the result
- [X] T008 [P] Test in `internal/collect/ports/services_test.go`: 22→ssh, 80→http, 443→https,
  445→smb, 554→rtsp, 631→ipp, 5000→synology-dsm, 8009→googlecast, 9100→jetdirect,
  udp 5353→mdns; an unknown port → ""
- [X] T009 Implement `internal/collect/ports/` (`checker.go`: `Checker{Dial, UDP, Clock,
  PerDevice=128, Devices=4, Timeout=1s, SilentTimeout=300ms}`, `Check(ctx, []Target) []contract.
  PortCheck`; `udp.go`: the five requests of research R3; `banner.go`; `services.go`: the table of
  research R4) with a `net.Dialer`-based default. Make T007, T008 pass
- [X] T010 [P] `internal/collect/ports/loopback_hw_test.go` (build tag `hwtest`): listen on two
  loopback ports, check 127.0.0.1 with the real dialer, both found open with a closed neighbour not

**Checkpoint**: contract, schema and checker are in; nothing is wired yet.

## Phase 3: User Story 1 - See a device's open ports (P1) 🎯 MVP

**Goal**: collectors check due (or, forced, all) devices and the device page lists open ports.

**Independent Test**: spec US1: a device with 22 and 80 open shows "22/tcp ssh" and "80/tcp http"
after a forced check; a never-checked device says "Ports not checked yet".

### Tests (red first)

- [X] T011 [P] [US1] Test in `internal/collect/portjob_test.go` `TestPortJobTargets`: from a run with
  `arp`, `icmp_tcp`, `router_table` and `ap_client` observations, a due list and an excluded list,
  the job targets the due addresses found by `arp`/`icmp_tcp` only; forced → every such address
  minus excluded; the port run has a new collection id, `subnets: []`, `observations: []`, the
  checks with each target's MAC, and `finished_at` before `started_at` + `MaxRunWindow`; a job
  near the window stops and reports unfinished targets `complete: false`; the job uses the plan
  fetched after the presence upload: an address absent from the first ping but due in the second
  is checked
- [X] T012 [P] [US1] Test in `cmd/hne-collector/main_test.go` `TestScanOncePorts`: with a fake
  server whose ping returns `port_scan.due = [.20]`, `scan --once` uploads the presence run then a
  port run checking .20 only; `scan --once --ports` checks every ARP-found address except
  `excluded`; `--dry-run --ports` prints both runs and uploads nothing; `check` prints
  `Port checks: on (1 due)`; `run` starts at most one background port job at a time
- [X] T013 [P] [US1] Test in `internal/inventory/ports_test.go` `TestApplyPortRun`: after a presence
  run with device D at 192.168.1.79, a port run with a complete check (80/tcp, 554/tcp) stores one
  `port_checks` row (device D, complete) and two `port_findings`; `device_ports` has both `open`
  with `first_open = last_open = finished_at`; a check matching no device is dropped; the port run
  creates no subnet scan, no offline change and no `went_offline` event; rebuild reproduces the
  same rows (research R10)
- [X] T014 [P] [US1] Test in `internal/store/ports_test.go` `TestPortPlan`: with clock.Fake, a
  never-checked online device is due; a device checked complete 21 h ago is due only once the
  minute of day is past its slot (`slot = FNV-1a(identity key) mod 1440`); one checked 19 h ago is
  not due; `due` lists only addresses on subnets the collector scanned in its latest run;
  `enabled` is false only when every scanned subnet is off;
  `TestDevicePortsView`: the page view gives open ports sorted by port, last check time and
  collector name, and "not checked" for a device without checks

### Implementation

- [X] T015 [US1] Implement `internal/collect/portjob.go` (targets from a run, the port run, the
  window stop) and wire `cmd/hne-collector/main.go`: `--ports` flag, `port_scan` from a ping made after
  the presence upload (cached like routers for `check`), foreground job in `scan`, one background job in `run`, `check` line per
  contracts/collector-cli-changes.md. Make T011, T012 pass
- [X] T016 [US1] Implement `internal/inventory/ports.go`: in `Apply`, a run without subnets skips
  the presence steps; each port check → device by MAC else by address on its subnet at
  `started_at`; insert `port_checks`/`port_findings`; upsert `device_ports` open rows; clear the
  three tables in `Rebuild` before replay; `ProjectionRules = 9` in `internal/app/app.go` (update
  `internal/app/rebuild_test.go`). Make T013 pass
- [X] T017 [US1] Implement `internal/store/ports.go`: `PortPlan(ctx, collectorID, now)`
  (due/excluded/enabled, research R5) and `DevicePorts(ctx, deviceID)`; return `port_scan` in
  `handlePing` (`internal/web/api.go`). Make T014 pass
- [X] T018 [P] [US1] Test in `internal/app/scanner_test.go` `TestBuiltinPortJob`: after a built-in
  scan, a port job runs for the store's due addresses through a fake checker and its port run is
  ingested; a device first found by this scan is port-checked in the same cycle; `TriggerPorts`
  makes the next scan force every found address; a second `TriggerPorts` while a job runs returns
  busy; the status reports "n of m"; with a fake job that blocks until released, the next scheduled
  scan still starts at its interval and no second job starts
- [X] T019 [US1] Implement in `internal/app/scanner.go`: the port job after each scan, in its own
  goroutine (one at a time), never blocking `Run`'s loop, with the plan computed after the scan's
  run is ingested (fake checker injectable), `TriggerPorts`, progress in `Status()`. Make T018 pass
- [X] T020 [P] [US1] Tests in `internal/web/ports_test.go`: `TestDevicePagePorts` — the device page
  shows the "Open ports" card with "22/tcp", "ssh", the address and the last check; "Ports not
  checked yet" without checks; "No open ports found" after a complete empty check; "Not reachable
  for port checks" for a device only listed by a router; no card for a manual device;
  `TestCheckPortsNow` — POST `/scan/ports` 202 and the status partial, 409 when busy, 503 without
  the built-in collector, 403 cross-site; the home page has the "Check ports now" button
- [X] T021 [US1] Implement the device page card (`internal/web/templates/device.html`,
  `internal/web/pages.go`), the home button and `POST /scan/ports` (`internal/web/server.go`,
  `home.html`). Make T020 pass. Release; **gate G1** (quickstart §1)
- [X] T022 [US1] Integration test in `tests/integration/us_ports_test.go` (written red with T020):
  upload a presence run, a ping returns .79 due, upload a port run for it, GET the device page:
  80/tcp and 554/tcp listed

**Checkpoint**: US1 works end to end (MVP).

## Phase 4: User Story 2 - See when ports change (P2)

**Goal**: open/closed history and "Previously open".

**Independent Test**: spec US2: 8765/tcp opened, then closed after the next complete check, both in
the history.

- [ ] T023 [P] [US2] Tests in `internal/inventory/ports_test.go`: `TestPortOpenedClosed` — first
  finding → event `port_opened` "80/tcp"; a later complete check without it → `closed`,
  `closed_at`, event `port_closed`; open again → `open`, new `port_opened`, `first_open` kept;
  `TestIncompleteCheckClosesNothing`; `TestTwoCollectors` — closed only when every collector that
  last saw it open has a newer complete check without it; `TestAllOpenCheck` — an `all_open` check
  closes nothing it did not list beyond the 512
- [ ] T024 [US2] Implement the closing and the events in `internal/inventory/ports.go` (data-model
  Rules). Make T023 pass
- [ ] T025 [P] [US2] Tests in `internal/web/ports_test.go`: "Previously open" lists closed ports with
  last open and closed at; an offline device's list is titled "Last known open ports"; the history
  shows "Port 80/tcp opened"/"closed"
- [ ] T026 [US2] Implement the template parts and the event labels (`device.html`, the event text
  map in `internal/web/pages.go`). Make T025 pass

## Phase 5: User Story 3 - Control the port scan (P3)

**Goal**: on/off per device and per subnet; settings show the port list and schedule.

**Independent Test**: spec US3: a device switched off is never checked, even forced, and its page
says so.

- [ ] T027 [P] [US3] Tests: `internal/store/ports_test.go` `TestPortsOff` — a device or subnet with
  the latest `ports` fact `off` is never due and is in `excluded`; on again → due by the normal rule;
  `internal/web/ports_test.go` `TestPortsToggle` — POST `/devices/{id}/ports` and
  `/subnets/{id}/ports` with `on`|`off` store the fact (303), 400 bad value, 404 unknown, 403
  cross-site; the device page says "Port checks are off for this device" / "… for its subnet";
  Settings lists the ports checked and the schedule text of contracts/web-ui-changes.md and a
  per-subnet select; `internal/inventory` — the facts survive a rebuild
- [ ] T028 [US3] Implement the facts (`internal/inventory/facts.go`), the plan exclusion
  (`internal/store/ports.go`), the handlers and templates (`device.html`, `settings.html`). Make
  T027 pass

## Phase 6: Polish & Cross-Cutting

- [ ] T029 Removal (feature 004): extend `internal/store/maintenance.go` so removing devices
  or all data deletes `port_checks`, `port_findings`, `device_ports`; test in
  `internal/store/maintenance_test.go`
- [ ] T030 [P] Update `specs/001-lan-inventory-topology/contracts/collector-cli.md` with the
  `--ports` flag and the `check` line
- [ ] T031 Real-network validation per quickstart.md §2–§5 on the owner's network; record results
  in this file (like 011 T021), including the forced-check duration (SC-004); a week later, record
  whether any device misbehaved (SC-005)
- [ ] T032 Mark spec `**Status**: Implemented (0.20)` and all tasks [X]

## Dependencies

- Phase 2 blocks everything. Within it: T003, T004a→T004, T005→T006, T007/T008→T009.
- US1 (Phase 3) needs Phase 2. T015 needs T004, T009; T016 needs T004, T006; T017 needs T006;
  T019 needs T009, T016, T017; T021 needs T017, T019.
- US2 needs T016 (device_ports). US3 needs T017 (plan) and T021 (page). US2 and US3 are
  independent of each other.

## Parallel Examples

- Phase 2: T003, T004a, T005, T007, T008 together (different files).
- US1 tests: T011, T012, T013, T014 together; then T015, T016, T017 in parallel (different
  packages); T018 and T020 after.
- US2 and US3 phases can run side by side after US1.

## Implementation Strategy

MVP = Phases 1–3: ports are checked (daily and forced) and shown on the device page. Then US2
(history), US3 (control), polish. Gate G1 is passed before Phases 4–5 start, so history and control are
built on confirmed real results.
