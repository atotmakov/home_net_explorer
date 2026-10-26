# Implementation Plan: Open Ports per Device

**Branch**: `015-device-open-ports` | **Date**: 2026-10-26 | **Spec**: [spec.md](spec.md)

## Summary

Collectors check every TCP port (1–65,535) and a few well-known UDP services of the devices they
reach directly, and the device page lists the open ones. The server decides who is due: once a day
per device at a time slot derived from its identity, and at once for a device never checked. It
hands collectors the due addresses in `ping`, so collectors stay stateless. A port check runs in
the background after the presence scan and is uploaded as its own *port run* (a v1 upload with
`port_checks` and no subnets), so the 15-minute presence scan is never delayed. `hne-collector scan
--once --ports` and a "Check ports now" button for the NAS's built-in collector force a check of
every device found. The server stores each completed check and its open ports, derives the
device's current ports, and records "port opened/closed" events. The owner can switch port checks
off per subnet and per device.

## Technical Context

**Language/Version**: Go ≥ 1.26 (server and collector).
**Primary Dependencies**: none new. TCP connect checks with `net.Dialer` (no raw sockets: works on
Windows without Npcap and in the NAS container); UDP requests built with
`golang.org/x/net/dns/dnsmessage` (already used) or by hand.
**Storage**: SQLite; migration 0010 adds `port_checks`, `port_findings`, `device_ports`, two event
types and two owner-fact fields (research R6).
**Testing**: CI only. A fake dialer and fake UDP exchange for the checker; contract tests on both
sides; inventory, store, web and integration tests; a `hwtest`-tagged loopback test of the real
dialer; real-network confirmation in quickstart (gate G1).
**Target Platform**: the NAS's built-in collector (192.168.1.0/24) and the Windows desktop
collector (192.168.1.0/24, 192.168.8.0/24). Subnets known only from a router's list are not
checked (no direct reach).
**Project Type**: existing Go monorepo (server + collector).
**Performance Goals**: a device whose closed ports answer with a reset is checked in under a
minute; a firewalled device (silent ports) in about 4 minutes; a forced check of ~30 devices within
30 minutes (SC-004).
**Constraints**: non-intrusive (001 FR-007): a connection attempt, closed at once; UDP only
standard queries without credentials (no SNMP community guess). Per device at most 128 connection
attempts in flight; at most 4 devices at once. No CIDR literals outside tests (lint).
**Scale/Scope**: ~30 devices, a handful of open ports each.
**Version**: `0.20` (server and collector).

## Constitution Check

| Principle | Result |
|-----------|--------|
| I. Local-only | ✅ Checks go only to LAN devices the collector already scans; the service names are a table in the binary; nothing leaves the home network |
| II. Test-first | ✅ Red tests first for each step; the checker's dialer and UDP exchange are interfaces with fakes; time from the injected clock; no real network in unit tests |
| III. Simplicity | ✅ No new dependency or component; reuses the upload, ping, owner-fact and event mechanisms. Three tables are the minimum for faithful history (checks, findings) plus the derived view (device ports) |
| IV. Distributed collection | ✅ Collectors stay stateless: the server picks due addresses (ping). Additive, optional v1 fields; older collectors send no port checks; server deployed first. **Technique confirmation**: TCP connect from the Windows desktop and from the NAS container (host network) is confirmed by gate G1 before the UI work depends on it |
| V. Faithful history | ✅ Every completed check and its open ports are stored from the raw run; the device ports and open/closed events are derived and rebuilt from them; "closed" only from a complete check |

Amends 001 FR-007 / "port scanning out of scope" (owner decision, spec Context); the
non-intrusive limit stays.

**Result**: PASS, with gate G1 in tasks.md (re-checked after design: unchanged).

## Project Structure

### Documentation (this feature)

```text
specs/015-device-open-ports/
├── spec.md
├── plan.md                      # this file
├── research.md                  # R1–R10
├── data-model.md                # migration 0010, entities, derivation rules
├── quickstart.md                # real-network validation
├── contracts/
│   ├── upload-api-changes.md    # port_checks in a run; port_scan in ping
│   ├── collector-cli-changes.md # scan --once --ports
│   └── web-ui-changes.md        # device page, settings, Check ports now
└── checklists/requirements.md
```

### Source Code (repository root)

```text
internal/contract/v1.go, validate.go   PortCheck, OpenPort, CollectionRun.PortChecks,
                                       PingResponse.PortScan; limits; validation
internal/collect/ports/                new: checker.go (TCP connect sweep, UDP probes, limits),
                                       services.go (port → service table), banner.go (passive
                                       SSH/FTP/SMTP greeting), *_test.go, loopback_hw_test.go
internal/collect/portjob.go            background port job: due ∪ forced addresses from a run's
                                       directly scanned observations → a port run
cmd/hne-collector/main.go              --ports flag; port job after each run in the loop;
                                       port_scan from ping
internal/app/scanner.go                built-in collector: port job after each scan; ForcePorts
internal/store/migrations/0010_ports.sql
internal/store/ports.go                due addresses, settings, device ports for the page
internal/inventory/ports.go            apply port checks: store checks/findings, derive device
                                       ports, open/closed events; rebuild replays port runs
internal/web/                          device page section and toggle, settings rows, button
internal/web/templates/                device.html, settings.html, home.html
specs/001-lan-inventory-topology/      FR-007 and out-of-scope note point to 015
```

**Structure Decision**: one new collector package (`internal/collect/ports`) beside `router` and
`accesspoint`; server code follows the existing store / inventory / web split.

## Complexity Tracking

None: no constitution violation to justify.
