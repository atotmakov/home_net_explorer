# Feature Specification: LAN Inventory & Topology Explorer

**Feature Branch**: `001-lan-inventory-topology`

**Created**: 2026-10-04

**Status**: Draft

**Input**: User description: "Discovery + inventory + topology visualiser. Scan the home LAN, list
devices (IP, MAC, hostname), track changes and updates over time, visualizing how devices connect
with each other. Web UI should work in containers on NAS. NAS works inside 192.168.1.* subnet. My
desktop sees two subnets 192.168.1.* and 192.168.0.*. So the info about subnets should be collected
from browser running on my desktop, or from some dropped binary, which uploads data to the web app
DB."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - See every device on my home subnet (Priority: P1)

As the home network owner, I open the web app hosted on my NAS and see a list of all devices
currently present on the subnet(s) the NAS is attached to, each with its IP address, MAC address,
hostname (when one can be resolved), manufacturer (when it can be derived from the MAC), and when it
was first and last seen.

**Why this priority**: This is the core value of the product. Without an inventory, there is nothing
to track or visualize. It works with the server alone, with no extra setup on other machines.

**Independent Test**: Deploy only the server on the NAS, trigger a scan, and compare the device list
with the router's DHCP client table and the devices known to be powered on.

**Acceptance Scenarios**:

1. **Given** the server is running on the NAS and has never scanned, **When** the user starts a
   scan from the web UI, **Then** within 2 minutes the device list shows every responsive device on
   the NAS's subnet(s), with IP, MAC, and hostname where available. No subnet has to be
   configured first.
2. **Given** a completed scan, **When** the user sorts or filters the list by IP, hostname, MAC,
   manufacturer, or status (online/offline), **Then** the list updates to match.
3. **Given** automatic scanning is enabled, **When** the scan interval elapses, **Then** a new scan
   runs without user action, and each device's last-seen time is updated.
4. **Given** a device, **When** the user assigns it a friendly name and notes, **Then** the name
   and notes are shown wherever the device appears and survive future scans.

---

### User Story 2 - Include subnets the NAS cannot see (Priority: P2)

As the owner, I run a small collector on my desktop. The desktop is attached to subnets the NAS
cannot see. The collector scans every subnet it can see and uploads its observations to the
server, so the inventory also covers those subnets. Which subnets exist can change at any time,
and the system adapts without configuration.

**Why this priority**: Part of the user's network is invisible from the NAS. Covering it is
required for a complete picture, but the product is already useful without it.

**Independent Test**: With the server running, run the collector on the desktop once and confirm
that devices on subnets only the desktop can see appear in the web UI, each attributed to the
desktop collector.

**Acceptance Scenarios**:

1. **Given** a collector configured with the server's address and an access token, **When** it runs
   a collection, **Then** its observations appear in the server's inventory within 1 minute after it
   finishes, labeled with the collector name and subnet.
2. **Given** a collector with a missing or invalid token, **When** it tries to upload, **Then** the
   server rejects the upload, stores nothing, and the collector reports a clear error.
3. **Given** the server is unreachable, **When** the collector finishes a collection, **Then** the
   collector keeps the results and retries the upload later instead of discarding them.
4. **Given** the same device is observed by both the NAS and the desktop collector, **When** both
   upload, **Then** the inventory shows one device that lists both sightings, not two devices.
5. **Given** the user opens the web UI, **When** they view the collectors page, **Then** they see
   each registered collector, the subnets it covers, and when it last reported.
6. **Given** a collector becomes attached to a subnet the system has never seen (e.g., a new
   router or VLAN), **When** it runs its next collection, **Then** the subnet is added to the
   inventory automatically, its devices appear, and the owner sees a "new subnet discovered"
   notice. No configuration change or restart is needed.

---

### User Story 3 - Track what changed over time (Priority: P3)

As the owner, I want to see what changed on my network: new devices, devices that went away or came
back, and changes to a device's IP address or hostname. I want to see these changes both as a
network-wide timeline and as each device's own history.

**Why this priority**: Spotting unknown devices and changes is the main ongoing reason to keep the
tool running. It depends on the inventory from P1/P2.

**Independent Test**: After a baseline scan, add a new device, change another device's IP, and power
off a third. Then confirm that all three changes appear on the timeline with correct times.

**Acceptance Scenarios**:

1. **Given** a baseline inventory, **When** a device never seen before appears in a scan, **Then**
   a "new device" event is recorded, and the device is marked as new until the user acknowledges it.
   Devices found by the first scan of a subnet form that subnet's baseline. They are recorded in
   history but not flagged as new.
2. **Given** a known device, **When** its IP address or hostname differs from its previous value,
   **Then** a change event is recorded that shows the old and new values.
3. **Given** a known device, **When** it is absent from scans for longer than the offline threshold,
   **Then** it is marked offline and a "went offline" event is recorded. When it is seen again, a
   "came back online" event is recorded.
4. **Given** recorded history, **When** the user opens a device, **Then** they see that device's
   full history of IPs, hostnames, presence, and the collectors that saw it.
5. **Given** recorded history, **When** the user filters the timeline by date range or event type,
   **Then** only matching events are shown.

---

### User Story 4 - Visualize how devices connect (Priority: P4)

As the owner, I want a visual map of my network that shows the subnets, the routers or gateways that
connect them, and which devices sit on which subnet, so I can understand the layout at a glance.

**Why this priority**: A map adds understanding, but it builds on the inventory and is not needed
for the inventory to be useful.

**Independent Test**: With an inventory that covers both subnets, open the map and confirm that each
device appears under the correct subnet and that the gateway linking the subnets is shown.

**Acceptance Scenarios**:

1. **Given** an inventory that covers both subnets, **When** the user opens the map, **Then** each
   subnet appears as a group, each device appears in its subnet, and the gateway devices that connect
   subnets are shown as links between them.
2. **Given** the map, **When** the user selects a device, **Then** its details and recent history
   are shown.
3. **Given** a device whose connection cannot be determined automatically, **When** the user sets
   the device it connects through (for example, "connected via Wi-Fi access point X"), **Then** the
   map reflects that link, and the link survives future scans.
4. **Given** the map, **When** the user switches between "online only" and "all known devices",
   **Then** the map updates to match.

Map depth (decided 2026-10-04): this feature covers subnet/gateway-level structure plus manual
links. Automatically discovering physical links (which switch port or access point each device is
on) is deferred to a separate, later feature.

---

### Edge Cases

- **Remote subnets have no MAC addresses**: devices on a subnet that a collector reaches only
  through a router may show no MAC address. These devices are identified by IP address and hostname,
  and they are marked as weakly identified.
- **Randomized/private MAC addresses** (phones, laptops): one physical device may appear as several
  devices. The system marks MACs that look randomized, and the user can manually merge two device
  records into one.
- **Multi-homed devices** (the desktop itself, routers): a device with addresses on both subnets is
  shown as one device with several addresses, not as several devices.
- **Out-of-order uploads**: when observations arrive late (for example, a collector was offline),
  they are placed in history by the time they were observed, not the time they arrived.
- **Clock skew**: if a collector's clock differs from the server's by more than 5 minutes, the
  server flags the collector in the UI.
- **IP reuse**: when an IP moves from one device to another, both devices' histories show the
  change correctly, and the address is not credited to the wrong device.
- **Scan coverage**: devices that block discovery probes may be missed. The UI shows when each
  subnet was last scanned and by which collector, so gaps are visible.
- **Newly appearing subnets**: an upload that covers a subnet never seen before is stored, and
  the subnet is tracked automatically. The owner is notified and can choose to ignore the
  subnet. Ignored subnets are no longer scanned or shown.
- **Very large subnets**: a directly attached subnet wider than /22 (e.g., a /16) is not scanned
  automatically. The UI shows it as skipped, and the owner can narrow it in that collector's
  settings.
- **Non-private networks**: subnets outside the private address ranges (e.g., a public
  address on a laptop) are never scanned.
- **Duplicate uploads**: if the same collection is uploaded twice, it does not create duplicate
  events.

## Requirements *(mandatory)*

### Functional Requirements

**Discovery & collection**

- **FR-001**: The server MUST be able to discover devices on its own directly attached subnet(s)
  without any other component.
- **FR-002**: The system MUST provide a collector that runs on a separate machine (e.g., the
  user's desktop), discovers devices on the subnets visible to that machine, and uploads the
  observations to the server.
- **FR-003**: For each discovered device, collection MUST capture, where available: IP address, MAC
  address, hostname, and the observation time.
- **FR-004**: Users MUST be able to start a scan on demand from the web UI for subnets the server
  scans itself, and on demand on the collector machine for collector-covered subnets.
- **FR-005**: The system MUST support automatic periodic scanning with a user-configurable interval
  (default: every 15 minutes) for both the server and collectors.
- **FR-006**: The system MUST NOT depend on specific subnets. By default, each collector MUST
  discover and scan every private (RFC 1918) IPv4 subnet it is directly attached to with a prefix
  of /22 or narrower. Subnets MUST be tracked automatically the first time any collector reports
  them. Users MAY restrict a collector to chosen subnets and MAY ignore a subnet.
- **FR-007**: Discovery MUST be non-intrusive. It is limited to identifying presence and identity,
  and it MUST NOT attempt logins, exploits, or brute force against devices.

**Collectors & uploads**

- **FR-008**: Uploads MUST be accepted only from registered collectors that present a valid access
  token, which the user issues and can revoke from the web UI.
- **FR-009**: Each observation MUST record which collector produced it and from which subnet.
- **FR-010**: Collectors MUST buffer results when the server is unreachable and retry later.
- **FR-011**: Re-uploading the same collection MUST NOT create duplicate observations or events.
- **FR-012**: The web UI MUST list collectors with their covered subnets, last-report time, and
  status.

**Inventory**

- **FR-013**: The system MUST merge observations into one record per physical device, using MAC
  address as the primary identity when available, and IP address plus hostname otherwise.
- **FR-014**: Users MUST be able to set a friendly name, notes, and a device type for each device.
  These edits MUST survive all future scans.
- **FR-015**: Users MUST be able to merge two device records into one and split a wrongly merged
  record.
- **FR-016**: The system MUST derive the device manufacturer from the MAC address using a lookup
  table stored locally, with no internet access.
- **FR-017**: The inventory list MUST support sorting and filtering by IP, MAC, hostname,
  manufacturer, subnet, status, and first/last seen.
- **FR-018**: A device MUST be marked offline when not observed for a configurable threshold
  (default: 3 consecutive missed scan intervals of every collector that covers its subnet **and
  is still reporting**). Collectors that have stopped reporting are ignored. If none are
  reporting, the subnet is shown as stale and device status is unchanged.

**History & changes**

- **FR-019**: The system MUST keep every observation with its observation time, and MUST NOT
  overwrite history when a device's details change.
- **FR-020**: The system MUST record change events: new device, IP changed, hostname changed, MAC
  changed, went offline, came back online, devices merged or split.
- **FR-021**: Users MUST be able to view a network-wide event timeline, filterable by date range,
  event type, device, and subnet.
- **FR-022**: Users MUST be able to view each device's full history.
- **FR-023**: New devices MUST stay highlighted as "new" until the user acknowledges them.
- **FR-024**: History MUST be kept indefinitely. A retention or pruning setting is out of scope
  for this feature.

**Topology map**

- **FR-025**: The system MUST display a map that groups devices by subnet and shows the gateways and
  multi-homed devices that connect subnets.
- **FR-026**: Users MUST be able to set a manual "connected via" link from a device to another
  device (e.g., an access point or switch). The map MUST show these links, and they MUST survive
  scans.
- **FR-027**: Selecting a device on the map MUST show its details and recent history.
- **FR-028**: The map MUST NOT depend on querying network equipment (switches, access points,
  routers) for physical link data. Physical-link discovery is out of scope for this feature.

**Deployment, access & privacy**

- **FR-029**: The server (web UI and its data) MUST run as a container on the user's NAS, and its
  data MUST persist across container restarts and upgrades.
- **FR-030**: The web UI MUST require a login (a single owner account) before showing any network
  data.
- **FR-031**: No collected data MUST ever be sent outside the home network. The system MUST work
  with no internet access.
- **FR-032**: Users MUST be able to export the inventory and history to a file for backup.

### Key Entities

- **Device**: a physical or virtual network endpoint as the user understands it. It has an
  identity (MAC when known), user-assigned name, notes, and type, a manufacturer, a status
  (online/offline/new), first-seen and last-seen times, and one or more current addresses.
- **Observation**: a single sighting of an address and identity by a collector at a point in time:
  IP, MAC (if visible), hostname (if resolved), subnet, collector, and observed-at time. It is
  immutable.
- **Collector**: a source of observations (the server's own scanner, or a remote collector such as
  the desktop). It has a name, an access token, covered subnets, a last-report time, and a status.
- **Subnet**: an address range discovered when a collector first reports it. It has an optional
  name, the collectors that cover it, its last-scanned time, and an ignored flag.
- **Change Event**: a derived record of a meaningful change (new, IP/hostname/MAC changed,
  offline/online, merged/split) with the time, device, old value, and new value.
- **Link**: a connection between devices or subnets on the map. It is either inferred (gateway,
  or a **bridge** formed by a device with addresses in several subnets) or set manually by the
  user.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A user can go from a freshly deployed server to a populated inventory of the NAS
  subnet in under 10 minutes, including setup.
- **SC-002**: A full scan of one /24 subnet finishes and is shown in the UI within 2 minutes.
- **SC-003**: The inventory includes at least 95% of the devices listed as active in the router's
  DHCP client table at scan time.
- **SC-004**: Adding the desktop collector makes devices on subnets only the desktop can see
  visible in the UI within 1 minute after its first collection completes.
- **SC-005**: A newly connected device appears as "new" in the UI no later than one scan interval
  after it joins the network.
- **SC-006**: A device seen by both the NAS and the desktop appears as exactly one device in 100%
  of test cases where its MAC is visible to both.
- **SC-007**: The device list, timeline, and map each load within 2 seconds for a network of 250
  devices with one year of history.
- **SC-008**: During operation, the system makes zero network connections to destinations
  outside the private address ranges of the home network.
- **SC-009**: The user can answer "what joined my network in the last 7 days?" in under 30 seconds
  from the home page.
- **SC-010**: A subnet never seen before appears in the inventory, with its devices, after the
  first collection that covers it, with zero configuration changes.

## Assumptions

- There is one user, the network owner. Multi-user roles are out of scope.
- The web UI is accessed from a browser on the home LAN. Remote access from outside the home is out
  of scope.
- The desktop collector is a small program that the user downloads from the web app and runs. A
  web page in a browser cannot reliably find devices (it cannot read hardware addresses or send
  discovery probes), so a browser-only collector is not the primary approach.
- The collector may run on a schedule while the desktop is on. Gaps while the desktop is off are
  acceptable and visible in the UI.
- The NAS can run containers with access to the host network, which is needed for discovery on its
  own subnet.
- Home network size is at most about 250 devices across at most a few /24 subnets.
- Only private IPv4 networks (RFC 1918) are in scope. IPv6 discovery is out of scope for this
  feature.
- Alerts outside the UI (email, push, chat) are out of scope for this feature. Changes are
  surfaced in the UI.
- Port scanning and service/OS fingerprinting are out of scope for this feature.
- Automatic physical topology discovery (switch ports, access point clients) is planned as a later,
  separate feature. The data model should not prevent adding it, but nothing is built for it now.
- The MAC manufacturer lookup table ships with the application and is updated with application
  releases.
