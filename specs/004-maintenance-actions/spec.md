# Feature Specification: Maintenance Actions

**Feature Branch**: `004-maintenance-actions`

**Created**: 2026-10-10

**Status**: Draft

**Input**: User description: "Maintenance actions for testing and upkeep (builds on features 001-003). The owner, from the web UI (Settings page, a "Maintenance" section, owner session + existing CSRF/Origin protections, each destructive action needs an explicit confirmation step), can: (1) Remove all collectors: deletes every remote collector; their tokens stop working immediately (uploads, ping, router login get invalid_token); they disappear from the Collectors page and their names can be reused; the scan history they uploaded is KEPT, so devices they found stay in the inventory (owner decision). The NAS built-in collector is not removed. (2) Remove all devices: deletes all devices together with all scan history (all collectors' uploaded runs) and all owner edits (names, types, notes, links, acknowledgements, identity merges, router settings); collectors, the owner account, settings and subnets stay; devices come back fresh at the next scan. Must keep constitution Principle V: derived state stays reproducible from stored observations (a rebuild after removal must not bring devices back). (3) Pause / resume the built-in scanner: a Pause button stops the NAS built-in collector's periodic scans (and the manual Scan now for it) until the owner presses Resume; the pause is stored and survives server restarts and redeploys; the UI shows that the built-in scanner is paused. Remote collectors are unaffected. (4) Drop all data: deletes everything except the owner account and login session: devices, all history, all edits, all remote collectors, subnets, router settings, and settings return to defaults (including un-pausing); the built-in collector keeps existing. After any action the owner sees a confirmation message with what was removed (counts)."

## Context

While testing features 001–003 on the real network, the owner needs to start over without
deleting the server's data volume by hand: throw away test collectors, wipe the inventory and
its history, stop the NAS from scanning while something else is being tested, or reset
everything but their login. Until now the only way was to stop the container and delete the
database file, which also deletes the owner account.

**Decisions taken with the owner (2026-10-10)**:
- **Remove all devices** removes the devices, all scan history and all of the owner's edits
  (names, types, notes, links, acknowledgements, merges, router settings). Collectors, the owner
  account, settings and subnet names/ignore choices stay.
- **Remove all collectors** removes the remote collectors only; the NAS built-in collector stays.
  The scan history those collectors uploaded is **kept**, so the devices they found stay.
- **Pausing the built-in scanner** is stored and lasts until the owner resumes it, across
  server restarts and redeploys.
- **Drop all data** keeps the owner account and the owner's login; everything else goes.

These are deliberate, owner-initiated deletions with an explicit confirmation, not retention
limits or silent loss (constitution Principle V). After every action the stored history and the
inventory still agree: rebuilding the inventory from the stored history gives the same result.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Start the inventory over (Priority: P1)

As the owner, after testing collectors and routers I want an empty inventory again without
reinstalling: I remove all devices, and the next scans build the inventory from scratch.

**Why this priority**: It is the most frequent testing need: every change to discovery or
merging has to be checked against a clean inventory.

**Independent Test**: With devices and history present, remove all devices and confirm. The
Devices page is empty, the home page shows no devices, and after the next scan the devices
reappear with no names, types or old history.

**Acceptance Scenarios**:

1. **Given** an inventory with devices, history and owner edits, **When** the owner removes all
   devices and confirms, **Then** no devices, no device history and no owner edits remain, and a
   message says how many devices and how many scan records were removed.
2. **Given** the devices were removed, **When** the inventory is rebuilt from stored history,
   **Then** it is still empty (nothing comes back from old history).
3. **Given** the devices were removed, **When** any collector scans again, **Then** the devices
   it finds appear as new devices, without the old names, types, notes or router settings.
4. **Given** the devices were removed, **When** the owner looks at the Collectors and Settings
   pages, **Then** all collectors are still there and still work, the settings are unchanged and
   subnet names and ignore choices are kept.
5. **Given** the owner opens the action but does not confirm, **Then** nothing is removed.

---

### User Story 2 - Pause and resume the NAS scanner (Priority: P1)

As the owner, I want to stop the NAS's built-in scanner while I test a desktop collector alone,
and start it again later, without changing the server's configuration or redeploying.

**Why this priority**: It lets the owner isolate what a single collector sees, which most other
tests depend on; it is also non-destructive and cheap.

**Independent Test**: Pause the built-in scanner; wait longer than its scan interval: no new
built-in scan happens and "Scan now" is unavailable; restart the server: still paused; resume:
a scan happens at the next interval and "Scan now" works again.

**Acceptance Scenarios**:

1. **Given** the built-in scanner is running, **When** the owner pauses it, **Then** no
   scheduled built-in scan starts until it is resumed, and the home page and Settings show it as
   paused, with a Resume control instead of "Scan now".
2. **Given** a built-in scan is in progress, **When** the owner pauses, **Then** that scan is
   allowed to finish and its results are kept; no further scan starts.
3. **Given** the scanner is paused, **When** the server restarts or is redeployed, **Then** it
   is still paused.
4. **Given** the scanner is paused, **When** the owner resumes it, **Then** scans start again
   on the usual schedule and "Scan now" works.
5. **Given** the scanner is paused, **Then** remote collectors keep uploading and the server
   keeps accepting their results, and the Collectors page shows the built-in collector as
   paused rather than as failing.

---

### User Story 3 - Remove all remote collectors (Priority: P2)

As the owner, after testing with several temporary collectors I want to remove them all at once
so that only the collectors I really use remain, while keeping what they found.

**Why this priority**: Useful clean-up, but revoking collectors one by one is already possible.

**Independent Test**: With two remote collectors that have uploaded results, remove all
collectors and confirm. The Collectors page lists only the built-in collector; the old tokens are
refused; the devices they found are still in the inventory; a new collector can be created with
one of the old names.

**Acceptance Scenarios**:

1. **Given** remote collectors (active or revoked), **When** the owner removes all collectors and
   confirms, **Then** they are no longer listed on the Collectors page and a message says how
   many were removed.
2. **Given** removed collectors, **When** one of them uploads results, pings the server or asks
   for a router login, **Then** it is refused as an invalid collector, exactly as a revoked one.
3. **Given** removed collectors, **Then** the devices and history they produced are unchanged,
   and that history still shows which collector produced it (shown as removed).
4. **Given** removed collectors, **When** the owner creates a new collector with the same name as
   a removed one, **Then** it is created normally and is a different collector from the removed
   one.
5. **Given** the built-in collector exists, **Then** removing all collectors leaves it in place
   and scanning (or paused) as before.

---

### User Story 4 - Drop all data (Priority: P3)

As the owner, I want to reset the server to a fresh state without losing my login, so I can
repeat an end-to-end test from the beginning.

**Why this priority**: The most drastic action; it is a combination of the others plus settings
reset, and needed less often.

**Independent Test**: With devices, history, edits, remote collectors, router settings, renamed
or ignored subnets, changed settings and a paused scanner, drop all data and confirm. The owner
is still logged in; every page shows an empty server with default settings; the built-in
scanner runs again; old collector tokens are refused.

**Acceptance Scenarios**:

1. **Given** a server in use, **When** the owner drops all data and confirms, **Then** no
   devices, history, owner edits, remote collectors, router settings, subnets or subnet names
   remain, settings are back to their defaults, and the built-in scanner is not paused.
2. **Given** all data was dropped, **Then** the owner is still logged in, the owner's password
   is unchanged, and the built-in collector still exists.
3. **Given** all data was dropped, **When** the inventory is rebuilt from stored history,
   **Then** it is still empty.
4. **Given** the owner opens the action but does not confirm, **Then** nothing changes.

---

### Edge Cases

- **Results collected before a removal and uploaded after it**: a collector may hold results in
  its local queue (offline spool, feature 001) or a built-in scan may be running at the moment of
  removal. After "Remove all devices" or "Drop all data", results whose scan started before the
  removal are acknowledged to the collector (so it stops resending them) but discarded, so old
  devices don't come back.
- **Removed collector still running**: its uploads and pings are refused; it reports the same
  error as a revoked collector, and the owner has to create a new collector to use that machine.
- **Built-in collector disabled by server configuration** (feature 001 `--no-builtin-scan`): the
  pause control is not offered; the other actions work.
- **Pause while paused / resume while running**: the request changes nothing and says so.
- **Two removals at once** (e.g. two browser tabs): each runs completely or not at all; the
  second finds less (or nothing) to remove and reports its own counts.
- **Nothing to remove**: the action succeeds and reports zero.
- **Router lockout memory** (feature 003): removing devices or dropping data also forgets the
  built-in collector's rejected router logins together with the router settings.
- **Remote collectors' local files** (their configuration, router list cache and rejection
  marker): they are on other machines and are not changed; a removed collector can no longer
  get router settings from the server.

## Requirements *(mandatory)*

### Functional Requirements

**Common**

- **FR-001**: The Settings page MUST offer a "Maintenance" section with four actions: remove all
  devices, remove all collectors, pause/resume the built-in scanner, and drop all data.
- **FR-002**: Only the logged-in owner MUST be able to perform these actions, with the same
  cross-site request protections as every other owner change (feature 001).
- **FR-003**: Each destructive action (remove all devices, remove all collectors, drop all data)
  MUST require an explicit confirmation: the owner states what is removed and types a
  confirmation word specific to the action. A request without the correct confirmation MUST
  change nothing and say why.
- **FR-004**: Each destructive action MUST either complete entirely or change nothing (no
  partial removal on failure).
- **FR-005**: After each action the owner MUST see a message stating what was done, with counts
  of what was removed (devices, scan records, collectors, as applicable).
- **FR-006**: Each action MUST be written to the server log with its counts (no secrets).
- **FR-007**: After every action, rebuilding the inventory from the stored history MUST give the
  same inventory as before the rebuild (constitution Principle V).

**Remove all devices**

- **FR-010**: Removing all devices MUST remove all devices, their addresses, sightings, events
  and connections, all stored scan history from every collector, and all owner edits about
  devices: names, notes, types, manual connections, acknowledgements, merges and splits, and
  router settings (feature 003) including the built-in collector's remembered router rejections.
- **FR-011**: Removing all devices MUST keep: all collectors and their tokens, the owner account
  and sessions, the settings, the built-in scanner's paused/running state, and subnet names and
  ignore choices.
- **FR-012**: Results whose scan started before the removal and that arrive afterwards MUST be
  acknowledged to the sender and discarded (see Edge Cases).
- **FR-013**: Each collector's last report time MUST be kept; the Collectors page keeps working
  (the per-collector details that come from removed scan history, such as the latest router read,
  are empty until the next scan).

**Pause / resume the built-in scanner**

- **FR-020**: While paused, the built-in collector MUST NOT start scheduled scans, and "Scan now"
  MUST NOT start a scan (the control is replaced by Resume and the request is refused).
- **FR-021**: Pausing MUST let a scan in progress finish; its results are stored normally.
- **FR-022**: The paused state MUST be stored and MUST survive server restarts and redeploys.
- **FR-023**: The home page and the Settings page MUST show when the built-in scanner is paused.
- **FR-024**: While paused, the Collectors page MUST show the built-in collector as paused (not
  as failing). Devices seen only by it keep their last status, because the offline rule counts
  completed scans only (feature 001); subnets covered only by it MAY show as stale, which is
  accurate.
- **FR-025**: Resuming MUST restore scheduled scans at the configured interval; the first scan
  after resuming starts within one interval.
- **FR-026**: Pausing MUST NOT affect remote collectors or the acceptance of their results.

**Remove all collectors**

- **FR-030**: Removing all collectors MUST remove every remote collector, active or revoked; the
  built-in collector MUST be kept.
- **FR-031**: From the moment of removal, a removed collector's token MUST be refused for every
  collector request (upload, ping, router login) with the same error as a revoked token.
- **FR-032**: Removed collectors MUST NOT be listed on the Collectors page, and their names MUST
  be available for new collectors.
- **FR-033**: The scan history uploaded by removed collectors MUST be kept, the inventory MUST
  not change, and that history MUST still identify the collector it came from, shown as removed
  where collector names appear.

**Drop all data**

- **FR-040**: Dropping all data MUST do everything "remove all devices" does, plus: remove all
  remote collectors (as FR-030–FR-032, but including their history), remove all subnet names,
  ignore choices and the subnet list, reset every setting to its default, and un-pause the
  built-in scanner.
- **FR-041**: Dropping all data MUST keep the owner account, the owner's password and current
  login sessions, and the built-in collector.
- **FR-042**: After dropping all data the server MUST behave like a freshly set-up server with
  the same owner: the first scan rediscovers subnets and devices.

### Key Entities

- **Maintenance action**: one of remove all devices, remove all collectors, drop all data, pause,
  resume; performed by the owner at a point in time, with the counts of what it removed.
- **Reset point**: the time of the latest "remove all devices" or "drop all data"; results whose
  scan started before it are discarded on arrival.
- **Built-in scanner state**: running or paused, stored with the settings.
- **Removed collector**: a former remote collector that no longer appears or works, but whose
  past scan history (when kept) still names it.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: The owner can go from a used server to an empty inventory, without touching the
  NAS, the container or any file, in under 1 minute.
- **SC-002**: After "remove all devices" or "drop all data", 0 old devices reappear: neither from
  a rebuild nor from results collected before the removal and uploaded later.
- **SC-003**: While the built-in scanner is paused, 0 built-in scans start, including across a
  server restart; after resume, a scan starts within one scan interval.
- **SC-004**: After "remove all collectors", 100% of requests with a removed collector's token
  are refused, and the inventory is identical to before.
- **SC-005**: After "drop all data", the owner can still log in with the same password, and every
  page shows the same content as a freshly set-up server.
- **SC-006**: 0 changes happen when a destructive action is submitted without its confirmation,
  without the owner's session, or from another site.

## Assumptions

- Only the owner uses the web UI (single-owner app, feature 001); no roles or second approval.
- The actions are meant for testing and upkeep on a home network; there is no undo and no
  automatic backup. The owner can back up the data volume beforehand (feature 001 deployment).
- "Settings return to defaults" means the defaults the server was started with (e.g. the built-in
  scan interval from the server configuration), the same as on a fresh install.
- Removing a single device or a single collector (rather than all of them) is out of scope;
  revoking a single collector already exists (feature 001).
- Remote collectors' own files on other machines are not changed by any of these actions.
