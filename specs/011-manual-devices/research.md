# Research: Manual Devices

No technique needs confirming on a real network: manual devices never touch the network
(Principle IV). The decisions below are about storage, identity and the views.

## R1. A manual device is an owner fact, replayed like names and notes

**Decision**: A new append-only fact table `user_manual_devices` (`identity_key`, `field` in
`add` / `mac` / `remove`, `value`, `at`). `add` creates the projection row, `mac` sets or clears
its MAC, `remove` deletes it. Name, type and notes stay in `user_device_attrs` under the manual
device's identity key, so the existing edit form and code apply unchanged. `loadUserFacts` merges
the new table into the replay order (Principle V, FR-011).

**Rationale**: Rebuild reproduces projections from runs plus user facts. A manual device has no
run, so it must be a fact or a rebuild would lose it (SC-003).

**Alternatives rejected**: a row in `devices` only (lost by every rebuild); a fake collection run
carrying the device (pollutes run history and freshness).

## R2. Identity key `manual:<uuid>`, MAC stored as an attribute

**Decision**: The identity key is `manual:` plus a random UUID generated when the owner adds the
device; it never changes. The MAC (optional, normalized like scanned MACs) is the `mac` column of
the projection row, set from the `mac` facts, with the vendor from the OUI table.

**Rationale**: Facts (names, links) are keyed by identity key. A MAC-based key (`mac:…`) would
change when the owner edits the MAC, orphaning those facts.

## R3. A scan that sees the manual device's MAC merges it (FR-010)

**Decision**: When the applier creates or updates a device with MAC m and a manual device has MAC
m, the manual device is merged into the scanned one with the existing `mergeInto` (name, type and
notes move over when the scanned device has none; links resolve through `merged_into`). The
scanned device then has a normal status. Because adding refuses a MAC that is already in the
inventory (FR-008), the scanned device is new at that moment and nothing of the owner's is lost.

**Spec correction**: the spec's edge case says the owner "can still undo it like any merge". The
product has no merge undo (merges are automatic, feature 001 R6). The plan drops that clause
rather than build an undo; the spec is updated.

## R4. Projection row: `devices.manual = 1`, no new status

**Decision**: Migration 0009 adds `devices.manual INTEGER NOT NULL DEFAULT 0`. A manual row has
status `online` in storage (inside the existing CHECK), `first_seen = last_seen =` the add time,
no addresses and no sightings. The shown state is a new `StateManual` (`"manual"`), set by the
state evaluation before anything else, so filters, counts and the Map treat it apart (FR-002,
FR-003). `evaluateOffline` only touches devices with addresses on the scanned subnet, so it never
changes a manual row.

**Alternatives rejected**: a new `status` value (rebuilding the `devices` table to change its
CHECK, for no gain since display goes through the shown state).

## R5. Removal clears the links that point to it

**Decision**: Removing a manual device writes the `remove` fact and, in the same transaction, a
`ClearUserLink` fact for every device whose current link points to it (FR-007, US3-3). Its own
outgoing link needs nothing: links from an unknown device are skipped. Only manual devices have a
remove route (FR-012).

## R6. Loops are refused when a link is saved (FR-009)

**Decision**: `POST /devices/{id}/link` follows the chain from the chosen target through the
current links (at most the number of devices); if it reaches `id`, the page answers 400 "this
would make a loop: <target> connects via <this device>". This applies to every link, not only
manual devices: a loop is never meaningful.

## R7. Map placement and "Online only"

**Decision**: A manual device's group is the group of the device it connects via, following the
chain through other manual devices; "Other" when the chain ends nowhere (FR-005). The node has
`state: "manual"` and `last_seen_ago: ""`; the JS model gives it the class `manual` (dashed
outline) and says "manual (added by hand)" in the tooltip and details. Under "Online only" a
manual node stays when an online or stale device connects via it, directly or through manual
devices (FR-006); `map-model.js` computes this from the manual edges (JSON shape unchanged).

## R8. Lists and counts

**Decision**: The Devices list shows a "manual" badge in the status column and "—" for the
address; the status filters online/stale/offline never match a manual device, nor does the subnet
filter. `Counts` gains `Manual`; the home page shows "N devices (M added by hand)" and online +
stale + offline + manual = total (SC-004). "Connected via" options rank routers, switches, access
points, then the rest (FR-004).

## R9. Maintenance

**Decision**: "Remove all devices" and "Drop all data" also delete `user_manual_devices`; the
result names how many of the removed devices were added by hand (FR-011).
