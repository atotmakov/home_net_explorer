# Feature Specification: Map Tree Layout

**Feature Branch**: `014-map-tree-layout`

**Created**: 2026-10-25

**Status**: In progress

**Input**: User description: "the visualisation tool we use for Map the most ugliest i've ever
seen, lets find the better one, which draw trees more careful, with respect levels and so" — after
a prototype on the real network data: "yes, turn it to the feature implement and deploy".

## Context

The Map (feature 010) draws devices with hand-placed positions: routers on a top row, one column
per router port, everything else in a grid. It ignores the owner's "Connected via" links (011), so a
switch added by hand sits beside the router with crossing lines; busy ports become one long column;
labels are tiny; a subnet with no router read is a plain grid. The owner wants a drawing that shows
the network as a tree with clear levels.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - See the network as a tree (Priority: P1)

As the owner, I open the Map and see each subnet as a tree: the gateway on top, then what connects
to it (router ports, switches, access points), then the devices behind each of them, level by level.

**Independent Test**: With a router, a switch connected via the router, two access points connected
via the switch and phones under the access points, every child is drawn below its parent, and no two
devices overlap.

**Acceptance Scenarios**:

1. **Given** devices with a parent (a router port, an access point's Wi-Fi, or "Connected via"),
   **When** the Map is shown, **Then** each is drawn below its parent, one level per hop, with the
   connecting line going down from the parent.
2. **Given** a port, Wi-Fi network or device with many devices directly under it (three or more
   with nothing under them), **When** the Map is shown, **Then** they are packed in a compact block
   under it (two or three columns) instead of one long row or column.
3. **Given** devices of a subnet with no known parent, **When** the subnet has a gateway, **Then**
   they hang under the gateway with a dotted line ("same subnet, wiring unknown").
4. **Given** each device, **When** the Map is shown, **Then** its box shows its name and its address
   (or "added by hand"), readable without zooming at the default size, with the state shown by colour
   (online, stale, offline dimmed, manual dashed) and gateways outlined.
5. **Given** a gateway of another subnet (two-subnet devices were removed 2026-10-25), **When** the Map is shown,
   **Then** a dashed line goes from it to the other subnet's box.

---

### User Story 2 - Use the map as before (Priority: P1)

As the owner, I keep everything the Map already does: "All devices" / "Online only", the details
panel on click, the hover text, moving and zooming.

**Independent Test**: Toggle "Online only", click a device, hover a device, drag and zoom.

**Acceptance Scenarios**:

1. **Given** "Online only", **When** chosen, **Then** offline devices disappear and the tree is laid
   out again without gaps (manual devices follow the 011 rule).
2. **Given** a device, **When** clicked, **Then** the details panel opens as before; clicking the
   background closes it.
3. **Given** the map, **When** the owner drags or uses the mouse wheel, **Then** it moves and zooms;
   a "Fit" button brings the whole map back into view.

### Edge Cases

- No devices: the "No devices yet" notice, as today.
- A "Connected via" link from the gateway to one of its own subnet's devices: no dotted line is added
  that would close a loop.
- A port or Wi-Fi network whose devices are all hidden ("Online only") is hidden too.
- 250 devices (010 SC): the layout finishes in under 2 seconds in the browser.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The Map MUST lay out each subnet as a top-down tree: parent above child, levels by hop
  (router → port/Wi-Fi → device; "Connected via" target → device).
- **FR-002**: Three or more childless devices under the same parent MUST be packed in one compact
  block (2 columns, 3 when more than 6).
- **FR-003**: Devices with no known parent in a subnet with a gateway MUST hang under the gateway with
  a dotted line, unless that would make a loop.
- **FR-004**: Device boxes MUST show name and address (or "added by hand"), state by colour, gateways
  outlined, manual devices dashed, offline dimmed, last-known lines dotted.
- **FR-005**: Gateways of other subnets (two-subnet devices until 2026-10-25) MUST be joined to the other subnet's
  box by a dashed line.
- **FR-006**: "All devices"/"Online only", the details panel, the hover text, pan, zoom and a Fit
  button MUST work.
- **FR-007**: All drawing code and libraries MUST be served by the server (no CDN, Principle I); the
  map data (`/ui/map.json`) is unchanged.

## Success Criteria *(mandatory)*

- **SC-001**: On the owner's network, every device with a known parent is drawn below it, and no two
  boxes overlap.
- **SC-002**: The 250-device test map lays out in under 2 seconds in a desktop browser.
- **SC-003**: The owner judges the new Map readable without zooming at the default size.

## Assumptions

- The map data from the server (feature 010, 011, 012) is enough; only the page changes.
- The layout library is ELK (elkjs, EPL-2.0), vendored like Cytoscape was; Cytoscape is removed.
