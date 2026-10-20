# Feature Specification: Device Status Freshness

**Feature Branch**: `006-device-status-freshness`

**Created**: 2026-10-19

**Status**: Draft

**Input**: User description: "On Device page Status column should reflect true: green online - "Last seen" less than internal scan frequency from now, gray online - on lates scan device was online but "Last seen" was is far from internal scan frequency from now, offline - on latest scna device was offline"

## Context

Today a device's status changes only when a scan is uploaded: it becomes offline after several
missed scans (default 3) and stays "online" forever when nobody scans its subnet any more. On the
owner's network the desktop collector ran only by hand, so `192.168.0.0/24` devices showed
"online" with a "Last seen" hours old. The owner wants the status to tell the truth at the moment
the page is viewed.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Status shows whether "online" is current (Priority: P1)

As the owner, when I look at the Devices list or a device's page, I want to see at a glance
whether a device is online right now (seen within one scan interval), or was online at the last
scan but that scan is old, so I don't trust stale data.

**Why this priority**: it is the core of the request; a stale "online" is the misleading case
found on the owner's network.

**Independent Test**: upload a scan that sees a device, then view the Devices page at once (green
"online") and again more than one scan interval later with no new scan (gray "online").

**Acceptance Scenarios**:

1. **Given** a device whose subnet's latest scan saw it, **When** the page is viewed less than one
   scan interval after it was last seen, **Then** its status is a green "online".
2. **Given** the same device and no newer scan of its subnet, **When** the page is viewed more than
   one scan interval after it was last seen, **Then** its status is a gray "online" with a hint
   that the information is old (e.g. "last scan 2 h ago").
3. **Given** a gray "online" device, **When** a new scan sees it, **Then** it is green again.

---

### User Story 2 - Offline means the latest scan did not see it (Priority: P1)

As the owner, I want a device to show "offline" when the latest scan of its subnet did not find
it, so the list matches what the network looked like at that scan.

**Why this priority**: together with US1 this makes the three states the owner described.

**Independent Test**: upload a complete scan that sees a device, then a later complete scan of the
same subnet that does not: the device shows "offline".

**Acceptance Scenarios**:

1. **Given** a device seen by an earlier scan, **When** the latest complete scan of its subnet does
   not see it, **Then** its status is "offline" at once (one missed complete scan is enough).
2. **Given** an "offline" device, **When** a later scan sees it, **Then** it is "online" (green).
3. **Given** the latest scan of the subnet was skipped or incomplete (ignored subnet, faked
   answers, ping fallback after a failed router read), **Then** it does not make devices offline;
   the latest complete scan decides.

---

### User Story 3 - Filters and counts follow the shown status (Priority: P2)

As the owner, I want the status filter on the Devices page and the online/offline counts on the
home page to use the same three states, so numbers match what I see in the list.

**Independent Test**: with one green, one gray and one offline device, the home page counts and
the Devices filter each give the matching device.

**Acceptance Scenarios**:

1. **Given** devices in all three states, **When** the owner filters by status, **Then** the
   choices are "online", "online (stale)" and "offline" and each returns the matching devices.
2. **Given** the same devices, **Then** the home page shows separate counts for online, stale and
   offline.
3. The unused statuses "new" and "new_offline" no longer appear as filter choices.

### Edge Cases

- **Several collectors scan the same subnet**: the latest complete scan by any of them decides
  offline; the interval is that of the collector whose scan is the latest.
- **Device with addresses in several subnets**: it is online if any of its current addresses'
  subnets saw it in their latest complete scan; the freshest "Last seen" decides green or gray.
- **Subnet nobody scans any more** (collector stopped, removed, or paused): its devices keep the
  result of the last complete scan and turn gray when "Last seen" is older than two intervals;
  nothing is guessed.
- **Clock differences**: "Last seen" is the server-corrected scan time, so a collector's wrong
  clock (the desktop's is 4.5 min off) does not change the state.
- **Boundary**: a scan takes some seconds and uploads after it finishes, so "Last seen" is about
  one interval old just before the next scan arrives. Green therefore lasts up to two scan
  intervals; a device turns gray only when a scan is clearly late or missing.
- **Merged devices** (identity merges) are not listed, as today.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The status shown for a device MUST be one of: "online" (green), "online (stale)"
  (gray), "offline".
- **FR-002**: A device MUST be shown "online" (green) when the latest complete scan of one of its
  subnets saw it and its "Last seen" is within the freshness threshold (FR-005) of the moment the
  page is viewed.
- **FR-002a**: The freshness threshold MUST be two scan intervals (e.g. 30 minutes for a 15-minute
  interval).
- **FR-003**: A device MUST be shown "online (stale)" (gray) when the latest complete scan of its
  subnet saw it but its "Last seen" is older than the freshness threshold; the page MUST say how
  old the information is.
- **FR-004**: A device MUST be shown "offline" when the latest complete scan of every subnet it has
  a current address in did not see it: one missed complete scan is enough.
- **FR-005**: The freshness threshold (FR-002a) MUST be based on the scan interval of the collector whose
  scan last covered the device's subnet (for the NAS built-in collector, the interval in Settings).
  If none of the device's subnets has a complete scan, the built-in scan interval from Settings
  is used.
- **FR-006**: The state MUST be evaluated when the page is viewed, so a gray state appears without
  any new scan.
- **FR-007**: The Devices list, the device page, the Devices status filter and the home page
  counts MUST use the same states.
- **FR-008**: Device history (came online / went offline events) MUST remain reproducible from the
  stored scans (constitution, Principle V); the time-based gray state is a view of the stored
  data, not a new stored fact. History follows the same offline rule: a device "went offline" at
  the first complete scan that missed it.
- **FR-009**: Status MUST stay readable without color (text plus color), as today.
- **FR-010**: The "Mark a device offline after this many missed scans" setting MUST be removed
  (one missed scan is the rule); an already stored value is ignored. The home page "stale" badge
  for a subnet uses the same freshness rule: no complete scan of it within two of the scanning
  collector's intervals.

### Key Entities

- **Device status (shown)**: online / online (stale) / offline, derived from the device's latest
  observation, the latest complete scans of its subnets, those scans' collector interval, and the
  current time.

## Success Criteria *(mandatory)*

- **SC-001**: A device not seen for more than two scan intervals never shows a green "online"
  (was: shown "online" indefinitely).
- **SC-002**: Within one scan of a device leaving the network, it shows "offline", and within one
  scan of it coming back it shows green "online".
- **SC-003**: The online/stale/offline counts on the home page always equal the number of devices
  with that status in the Devices list.

## Assumptions

- "Internal scan frequency" means the scan interval of the collector that scanned the device's
  subnet (the built-in collector's interval from Settings, a remote collector's configured
  interval as it reports it in each scan).
- Only complete scans decide offline, as today (skipped or incomplete scans never do).
- Gray is used for the stale state; "offline" gets its own, clearly different style (today offline
  is gray, so it changes).
- No change to collectors or the upload contract.
- Devices that answer only now and then (sleeping phones) will flip between online and offline
  more often; the owner accepted this (clarification 2026-10-19).

## Clarifications

### Session 2026-10-19

- Q: When does a device become offline? → A: as soon as the latest complete scan of its subnet
  misses it (one missed scan); the N-missed-scans setting goes away.
- Q: When does green turn gray? → A: when "Last seen" is older than two scan intervals.
