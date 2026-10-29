# Feature Specification: Open Ports per Device

**Feature Branch**: `015-device-open-ports`

**Created**: 2026-10-26

**Status**: Draft (clarified 2026-10-26)

**Input**: User description: "On each ip address (device) could be several ports and protocols, lets
collect all open ports and protocols and publish it on each device page"

## Context

Features 001–014 find which devices are on the network and how they connect, but say nothing about
what a device offers: a camera's web page, a NAS's file shares, a player's remote-control port. The
owner wants each device page to list the network services the device answers on, so a device can
be recognised and reached ("the Axis camera has a web UI on 80 and RTSP on 554").

This changes a rule of feature 001: FR-007 limits discovery to presence and identity, and port
scanning was out of scope there. Checking whether a port accepts a connection is still
non-intrusive in the sense of FR-007 (no logins, exploits or brute force), and this feature keeps
that limit.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - See a device's open ports (Priority: P1)

As the owner, I open a device page and see the ports the device accepts connections on, each with
its protocol (TCP or UDP), the service usually found there (for example "http", "ssh", "rtsp"),
when it was last found open, and which collector found it.

**Why this priority**: This is the feature: without it nothing is shown.

**Independent Test**: With a device on the network that runs a web server on port 80 and SSH on
port 22, run a scan, open the device page and check that both ports are listed with their protocol
and service name, and that a closed port (for example 23) is not.

**Acceptance Scenarios**:

1. **Given** a device that accepts connections on TCP 80 and TCP 22, **When** a port scan of its
   subnet has run, **Then** its device page lists "22/tcp ssh" and "80/tcp http" with the time they
   were last found open.
2. **Given** a device with no open ports among those checked, **When** its page is opened, **Then**
   the page says no open ports were found and when the device was last checked.
3. **Given** a device that has never been port-scanned (for example added by hand, feature 011, or
   only known from a router's client list), **When** its page is opened, **Then** the page says its
   ports have not been checked yet.

---

### User Story 2 - See when ports change (Priority: P2)

As the owner, I see when a device opens a new port or stops answering on one, so a changed device
(a new service, a firmware update, something unexpected listening) stands out.

**Why this priority**: The constitution requires changes to be detectable over time (Principle V);
a list that only shows the present hides that a port was closed yesterday.

**Independent Test**: Scan a device with port 80 open, stop the web server, scan again: the device
page shows 80/tcp as closed since the second scan, and the device's history shows "port 80/tcp
closed" with the time.

**Acceptance Scenarios**:

1. **Given** a device whose port 8080/tcp was not open, **When** a scan finds it open, **Then** the
   device's history records "port 8080/tcp opened" with the time and the collector.
2. **Given** a device whose port 80/tcp was open, **When** the next complete port scan of that device
   finds it closed, **Then** the device page no longer lists it as open, shows it under "previously
   open" with the time it was last seen open, and the history records "port 80/tcp closed".
3. **Given** a device that is offline, **When** its page is opened, **Then** its last known ports
   are shown as last known, not as closed.

---

### User Story 3 - Control the port scan (Priority: P3)

As the owner, I can turn port scanning off for a subnet or for a single device (a fragile device, a
device I do not want touched), and I can see which ports are checked.

**Why this priority**: Port checks send many more packets than presence checks; some cheap IoT
devices misbehave under them, and the owner must be able to stop that without stopping inventory.

**Independent Test**: Turn port scanning off for one device, run a scan: the device is still found
(presence), but its ports are not checked and its page says port scanning is off for it.

**Acceptance Scenarios**:

1. **Given** port scanning is off for a device, **When** a scan runs, **Then** no port of that
   device is checked, its presence is still recorded, and its page keeps the last known ports
   marked as such.
2. **Given** port scanning is off for a subnet, **When** a scan runs, **Then** no device of that
   subnet is port-checked.
3. **Given** the Settings page, **When** the owner opens it, **Then** it shows which ports are
   checked (all TCP, plus the listed UDP services), the schedule (daily) and on how many subnets
   port checks are on.
4. **Given** the daily check of a device ran this morning, **When** the owner presses the force
   button for the NAS collector, or runs the desktop collector's scan with the force flag, **Then**
   every reachable device is port-checked now and the device pages show the new results.

### Edge Cases

- A device with several addresses (one MAC, two subnets): each address is checked; a port is
  listed once, with the address and collector of its latest finding.
- A device that answers on every port (some firewalls or captive devices accept any connection): the
  list is capped and the page says the device answers on all ports checked.
- A device seen only through a router's client list on a subnet the collector cannot reach directly:
  its ports are not checked; the page says why.
- A port found open by one collector and closed by another (different vantage, a firewall between
  subnets): each result is kept with its collector; the device page shows it open, with which
  collector saw it.
- A scan interrupted or timed out for a device: its ports keep their previous state; nothing is
  marked closed from an incomplete check.
- A device whose address changed (DHCP): ports belong to the device (by MAC), not to the old
  address.
- Manual devices (feature 011) have no address and are never port-scanned.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Collectors MUST check, for every device they find on a subnet they scan, which ports
  accept connections: all TCP ports (1–65,535), and the UDP ports of the well-known services that
  answer a standard request (FR-003). *(Owner, 2026-10-26: all TCP ports.)*
- **FR-002**: Port checks MUST stay non-intrusive (001 FR-007): only a connection attempt, or for
  UDP a standard harmless request, then close; no logins, no payloads beyond what identifies the
  service, no brute force, no exploits.
- **FR-003**: Each open port MUST be reported with its number, its transport (TCP or UDP) and the
  service name usually found on it; a service identified from the device's own reply (for example
  an HTTP or SSH greeting) MAY replace the usual name.
- **FR-004**: The server MUST record each port result with its time, collector, device, address and
  subnet (Principle V), and derive each device's current open ports from those records.
- **FR-005**: A port MUST be considered closed only after a complete port check of that address by a
  collector finds it closed; an incomplete check MUST NOT close any port.
- **FR-006**: Opening and closing of a port MUST be recorded as device history events ("port
  80/tcp opened", "port 80/tcp closed") with the time and collector.
- **FR-007**: The device page MUST list the current open ports (port, transport, service, address,
  last found open, collector), the previously open ports with the time they were last seen open,
  and the time of the last port check; it MUST say when the device has not been checked or port
  scanning is off for it.
- **FR-008**: Port checks MUST run, by default, once a day per device, and at the first scan that
  finds a new device; the daily checks MUST be spread over the day, not all at once. *(Owner,
  2026-10-26.)*
- **FR-008a**: The owner MUST be able to force a port check of every device now, regardless of the
  daily schedule: on a standalone collector with a command-line flag of its scan command (for
  example on the desktop), and for the server's built-in collector (the NAS) with a button in the
  web UI. A forced check reports its progress like a normal scan and is recorded as a complete port
  check (FR-005).
- **FR-009**: The owner MUST be able to turn port scanning off and on for a subnet and for a single
  device; the choice MUST survive scans and be recorded like other owner facts (feature 001).
- **FR-010**: Port checks MUST NOT delay a collector's presence scan beyond its interval; they run
  alongside it, rate-limited per device so that one device never receives a burst it cannot handle.
- **FR-011**: Port results MUST stay local (Principle I): no external service is used to identify
  services; the port-to-service names come with the application.
- **FR-012**: Removing devices or all data (feature 004) MUST remove their port records too.

### Key Entities

- **Port result**: one check of one port on one address by one collector at one time: port number,
  transport (TCP/UDP), open or closed, service name, how the service was identified (usual name or
  device reply).
- **Device port** (derived): a port a device has been seen with: port, transport, service, state
  (open, previously open, last known), first and last time found open, the address and collector.
- **Port scan setting**: the owner's on/off choice for a subnet or a device, with time.
- **Port list**: the ports checked (all TCP; the UDP services), shown on the Settings page.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: For the devices on the home network, every service the owner knows to be running on a
  port in the port list (camera web UIs, the NAS's web and file-sharing ports, SSH) appears on the
  device page within one scan cycle after it starts.
- **SC-002**: A port that stops answering is shown as no longer open within two scan cycles, and
  never because of a single missed or interrupted check.
- **SC-003**: The owner can tell from a device page, in under 10 seconds, what the device offers and
  how to reach it (port and service).
- **SC-004**: The presence scan keeps finishing within the collector's interval (15 minutes by
  default) while daily port checks run; a forced port check of every online device on the home
  network (about 30 devices, all TCP ports) finishes within 30 minutes.
- **SC-005**: No device on the home network stops working or reboots because of port scanning over
  one week of normal operation.

## Assumptions

- Device identity is unchanged (MAC, 001 data model); ports are attached to devices through their
  addresses.
- The collectors that already scan a subnet do the port checks for it (the NAS for 192.168.1.0/24,
  the desktop for its subnets); no new collector is needed. Subnets known only from a router's
  client list (192.168.0.0/24 from the NAS) are port-checked only by a collector attached to them.
- The collector-to-server upload format gets a new, optional part for port results; older collectors
  keep working and simply report no ports (versioned contract, Principle IV).
- UDP is reported only where a service answers a credential-free request (DNS, NTP, NetBIOS,
  SSDP, mDNS); SNMP is not checked, since every SNMP request carries a community string (a login
  guess, FR-002). A silent UDP port is not reported, since silence cannot tell open from filtered.
- The service names come from a list shipped with the application (the usual names of well-known
  ports); identifying exact software versions is out of scope.
- Port history uses the same retention as other device history (no new retention rule).
- Feature 001 FR-007 and its "port scanning out of scope" line are amended to point here.
