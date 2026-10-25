# Implementation Plan: Map Tree Layout

**Branch**: `014-map-tree-layout` | **Date**: 2026-10-25 | **Spec**: [spec.md](spec.md)

## Summary

Replace the Map's Cytoscape drawing and hand-placed positions with a tree built from the existing
`/ui/map.json` (router ports, Wi-Fi, "Connected via", dotted "wiring unknown" under the gateway),
laid out top-down by ELK's layered algorithm (elkjs, vendored) and drawn as plain SVG. A prototype on
the owner's real data (2026-10-25) confirmed the approach.

## Technical Context

**Language/Version**: JavaScript (ES modules, browser + Node ≥ 20 for tests); Go ≥ 1.26 (page).
**Primary Dependencies**: elkjs 0.9.3 (`lib/elk.bundled.js`, EPL-2.0) vendored in
`internal/web/static/vendor`; Cytoscape removed.
**Storage**: none.
**Testing**: `node --test internal/web/jstest/*.test.mjs` (pure model, plus real ELK layouts in Node:
levels, no overlap, groups contain their nodes, 250-device time); Go page test.
**Target Platform**: desktop browser on the LAN.
**Project Type**: existing Go monorepo; web page only.
**Performance Goals**: 250 devices laid out in < 2 s (SC-002).
**Constraints**: no CDN (Principle I); `/ui/map.json` unchanged; values rendered as text, never HTML.
**Scale/Scope**: ~40 devices today, 250 tested.
**Version**: `0.15`.

## Constitution Check

| Principle | Result |
|-----------|--------|
| I. Local-only | ✅ elkjs is vendored and served by the server; no runtime fetch elsewhere |
| II. Test-first | ✅ JS model and ELK layout tests first; Go page test first |
| III. Simplicity | ✅ One dependency replaced by one (Complexity Tracking); SVG instead of a canvas framework |
| IV. Distributed collection | ✅ Not touched |
| V. Faithful history | ✅ Not touched (view only) |

**Result**: PASS.

## Research

- **R1 Layout engine**: ELK layered (direction DOWN, orthogonal edges, `INCLUDE_CHILDREN` for subnet
  boxes). Rejected: Cytoscape + ELK adapter (keeps Cytoscape's hard-to-read look), Graphviz WASM
  (2–3 MB, harder interaction), D3 tidy tree (no boxes or cross-subnet links), vis-network
  hierarchical (worse layouts).
- **R2 Tree**: parent → child edges: port edges as they are; manual edges reversed; a dotted edge
  from the group's gateway to each device with no parent, skipped when the device is an ancestor of
  the gateway (loop).
- **R3 Leaf blocks**: three or more childless devices of one parent become one ELK node (a block) of
  2 columns (3 above 6); members are placed in the block's grid after layout.
- **R4 Rendering**: SVG built from strings with every value escaped; click/hover by event delegation
  on `data-id`; pan/zoom by a transform on one group; Fit recomputes it.
- **R6 Subnet boxes**: each subnet is laid out as its own layered tree; the boxes are shelf-packed
  (tallest first, rows up to a width that matches the screen's aspect ratio). ELK's own packing
  (INCLUDE_CHILDREN layered, rectpacking with any optimisation goal) put the owner's three subnets in
  one wide row or one column. A "connected via" across subnets is drawn straight.
- **R5 Online only**: the existing `visibleIds` decides what is shown; the tree is laid out again on
  the visible part.

## Project Structure

```text
internal/web/static/map-model.js        tooltip, visibleIds, details (kept); tree, elkGraph, place,
                                        nodeClass (new); toElements, layout (removed)
internal/web/static/map.js              ELK + SVG rendering, panel, tip, pan/zoom/Fit
internal/web/static/vendor/elk.bundled.js  new (elkjs 0.9.3); cytoscape.min.js removed; VERSIONS.txt
internal/web/templates/map.html         scripts, Fit button, SVG container
internal/web/static/app.css             map SVG styles
internal/web/jstest/map-model.test.mjs  model + ELK layout tests
internal/web/map_test.go                page scripts
VERSION                                 0.15
```

## Complexity Tracking

| Addition | Why needed | Simpler alternative rejected because |
|----------|------------|--------------------------------------|
| elkjs (1.6 MB, replaces Cytoscape 435 KB) | Layered tree layout with nested subnet boxes is the feature | Hand-written layout (the 010 revision) is what the owner rejected; other engines in R1 |
