# Feature Specification: Access Point Client Lists

**Feature Branch**: `012-access-point-clients`

**Created**: 2026-10-24

**Status**: Draft

**Input**: User description: "Lets inspec wifi points itself to get the device list connected to
them, looks like new feature. Lets make it possible to mark devices as wifi point with login and
pass, like with router, and grab the devices, looks like new feature."

## Context

All Wi-Fi devices of the owner's 192.168.0.x network connect through one of two TP-Link EAP225
access points (192.168.0.5 and 192.168.0.10). The Huawei router only sees them behind its LAN1
port, and scans cannot tell which access point a phone uses, so the Map (feature 010) cannot draw
the Wi-Fi side of the network. Each EAP225 knows its own clients and shows them in its web
interface behind a login.

This feature does for access points what features 002 and 003 do for routers: the owner sets an
access point's model and login on its device page, collectors read the access point's client list
at each scan, and the server records through which access point and Wi-Fi network each device is
connected.

Router-read decisions of feature 003 apply unchanged: logins are stored on the server **in plain
text**, every collector receives every configured access point, collectors fetch the login before
each read and keep it in memory only, and a rejected login is not retried until the settings
change or the owner runs the collector's check command.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Configure an access point from its device page (Priority: P1)

As the owner, I open an EAP225's device page, set its type to "access point", pick its model and
enter its username and password, the same way I set up the Huawei router.

**Why this priority**: Without a configured access point nothing can be read.

**Independent Test**: On a device page set type "access point", choose the EAP225 model, enter a
username and password, save; reopen the page and check that model, address and username are shown
and the password only as "set".

**Acceptance Scenarios**:

1. **Given** a device whose type is "access point", **When** the owner opens its page, **Then**
   access point settings are offered: a model from the supported list, a username, a password, and
   the address taken from the device's current address.
2. **Given** saved settings, **When** any page is opened, **Then** the password is never shown,
   only that it is set; the owner can replace or clear it.
3. **Given** saved settings, **When** the owner clears them or changes the device type away from
   "access point", **Then** the access point is no longer read.
4. **Given** a device whose type is not "access point", **When** its page is opened, **Then** no
   access point settings are offered.

---

### User Story 2 - Read which devices use each access point (Priority: P1)

As the owner, I want each scan to ask the access points which devices are connected to them and on
which Wi-Fi network and band, so the inventory knows where each Wi-Fi device is.

**Why this priority**: This is the data the owner asked for.

**Independent Test**: With a configured access point (a test stand-in in automated tests) that
reports three clients, run a scan and check the three devices show "via <access point> ·
<Wi-Fi name> (<band>)" on their device pages.

**Acceptance Scenarios**:

1. **Given** a configured access point, **When** a collector scans, **Then** it reads the access
   point's current clients and reports each with its MAC address, IP address when the access point
   knows it, Wi-Fi network name and band.
2. **Given** a client whose MAC is a known device, **When** the read arrives, **Then** that device
   shows the access point and Wi-Fi network it is connected through, and counts as seen at that
   time.
3. **Given** a client with an IP address in a known subnet but an unknown MAC, **When** the read
   arrives, **Then** a new device is recorded, as router reads do.
4. **Given** a client with no IP address and an unknown MAC, **When** the read arrives, **Then** no
   device is created from it (a MAC alone is not enough to name a device), and the count of such
   clients is shown with the read outcome.
5. **Given** a device that moved from one access point to the other, **When** the next read
   arrives, **Then** the device shows the access point it uses now.
6. **Given** a read that fails (unreachable, login rejected, page not understood), **When** the scan
   finishes, **Then** the rest of the scan is complete, and the Collectors page and the collector's
   check command show the failure and its reason for that access point.

---

### User Story 3 - Wi-Fi devices on the Map (Priority: P1)

As the owner, I open the Map and see each Wi-Fi device behind the access point it uses, grouped by
Wi-Fi network, without setting "Connected via" by hand.

**Why this priority**: The corrected map is the goal of the investigation that led to this
feature.

**Independent Test**: After a read that puts two devices on access point A and one on access point
B, open the Map and check each device hangs off the right access point, and none of them off the
router's LAN1 port.

**Acceptance Scenarios**:

1. **Given** devices reported by an access point, **When** the Map is shown, **Then** each is linked
   to that access point through its Wi-Fi network and band (for example "Home (5 GHz)"), like the
   router's LAN / Wi-Fi ports in feature 010.
2. **Given** a device reported both by the router (on LAN1) and by an access point, **When** the Map
   is shown, **Then** only the access point connection is drawn.
3. **Given** an offline device last reported by an access point, **When** the Map is shown, **Then**
   it hangs off that access point with a "last known" link.
4. **Given** a device with a manual "Connected via" link, **When** the Map is shown, **Then** the
   manual link wins over the access point report (010 FR-009).
5. **Given** an access point that is itself connected via a manual device (feature 011, for
   example the switch), **When** the Map is shown, **Then** the chain router → switch → access point
   → Wi-Fi devices is drawn.

### Edge Cases

- **An access point read is not a full scan of the subnet**: a device missing from an access
  point's list may be wired, asleep or on the other access point, so a read never makes a device
  offline and never counts as a completed scan of the subnet (unlike router reads, 002 FR-009).
- **Two access points broadcast the same Wi-Fi name**: the connection names the access point and
  the network, so the two are not confused.
- **A device appears on both access points in the same scan** (roaming during the scan): the
  report of the access point with the higher address is kept, so the result does not depend on
  timing; the next scan corrects it.
- **Phones with private (random) MAC addresses**: a phone keeps one private MAC per Wi-Fi network,
  so it is matched as usual; if it changes its private MAC it shows up as a new device, as with
  scans today.
- **The access point changes its own address**: the read uses the device's current address
  (as router settings do, 003 FR-002).
- **Many failed logins**: after a rejected login the access point is not retried until its settings
  change or the owner runs check, so the access point never locks the owner out.
- **Access points managed by a central controller** (TP-Link Omada) have no client list of their
  own; that setup is out of scope, and the read fails with a reason that says so when it is
  detected.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The device page MUST offer access point settings for a device whose type is "access
  point": a model from the supported list, a username and a password; the address MUST be one of
  the device's current addresses.
- **FR-002**: The first supported model MUST be the TP-Link EAP225 in standalone mode.
- **FR-003**: Access point settings MUST be stored, shown, changed, cleared and delivered to
  collectors as router settings are in feature 003 (FR-003 to FR-006, FR-008 to FR-012, FR-014,
  FR-015 there): the password in plain text on the server, never displayed after saving, fetched
  by active collectors before each read and kept in memory only, and every collector receives
  every configured access point. Access points are configured only in the web UI: they are not
  listed in, and cannot be added through, `hne-collector.yaml` (003 FR-007 and FR-013 do not
  apply).
- **FR-004**: Access point reads MUST be read-only: the collector MUST NOT change any access point
  setting.
- **FR-005**: The access point's address MUST be a private (RFC 1918) address.
- **FR-006**: At each scan a collector MUST read the current client list of each configured access
  point it can reach and report, per client, the MAC address, the IP address when known, the Wi-Fi
  network name and the band.
- **FR-007**: A reported client MUST count as a sighting of the device with that MAC (or, for an
  unknown MAC with an IP address in a known subnet, a new device), with its connection recorded as
  the access point plus Wi-Fi network and band. Clients with neither a known MAC nor an IP address
  MUST NOT create devices.
- **FR-008**: An access point read MUST NOT make any device offline and MUST NOT count as a
  completed scan of a subnet.
- **FR-009**: If a read fails, the rest of the scan MUST complete; the Collectors page MUST show,
  per collector, the outcome of its latest read of each access point (as for routers, 002 FR-013),
  and the check command MUST report per access point: reachable or not, login accepted or not,
  number of clients read.
- **FR-010**: After a rejected login, a collector MUST NOT try that access point again until its
  settings change or the owner runs the check command.
- **FR-011**: The Map MUST link each device whose latest connection comes from an access point to
  that access point through its Wi-Fi network and band; for an offline device the last reported
  one is used and drawn as "last known".
- **FR-012**: For one device the Map MUST draw only one connection: the manual "Connected via"
  link if there is one, else the most recent report. When the router and an access point report
  the same device in one scan, the access point's report MUST be the one recorded (it says more:
  the router only sees the access point's port).
- **FR-013**: The device page MUST show the device's latest access point connection (access point,
  Wi-Fi network, band, when).

### Key Entities

- **Access point settings**: for a device of type "access point": model, username, password (plain
  text), address (one of the device's current addresses).
- **Access point read**: one collector's attempt to read one access point during a scan: when,
  outcome (ok, unreachable, login rejected, not understood, controller-managed), number of clients.
- **Wi-Fi connection**: for a device in a read: the access point, the Wi-Fi network name, the band,
  and when it was seen.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: The owner configures both EAP225s from their device pages in under 2 minutes each,
  without editing any file on a collector.
- **SC-002**: After the next scan, every Wi-Fi device that is connected at that time (today at
  least Pixel-9a, Pixel-10a and Kates-iPhone when they are home) shows the access point it uses,
  and on the Map none of them hangs off the router's LAN1 port.
- **SC-003**: Reading both access points adds no more than 10 seconds to a scan.
- **SC-004**: With the access point's correct login replaced by a wrong one, the access point is
  tried once and then not again until the setting changes or check is run (zero repeated login
  attempts).
- **SC-005**: A device that moves between the two access points shows the new one after the next
  scan.

## Assumptions

- Both EAP225s run in standalone mode and their web interface is reachable from the collector on
  the owner's desktop (192.168.0.x is reachable from it today, as the Huawei read shows).
- The second EAP225 (192.168.0.10) currently has no type; the owner sets it to "access point"
  before configuring it.
- Which switch port a device is plugged into is not read; unmanaged switches are added by hand
  (feature 011).
- Access points of other brands, and TP-Link Omada controllers, are later additions.
- No login on the web UI (feature 007); anyone on the home network can configure access points, as
  with routers today.
