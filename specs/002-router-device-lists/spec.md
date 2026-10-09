# Feature Specification: Router Device Lists

**Feature Branch**: `002-router-device-lists`

**Created**: 2026-10-08

**Status**: Implemented (real-network validation pending: tasks.md T037)

**Input**: User description: "Router device lists (Huawei HG8145V5 first). The desktop collector can't learn MAC addresses on subnets it reaches only through a router (e.g. 192.168.0.0/24 behind 192.168.8.1). The ISP router at 192.168.0.1 (Huawei HG8145V5 GPON ONT) knows every device on its LAN: hostname, IP, MAC, LAN port / Wi-Fi, online status, connection duration (its "User Device Information" page, behind a login). Let a collector read that list with owner-supplied router credentials and report the online devices as observations with real MACs, so routed-subnet devices get proper identities (MAC, manufacturer, randomized flag), weak devices fold into MAC devices, and offline detection is accurate. Router credentials stay on the collector machine and are never uploaded. Support for more router models can be added later. Also add an extra_subnets option so routed subnets can be scanned in addition to auto-discovered ones."

## Context

Feature 001 discovers devices with ARP on the subnets a collector is directly attached to. A subnet
reached **through a router** (here 192.168.0.0/24, one hop behind 192.168.8.1) can only be probed
for presence: MAC addresses never cross a router, so those devices get weak identities (IP or
hostname only), no manufacturer, and miss devices that ignore pings (e.g. sleeping phones).

The router that serves such a subnet already knows every device on it. On the owner's network the
ISP router (Huawei HG8145V5) lists 28 devices with hostname, IP, MAC and LAN port, 14 of them
online; a ping sweep from the desktop found 13 of those 14. This feature lets a collector read
that list, with the owner's own router login, and use it as a first-class source of observations.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - See real devices behind the ISP router (Priority: P1)

As the owner, I give my desktop collector the address and login of my ISP router. From the next
scan on, the devices on the router's network appear in the inventory with their MAC address,
manufacturer and hostname, exactly like devices on directly attached subnets, including the
devices that don't answer pings.

**Why this priority**: It is the whole point of the feature: the routed subnet becomes as visible
as the directly attached ones.

**Independent Test**: Configure the router on the desktop collector, run one scan, and compare the
online devices on the router's own device page with the inventory: each one appears once, with its
MAC.

**Acceptance Scenarios**:

1. **Given** a collector configured with the router's address and a valid login, **When** it
   runs a scan, **Then** every device the router reports as online appears in the inventory with
   its IP, MAC, hostname (when the router knows one) and manufacturer (when the MAC is not
   randomized).
2. **Given** the router reports a device that also answered a ping earlier and was recorded with
   a weak identity, **When** the router's data arrives, **Then** the weak record is folded into the
   MAC-identified device and its history is kept.
3. **Given** a device on the router's network that ignores pings (e.g. a sleeping phone), **When**
   the router reports it as online, **Then** it appears in the inventory.
4. **Given** the router's list includes devices it reports as offline, **When** the collector
   reads the list, **Then** those entries do not create sightings or mark anything online.
5. **Given** the router reads succeed at every scan, **When** a device disappears from the
   router's online list for the offline threshold, **Then** it is marked offline exactly as on a
   directly scanned subnet.

---

### User Story 2 - Keep the router login safe and visible (Priority: P2)

As the owner, I want to be sure my router password never leaves the collector machine, and I want
to see whether router reads are working without logging into the router myself.

**Why this priority**: Storing a password for network equipment is new to the system; it must be
trustworthy and diagnosable, but it supports Story 1 rather than adding visible inventory.

**Independent Test**: Inspect what the collector uploads and what the server stores: no router
password appears anywhere. Break the password on purpose: `check` and the Collectors page show
the problem.

**Acceptance Scenarios**:

1. **Given** a configured router, **When** the collector uploads a run, **Then** the upload
   contains the router's device observations but not the router password or username.
2. **Given** a configured router, **When** the owner runs the collector's check command, **Then**
   it reports whether the router was reached, whether the login was accepted, and how many devices
   the router lists (online and offline).
3. **Given** a wrong password or an unreachable router, **When** a scan runs, **Then** the rest of
   the scan still completes and is uploaded, the router part is reported as failed with a reason,
   and the Collectors page shows the failure for that collector.
4. **Given** a router read, **When** it finishes, **Then** the collector has logged out of the
   router, so the owner can still log into the router's own page.

---

### User Story 3 - Scan routed subnets without losing auto-discovery (Priority: P3)

As the owner, I want to add a routed subnet (like 192.168.0.0/24) to a collector's scan list
while it keeps discovering its directly attached subnets automatically.

**Why this priority**: Useful on its own for networks without a supported router, and a fallback
when the router can't be read; smaller in value than Story 1.

**Independent Test**: Add 192.168.0.0/24 as an extra subnet, run check and a scan: both directly
attached subnets and 192.168.0.0/24 are covered; attach a new adapter and the next scan covers it
too.

**Acceptance Scenarios**:

1. **Given** a collector with extra subnets configured and no fixed subnet list, **When** it
   scans, **Then** it covers its directly attached subnets plus the extra ones.
2. **Given** an extra subnet that is directly attached anyway, **When** the collector scans,
   **Then** it is scanned once, with the directly attached method.
3. **Given** an extra subnet that is not private, **When** the collector loads its configuration,
   **Then** it refuses the configuration with a clear error.

### Edge Cases

- **Router serves a subnet the collector also scans**: router data and the collector's own probes
  describe the same devices; each device appears once (identified by MAC), with sightings from
  both sources.
- **Router reports a device outside its own subnet** (e.g. a stale DHCP entry with another
  network's address): the entry is ignored.
- **Router entry without hostname** (`--`): the device is recorded by MAC, named by IP.
- **Randomized MAC** (phones with private Wi-Fi addresses): recorded and flagged as randomized,
  like any other randomized MAC.
- **Same hostname for two MACs** (e.g. two "BEAR" entries): they remain two devices.
- **Router session limit**: the router may allow only one logged-in session; if the owner is
  logged in at the same moment, the read fails for that scan with a clear reason and is retried
  next scan, without locking the owner out.
- **Router firmware changes its pages**: the read fails with a "router page not understood"
  reason; the rest of the scan is unaffected.
- **Login lockout**: repeated failed logins can lock a router; after a rejected login the
  collector stops trying the router until its configuration changes or the owner retries
  explicitly.
- **Router at an address reached via another router**: allowed, as long as it is a private
  address.

## Requirements *(mandatory)*

### Functional Requirements

**Router device lists**

- **FR-001**: A collector MUST be able to read the device list of a configured router and report
  each device the router lists as **online** as an observation with its IP, MAC and (when known)
  hostname.
- **FR-002**: The first supported router model MUST be the Huawei HG8145V5. The configuration MUST
  name the model, so further models can be added later without changing existing setups.
- **FR-003**: Router access MUST be opt-in per collector: no router is contacted unless the owner
  configured it, and only with the credentials the owner supplied. The system MUST NOT guess or try
  default credentials.
- **FR-004**: Router access MUST be read-only: the collector MUST NOT change any router setting,
  and MUST end its router session after each read.
- **FR-005**: The router's address MUST be a private (RFC 1918) address.
- **FR-006**: Router credentials MUST be stored only on the collector machine and MUST NEVER be
  included in uploads, logs, check output or the server's data. They are kept as plain text in the
  collector's configuration file, protected by the machine's file permissions like the collector
  token (decided 2026-10-08; encryption at rest is out of scope).
- **FR-007**: Entries the router reports as offline MUST NOT produce observations.
- **FR-008**: Router entries whose address is outside the router's own subnet MUST be ignored.
- **FR-009**: The router's subnet MUST be reported in the run like a scanned subnet, so it is
  discovered automatically (feature 001, FR-006) and a successful, complete router read counts as
  a completed scan of that subnet for offline detection (feature 001, FR-018).
- **FR-010**: If a router read fails (unreachable, login rejected, page not understood, session
  busy), the rest of the scan MUST still complete and upload, and the run MUST NOT count as a
  completed scan of the router's subnet.
- **FR-011**: After a rejected login, the collector MUST NOT try the router again until its router
  configuration changes or the owner runs the check command, to avoid locking the router.
- **FR-012**: The collector's check command MUST report, per configured router: reachable or not,
  login accepted or not, and the number of devices listed (online and offline), without printing
  credentials.
- **FR-013**: The Collectors page MUST show, per collector, the outcome of its latest router read
  (OK with device count, or the failure reason).
- **FR-014**: Each router observation SHOULD record how the device is connected according to the
  router (LAN port or Wi-Fi), for later use by the topology map.

**Extra subnets**

- **FR-015**: A collector MUST accept a list of extra subnets that are scanned **in addition to**
  its automatically discovered subnets; the existing fixed subnet list keeps its meaning
  (replacing auto-discovery).
- **FR-016**: Extra subnets MUST be private, MUST follow the same size limits as other configured
  subnets, and a subnet that is also directly attached MUST be scanned only once, with the
  directly attached method.
- **FR-017**: The collector's check command MUST list extra subnets with how each will be scanned.

### Key Entities

- **Router source**: a router a collector reads devices from. It has a model, a private address,
  a username and a password (kept on the collector machine only), and the outcome of its last
  read.
- **Router observation**: a device the router reports as online at read time: IP, MAC, hostname
  (optional) and connection (LAN port or Wi-Fi, optional). It identifies devices exactly like an
  ARP observation.
- **Router read outcome**: OK (with online/offline counts) or a failure reason (unreachable, login
  rejected, page not understood, session busy), shown to the owner.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: With the ISP router configured, 100% of the devices the router lists as online
  appear in the inventory after one scan, each with its MAC address.
- **SC-002**: Devices on the router's subnet that were previously known only by IP become
  MAC-identified after the first successful router read, with no duplicate device left behind.
- **SC-003**: A device that goes offline on the router's subnet is shown offline within the same
  number of scans as on a directly scanned subnet.
- **SC-004**: No router credential appears in any upload, server record, log line or check output
  (verified by searching all of them for the configured password).
- **SC-005**: A wrong router password or an unreachable router never prevents the rest of a scan
  from being uploaded, and the failure is visible on the Collectors page after that scan.
- **SC-006**: The owner can still log into the router's own page between collector scans (the
  collector never leaves a session open).
- **SC-007**: Adding an extra subnet never stops the collector from discovering a newly attached
  adapter on its next scan.

## Assumptions

- The desktop collector (Windows) is the first one to use router sources, because it can reach the
  ISP router; the NAS collector may use them too if it can reach the router.
- The owner's router account can see the device list; the feature does not handle ISP-restricted
  accounts that can't.
- Router reads happen once per collector scan (every collector interval), not more often.
- Only IPv4 data from the router is used; IPv6 addresses it shows are ignored (IPv6 is out of scope
  for the project).
- Supporting additional router models, and reading switches or Wi-Fi access points, are separate
  later features.
- Plain HTTP to the router is acceptable on the home LAN, like the router's own web page.
- The router password sits in plain text in the collector's configuration file (owner's choice);
  the owner is responsible for keeping that folder private, as for the collector token.
