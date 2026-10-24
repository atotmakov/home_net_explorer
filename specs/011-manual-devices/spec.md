# Feature Specification: Manual Devices

**Feature Branch**: `011-manual-devices`

**Created**: 2026-10-24

**Status**: Paused (2026-10-24, owner: 012 first)

**Input**: User description: "I have tp link sl1218p router, it is unmanaged device so there is no
ip on it, network scanner does not see it. Lets add ability to create device without ip and
integrate it in network map and device list, looks like new feature."

## Context

The inventory holds only devices that scans or router reads have seen, and a device is seen through
its IP address. Some equipment has no IP address at all: the owner's TP-Link TL-SL1218P is an
unmanaged switch that sits between the Huawei router (its LAN1 port) and every wired device of
192.168.0.x. Because it never appears, the Map (feature 010) cannot draw the real wiring: the
Huawei reports all those devices on LAN1, and the owner's "Connected via" links (010 User Story 5)
have no switch to point to.

This feature lets the owner add such a device by hand. A manual device is a device like any other
in the Devices list, on the Map and in "Connected via", except that scans never confirm it.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Add a device that scans cannot see (Priority: P1)

As the owner, I add the TL-SL1218P switch to the inventory by hand, with a name and the type
"switch", and it shows up in the Devices list.

**Why this priority**: Without the device in the inventory nothing else in this feature (or the
corrected map) is possible.

**Independent Test**: Add a manual device with a name and type, open the Devices list, and check
that it is there with its name, type and a "manual" marker, and that its device page opens.

**Acceptance Scenarios**:

1. **Given** the Devices list, **When** the owner chooses "Add device" and enters the name
   "TL-SL1218P", type "switch" and notes, and saves, **Then** the device page of the new device
   opens and the device is in the Devices list.
2. **Given** a manual device, **When** the owner looks at it in the Devices list, **Then** it is
   marked "manual" instead of an online / stale / offline status, and shows no IP address.
3. **Given** the add form, **When** the owner leaves the name empty, **Then** nothing is saved and
   the form says a name is required.
4. **Given** the add form, **When** the owner enters a MAC address that already belongs to a device
   in the inventory, **Then** nothing is saved and the form names the existing device.
5. **Given** the Devices list filters (for example by status or subnet), **When** the owner filters
   by online, stale or offline, **Then** manual devices are not in the result; with no filter they
   are.

---

### User Story 2 - Wire the manual device into the network (Priority: P1)

As the owner, I say that the switch connects via the Huawei router, and that my wired devices
connect via the switch, and the Map shows that wiring.

**Why this priority**: This is why the owner asked for the feature: a correct map of 192.168.0.x.

**Independent Test**: Set the manual switch "connected via" the router, set two devices "connected
via" the switch, open the Map and check the chain router → switch → devices.

**Acceptance Scenarios**:

1. **Given** a manual device, **When** the owner opens "Connected via" on any other device's page,
   **Then** the manual device is one of the choices, listed with routers, switches and access
   points first.
2. **Given** a manual device, **When** the owner sets its own "Connected via" to the Huawei router,
   **Then** the Map shows a link from the router to the manual device.
3. **Given** devices connected via the manual switch, **When** the Map is shown, **Then** they are
   drawn behind the switch, and the router-reported LAN1 port link of those devices is not drawn
   (as for any manual link, 010 FR-009).
4. **Given** a manual device connected via a device in 192.168.0.x, **When** the Map is shown,
   **Then** the manual device is drawn in the 192.168.0.x group; a manual device connected to
   nothing is drawn in the "Other" group.
5. **Given** the Map with "Online only", **When** a manual device has at least one online or stale
   device connected via it, **Then** it stays visible; otherwise it is hidden like an offline
   device.
6. **Given** a manual device on the Map, **When** the owner points at it or selects it, **Then** the
   tooltip and details say "manual" (added by hand, not seen by scans) instead of "last seen".

---

### User Story 3 - Edit and remove a manual device (Priority: P2)

As the owner, I can rename or retype a manual device, change its MAC and notes, and remove it when
the equipment is gone.

**Why this priority**: Mistakes and changes happen, but adding and wiring come first.

**Independent Test**: Edit a manual device's name and check the Devices list and Map show the new
name; remove it and check it disappears and devices that were connected via it lose that link.

**Acceptance Scenarios**:

1. **Given** a manual device, **When** the owner changes its name, type, MAC or notes on its device
   page and saves, **Then** the change shows everywhere it is listed.
2. **Given** a manual device, **When** the owner chooses "Remove" and confirms, **Then** it is gone
   from the Devices list, the Map and the "Connected via" choices.
3. **Given** devices connected via a removed manual device, **When** the Map is shown, **Then**
   their manual link is gone and they are drawn from scan data again (for example on the router's
   LAN1 port), and their "Connected via" shows "not set".
4. **Given** the owner opens "Remove" but does not confirm, **Then** nothing is removed.
5. **Given** a device found by scans, **When** the owner opens its page, **Then** there is no
   "Remove" action (removing scanned devices is out of scope).

### Edge Cases

- **A scan later sees the manual device's MAC** (for example the owner replaces the unmanaged
  switch with a managed one, or entered the MAC of a device that was simply offline): the manual
  device and the scanned one become one device, keeping the owner's name, type, notes and links;
  from then on it has a normal status. The owner can still undo it like any merge.
- **"Connected via" loops** (the switch via a device that is itself connected via the switch): the
  page refuses the link that would close the loop and says why.
- **Rebuild of the inventory from stored history** (Principle V): manual devices and their links
  are owner facts and are kept, like names and notes.
- **"Remove all devices" and "Drop all data"** (feature 004): manual devices are owner edits and are
  removed with the other edits; the confirmation counts include them.
- **Home page counts** (online / stale / offline): manual devices are not counted in any of them;
  the total device count does include them and says how many are manual.
- **A MAC address in an unusual form** (dashes, upper case, no separators): it is accepted and
  stored in the same form as scanned MAC addresses; anything that is not a MAC address is refused.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The owner MUST be able to add a device by hand with a name (required), a type (one of
  the existing device types, optional), a MAC address (optional) and notes (optional). No IP
  address is entered or required.
- **FR-002**: A manual device MUST appear in the unfiltered Devices list, with a "manual" marker in
  place of a status and no address, and MUST have a device page like any other device.
- **FR-003**: A manual device MUST NOT match the online, stale or offline filters and MUST NOT be
  counted in the online / stale / offline counts; it MUST be counted in the total.
- **FR-004**: A manual device MUST be selectable in "Connected via" on other devices' pages, and
  MUST itself have a "Connected via" setting; routers, switches and access points MUST be listed
  first among the choices.
- **FR-005**: The Map MUST show every manual device exactly once, in the group of the device it is
  connected via (following the chain if that device is itself manual), or in "Other" when it is
  connected to nothing on the map; it MUST be drawn so that it is distinguishable from scanned
  devices and say "manual" in its tooltip and details.
- **FR-006**: Under "Online only" a manual device MUST stay visible when at least one online or
  stale device is connected via it, directly or through other manual devices; otherwise it MUST be
  hidden.
- **FR-007**: The owner MUST be able to edit a manual device's name, type, MAC and notes, and to
  remove it after an explicit confirmation; removing it MUST also remove the "Connected via" links
  that point to it.
- **FR-008**: The system MUST refuse a MAC address that is malformed or that already belongs to
  another device, naming that device.
- **FR-009**: The system MUST refuse a "Connected via" link that would make a loop.
- **FR-010**: When a scan or router read sees a device whose MAC equals a manual device's MAC, the
  two MUST become one device that keeps the owner's name, type, notes and links.
- **FR-011**: Manual devices and their links MUST be kept by a rebuild of the inventory from
  history, and MUST be removed by "Remove all devices" and "Drop all data" (feature 004), which
  MUST count them in their confirmation message.
- **FR-012**: Scanned devices MUST NOT gain a "Remove" action through this feature.

### Key Entities

- **Manual device**: a device the owner added by hand: name, type, optional MAC, notes, when it was
  added; never has an IP address or a scan status; may be connected via another device and may
  have devices connected via it.
- **Connected via link** (existing, feature 010): from a device to the device it connects through;
  now either end may be a manual device.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: The owner can add the TL-SL1218P switch, connect it via the Huawei router and connect
  one wired device via it in under 2 minutes, without leaving the web UI.
- **SC-002**: After the owner wires the switch and the wired devices of 192.168.0.x, the Map shows
  Huawei → switch → each wired device, and no wired device is drawn on the router's LAN1 port.
- **SC-003**: Manual devices and their links are still present and unchanged after 3 later scans
  and after a rebuild of the inventory.
- **SC-004**: The device total on the home page equals the unfiltered Devices list count, and the
  online + stale + offline counts plus the manual count equal that total.

## Assumptions

- Manual devices are for equipment without an IP address that scans cannot see (unmanaged
  switches, passive hubs, powerline adapters, …); a device with an IP address is found by scans.
- Device types are the existing list (router, access point, switch, computer, …); no new type is
  added.
- There is one owner and no login (feature 007); anyone on the home network who can open the UI can
  add, edit and remove manual devices, as with names and notes today.
- Which port of the switch a device is plugged into is not recorded; "connected via the switch" is
  enough for the map.
- Reading client lists from access points (the owner's TP-Link EAP225s) is a separate feature
  (012); this feature only makes manual wiring possible.
- Removing scanned devices one by one is out of scope.
