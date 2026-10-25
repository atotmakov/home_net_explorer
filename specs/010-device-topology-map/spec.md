# Feature Specification: Device Topology Map

**Feature Branch**: `010-device-topology-map`

**Created**: 2026-10-23

**Status**: Draft

**Input**: User description: "On the Map tab i would like to see device toplogy including all
deveces - online and offline"

## Context

The navigation bar has had a **Map** tab since feature 001, but the page was never built (001 User
Story 4, tasks T084–T090, are open; `/map` answers "not found"). This feature delivers the map,
with one change to the 001 plan: it shows **every known device by default, online and offline**,
instead of starting with online devices only. It also uses connection information that did not
exist when 001 was written: the router port or Wi-Fi interface that a router reports for each
device (feature 002), and the online / stale / offline states of feature 006.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - See every device in its place on the network (Priority: P1)

As the owner, I open the Map tab and see my whole network at once: each subnet as a group, every
device I have ever seen inside the subnet it belongs to, whether it is online right now or not, and
its status shown the same way as on the Devices list.

**Why this priority**: This is the request itself: one picture of all devices, including the ones
that are currently offline. Without it there is no map at all.

**Independent Test**: With an inventory of devices in three subnets, some online, some stale and
some offline, open the Map tab and check that every device of the Devices list appears once, in the
right subnet, with the right status.

**Acceptance Scenarios**:

1. **Given** an inventory with devices in 192.168.0.x, 192.168.1.x and 192.168.8.x, **When** the
   owner opens the Map tab, **Then** each subnet appears as a labelled group and each device
   appears inside its subnet's group.
2. **Given** devices that are online, stale ("online" but not seen recently) and offline, **When**
   the map is shown, **Then** all of them are on it, and each shows its status with the same words
   and colors as the Devices list (online green, stale in the normal text color, offline gray).
3. **Given** the map, **When** the owner counts the devices on it, **Then** the number equals the
   number of devices on the Devices list with no filter.
4. **Given** the map, **When** the owner points at a device, **Then** its name (or address), IP
   address, MAC address and "last seen … ago" are shown.
5. **Given** a device that was removed or merged into another device, **When** the map is shown,
   **Then** it does not appear (as on the Devices list).

---

### User Story 2 - See how devices are connected (Priority: P1)

As the owner, I want to see what connects to what: the router or gateway of each subnet and,
where a router reports it, which router port or Wi-Fi interface a device is connected through.
(A "two-subnet" link for devices in two subnets at once was part of this story until 2026-10-25;
the owner removed it: a PC with two network adapters has two MACs and is two devices, FR-006.)

**Why this priority**: "Topology" means connections, not only groups. The data for the connections
is already collected; without it the map is just the Devices list drawn as boxes.

**Independent Test**: With a router that reports ports and Wi-Fi for its devices, open the map
and check the gateway and the per-port / per-Wi-Fi grouping.

**Acceptance Scenarios**:

1. **Given** a subnet whose gateway is known, **When** the map is shown, **Then** the gateway
   device is marked as the gateway of that subnet.
2. **Given** a device with addresses in two subnets, **When** the map is shown, **Then** it appears
   once, in the subnet of its first address (no two-subnet link since 2026-10-25, FR-006).
3. **Given** a router that reports the port or Wi-Fi interface of its devices (the Huawei router
   reports its LAN ports as LAN1–LAN4 and its Wi-Fi networks as SSID1, SSID2, …; shown as the
   router reports them), **When** the map is shown, **Then** the devices behind that router are
   linked to it through their reported port or Wi-Fi interface.
4. **Given** an offline device whose last report came from a router port, **When** the map is shown,
   **Then** it is linked through that last known port, and the link is drawn as "last known"
   rather than current.

---

### User Story 3 - Focus on what is online (Priority: P2)

As the owner, I can hide offline devices (and later show them again) when the full map is too busy,
without leaving the page.

**Why this priority**: All devices is the default the owner asked for; hiding offline ones is a
convenience on a busy map.

**Independent Test**: Open the map, switch to "online only", check offline devices and their links
disappear and stale devices stay, switch back and check everything returns.

**Acceptance Scenarios**:

1. **Given** the map showing all devices, **When** the owner switches to "online only", **Then**
   offline devices and their links are hidden, and online and stale devices stay.
2. **Given** "online only", **When** the owner switches back to "all devices", **Then** every device
   is shown again.
3. **Given** the owner reopens the Map tab later, **Then** it opens with all devices shown.

---

### User Story 4 - Open a device from the map (Priority: P2)

As the owner, I click a device on the map to see its details, and I can go from there to its
device page.

**Why this priority**: The map is a way into the inventory; it is useful without it but much less.

**Independent Test**: Click a device on the map and check its details appear and its device page
opens from there.

**Acceptance Scenarios**:

1. **Given** the map, **When** the owner selects a device, **Then** its details are shown next to
   the map (name, type, addresses, MAC, vendor, status, last seen, connection) with a link to the
   device page.
2. **Given** a selected device, **When** the owner selects another one or an empty area, **Then**
   the details change or close.

---

### User Story 5 - Record a connection the scans cannot see (Priority: P3)

As the owner, I can say that a device connects through another device (for example "the phone is
on the Wi-Fi access point in the living room"), and the map keeps that link after future scans.

**Why this priority**: Scans cannot see access points or switches without a router report (001
FR-028); a manual link fills the gap. Valuable but not needed for a first map.

**Independent Test**: Set "connected via" for a device, run a scan, and check the link is still on
the map; remove it and check it is gone.

**Acceptance Scenarios**:

1. **Given** a device, **When** the owner sets "connected via" another device, **Then** the map
   shows a link between them, marked as set by the owner.
2. **Given** a manual link, **When** new scans arrive, **Then** the link is still shown.
3. **Given** a manual link, **When** the owner removes it, **Then** it disappears from the map.
4. **Given** a manual link and a router-reported port for the same device, **When** the map is
   shown, **Then** the owner's link is shown and the router-reported one is not.

### Edge Cases

- A device with no current address in any known subnet (for example only seen in a subnet the
  owner has since ignored) appears in an "Other" group rather than being dropped.
- A subnet with no devices is not shown as an empty group, except the subnets of the server and of
  each collector, which are always shown so the owner sees what is scanned.
- Address entries that are not real devices (for example a broadcast MAC address) are shown like
  any other device on the Devices list; whether to keep them out is a separate inventory question,
  not a map question.
- A large network (250 devices) stays usable: it is drawn within the SC-002 time, offline devices
  are dimmed relative to online ones, and the owner can zoom and pan.
- With no devices at all, the map shows a short message pointing to "Scan now" on the home page.
- The page works with no internet connection (constitution, Principle I).

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The Map tab MUST show every device that the Devices list shows with no filter,
  online, stale and offline, each exactly once.
- **FR-002**: Devices MUST be grouped by subnet; each group MUST show the subnet's name (if the
  owner named it) or its address range.
- **FR-003**: Each device MUST show its status with the same words and colors as the Devices list
  (feature 006: online green, stale in the normal text color, offline gray) and a "last seen …
  ago" hint.
- **FR-004**: The map MUST open with all devices shown, and MUST offer a switch between "all
  devices" and "online only" (online and stale) that does not reload the page.
- **FR-005**: The gateway of each subnet, when known, MUST be marked as such.
- **FR-006**: A device with current addresses in more than one subnet MUST appear once, in the
  subnet of its first address. *(Changed 2026-10-25 by the owner: the "two-subnet" link, `bridge`
  in the map data, is removed. A computer with two network cards has two MACs and is two devices;
  a device that left a subnet loses its address there, PR #38.)*
- **FR-007**: When a router reports the port or Wi-Fi interface of a device, the device MUST be
  linked to that router through the reported port or interface; for an offline device the last
  reported one is used and drawn as "last known".
- **FR-008**: Selecting a device MUST show its details next to the map, with a link to its device
  page.
- **FR-009**: The owner MUST be able to set and remove a manual "connected via" link from a device
  to another device; manual links MUST survive scans and take precedence over router-reported
  connections for the same device.
- **FR-010**: Links MUST be visually distinguishable by kind: gateway, router port/Wi-Fi, manual.
- **FR-011**: The map MUST use only data already collected; it MUST NOT query switches, access
  points or routers when it is opened (001 FR-028), and MUST work without internet access.
- **FR-012**: Removed or merged devices MUST NOT appear on the map.

### Key Entities

- **Subnet group**: a subnet in the inventory; name or address range; whether it is stale (no
  recent complete scan, feature 006).
- **Device on the map**: a device of the inventory with its status, name, type, addresses, and
  whether it is a gateway.
- **Connection**: a link between a device and a subnet or another device, of one kind: gateway,
  router port / Wi-Fi (with the port or interface name, current or last known), or
  manual (set by the owner).

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: On the owner's network, the number of devices on the map equals the number on the
  unfiltered Devices list (today 28), and the online / stale / offline counts equal the home page
  counts.
- **SC-002**: The map of a 250-device inventory is shown within 2 seconds of opening the Map tab on
  the home network.
- **SC-003**: Switching between "all devices" and "online only" updates the map within 1 second.
- **SC-004**: The owner can find any device on the map and open its device page in at most two
  clicks after locating it.
- **SC-005**: A manual "connected via" link is still shown after 3 later scans.

## Assumptions

- "All devices" means everything the Devices list shows without a filter; removed and merged
  devices are excluded as they are everywhere else.
- Status words, colors and the stale rule are those of feature 006; this feature does not change
  them.
- Router port / Wi-Fi information exists only for subnets read from a router (feature 002, today
  192.168.0.x through the Huawei router); devices elsewhere are grouped by subnet only.
- The Timeline tab, also never built (001 User Story 3), is out of scope for this feature.
- The device type (router, access point, phone, ...) comes from what the inventory already knows
  or the owner set; no new type detection is added.
- No login (feature 007): anyone on the home network who can open the UI can see the map and set
  manual links.
