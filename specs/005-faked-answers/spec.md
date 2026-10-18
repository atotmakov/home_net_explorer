# Feature Specification: Faked Answers Defence

**Feature Branch**: `005-faked-answers`

**Created**: 2026-10-13

**Status**: Implemented (real-network validation pending: tasks.md T012)

**Input**: User description: "Implement this defence on collector side. In this situation collector should send some signal to backend, that scan complete with error. This should be visible in ui."

## Context

On the owner's desktop, a VPN app (Happ) routes all traffic, including `192.168.0.0/24`, through
its tunnel, and the tunnel answers ICMP echo for **every** address instantly (even
`10.255.255.77`). The desktop collector probes `192.168.0.0/24` with ICMP/TCP because it is not
on-link, so all 254 addresses "answered" and became devices without MACs (found 2026-10-13 with a
dry run: 254/254 ICMP answers, route via `happ-tun`).

The collector must notice that the network path fakes answers, record nothing for that subnet,
and tell the server, which shows the failed scan in the UI.

## Revision 2026-10-17: canary withdrawn

The canary probe of `192.0.2.1` (FR-001, US1 scenarios 1 and 3, US3 scenario 2) gave a false
positive on the owner's network: the `192.168.8.1` router proxies public destinations and accepts
any TCP connect to them (including `192.0.2.1`), while private subnets such as `192.168.0.0/24`
are routed directly and answer truthfully. A public address cannot speak for private subnets, so
the canary is removed from the collector (scans and `check`). The saturation rule (FR-002) alone
decides; it is measured on the subnet itself and caught the original Happ case (254/254).
Struck items below are kept for history.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Fake answers never become devices (Priority: P1)

As the owner, when a VPN or proxy on a collector's machine answers for every address, I want the
collector to refuse those answers, so my inventory doesn't fill with hundreds of ghost devices.

**Independent Test**: a collector whose path answers every probe (including an address that
cannot exist) scans a routed subnet: the run has no observations for it and reports the subnet as
failed with reason "faked answers".

**Acceptance Scenarios**:

1. ~~**Given** a routed subnet reached through the default route, **When** an address reserved
   for documentation (`192.0.2.1`, never used on any network) answers the presence probe,
   **Then** the subnet is not probed and is reported as skipped with reason `faked_answers`.~~ (withdrawn)
2. **Given** a routed subnet, **When** 90% or more of at least 16 probed addresses answer,
   **Then** none of them is recorded and the subnet is reported as skipped with reason
   `faked_answers`.
3. ~~**Given** a subnet with its own route (not the default route), **When** the documentation
   address answers through the default route, **Then** that subnet is still probed (the check
   says nothing about its path); the 90% rule still applies.~~ (withdrawn)
4. **Given** an on-link subnet (ARP), **Then** neither check applies (ARP answers carry MACs from
   the link itself).
5. **Given** a normal path, **Then** routed scans work as before.

### User Story 2 - The owner sees the failed scan (Priority: P1)

As the owner, I want to see on the server that a collector's scan of a subnet failed and why, so
I can fix the collector's machine (e.g. exclude local traffic from the VPN).

**Independent Test**: upload a run with a `faked_answers` subnet: the Collectors page shows that
collector with a "scan error" status and the subnet with the reason; the home page shows a "Scan
problems" notice; devices of that subnet are not marked offline.

**Acceptance Scenarios**:

1. **Given** a collector's latest run has a subnet skipped for `faked_answers`, **Then** the
   Collectors page shows a warning status for that collector and lists the subnet with
   "answers faked on the network path (VPN?)".
2. **Given** the same, **Then** the home page shows a "Scan problems" notice naming the collector
   and the subnet.
3. **Given** a later run of that collector scans the subnet normally, **Then** the warning
   disappears.
4. **Given** a subnet skipped for `faked_answers`, **Then** devices on it are not marked offline
   because of that run (a skipped subnet is never a completed scan).
5. **Given** a removed collector (feature 004), **Then** its old problems are not shown.

### User Story 3 - The collector says why (Priority: P2)

As the owner running the collector by hand, I want its log and `check` to say that the network
path fakes answers.

**Acceptance Scenarios**:

1. **Given** a scan that skipped a subnet for `faked_answers`, **Then** the collector logs a
   warning naming the subnet and suggesting to exclude local traffic from the VPN.
2. ~~**Given** `check` with a routed subnet planned on the default route, **Then** it tests the documentation address
   and prints either "network path: OK" or a warning that the path fakes answers.~~ (withdrawn)

### Edge Cases

- The VPN fakes TCP connects but not ICMP, or the reverse: the presence probe tries ICMP then TCP,
  so either kind of fake answer counts.
- Router fallback (feature 002): the ICMP/TCP fallback for a failed router read uses the same
  checks.
- A real, very full subnet (≥ 90% of addresses in use): reported as faked. Home subnets of /24
  are never that full; accepted.
- A server older than this feature rejects the new reason (400): deploy the server first, as with
  features 002 and 003.

## Requirements *(mandatory)*

### Functional Requirements

- ~~**FR-001**: Before probing routed subnets that use the default route, the collector MUST probe
  `192.0.2.1` once per scan; if it answers, those subnets MUST be skipped with reason
  `faked_answers` and not probed.~~ (withdrawn 2026-10-17)
- **FR-002**: After probing a routed subnet, if at least 16 addresses were probed and 90% or more
  answered, the collector MUST drop those observations and report the subnet as skipped with
  reason `faked_answers`.
- **FR-003**: The upload contract MUST accept `skip_reason = faked_answers` (additive v1 change).
- **FR-004**: The server MUST store such subnets like any skipped subnet; they never count as a
  completed scan.
- **FR-005**: The Collectors page MUST show a warning status ("scan error") for a collector whose
  latest run has a `faked_answers` subnet, and list each such subnet with an explanation.
- **FR-006**: The home page MUST show a "Scan problems" notice listing collector and subnet for
  each such subnet in the latest run of every collector that is not removed.
- **FR-007**: The collector MUST log a warning per such subnet. ~~`check` MUST report the result of
  the documentation-address test when a routed subnet on the default route is planned.~~

### Key Entities

- **Scan problem**: a subnet in a collector's latest run skipped for `faked_answers`.

## Success Criteria *(mandatory)*

- **SC-001**: With a path that answers every address, a scan records 0 observations for routed
  subnets (was 254).
- **SC-002**: The failed scan is visible on the Collectors page and the home page after the next
  upload, with the subnet and the reason.
- **SC-003**: With a normal path, routed scans give the same results as before.

## Assumptions

- ~~`192.0.2.0/24` (TEST-NET-1, RFC 5737) is never assigned to a real host, so an answer from it
  proves the path fakes replies.~~ Wrong in practice: proxies answer for public addresses only.
- Fixing the machine (e.g. Happ "bypass LAN") is the owner's action; the collector only detects
  and reports.
