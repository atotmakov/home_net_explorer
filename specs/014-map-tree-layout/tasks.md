---

description: "Task list for Map Tree Layout"
---

# Tasks: Map Tree Layout

**Input**: `specs/014-map-tree-layout/` (spec.md, plan.md)

**Tests**: REQUIRED (Principle II): tests first in the same PR, then the implementation; CI only.

## Phase 1: Setup

- [ ] T001 Vendor elkjs 0.9.3 `lib/elk.bundled.js` as `internal/web/static/vendor/elk.bundled.js`
  with its sha256 in `VERSIONS.txt`; `VERSION` 0.15

## Phase 2: User Story 1 - See the network as a tree (P1) 🎯 MVP

- [ ] T002 [P] [US1] Tests in `internal/web/jstest/map-model.test.mjs`: `tree` (port edges, manual
  edges reversed, dotted edges from the gateway for parentless devices, none that would loop, only
  visible elements); `elkGraph` (groups as compound nodes, ports and devices sized, leaf blocks of ≥3
  childless devices with 2/3 columns, one edge per block); `nodeClass` (state, gateway, manual,
  offline dim); real ELK layout in Node: every tree child below its parent, no two device boxes
  overlap, every node inside its group box, 250 devices in < 2 s
- [ ] T003 [P] [US1] Test in `internal/web/map_test.go` `TestMapPage`: the page loads
  `/static/vendor/elk.bundled.js` and `/static/map.js`, has a Fit button, no Cytoscape
- [ ] T004 [US1] Implement `tree`, `elkGraph`, `place`, `nodeClass` in
  `internal/web/static/map-model.js`; remove `toElements` and `layout`. Make T002 pass
- [ ] T005 [US1] Implement `internal/web/static/map.js` (ELK + SVG), `map.html`, `app.css`; remove
  `cytoscape.min.js`. Make T003 pass

## Phase 3: User Story 2 - Use the map as before (P1)

- [ ] T006 [US2] In `map.js`: "Online only" re-lays out the visible part; click → details panel,
  background click closes it; hover text; drag, wheel zoom, Fit (manual check on the NAS)

## Phase 4: Polish

- [ ] T007 Deploy 0.15 and check the owner's map (SC-001, SC-003); record the result here
