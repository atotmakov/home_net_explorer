<!--
Sync Impact Report
- Version change: 1.0.0 → 1.1.0 (MINOR: guidance materially expanded)
- Modified sections:
  - Deployment & Environment Constraints: the hard-coded subnet list is replaced with a
    subnet-agnostic rule (runtime discovery, private ranges only)
  - IV. Distributed Collection: rationale generalized (no specific subnets)
- Added sections: none
- Removed sections: none
- Deferred TODOs: none
-->

# Home Net Explorer Constitution

## Core Principles

### I. Local-Only & Private

- All collected data (devices, addresses, hostnames, topology, history) MUST stay on the
  user's home network. It MUST be stored only in the self-hosted server's local storage.
- The application MUST NOT send telemetry, analytics, crash reports, or any network data to
  third-party or cloud services, and MUST NOT require internet access to function.
- Runtime assets (UI scripts, fonts, icons, vendor lookup tables) MUST be bundled or served
  locally; no CDN fetches at runtime.
- Collector-to-server traffic MUST target only the configured local server and MUST be
  authenticated (e.g., a shared token), so that arbitrary LAN hosts cannot inject data.

Rationale: the inventory describes the user's private network; leaking it is the worst
failure this product can have.

### II. Test-First (NON-NEGOTIABLE)

- Tests MUST be written and observed failing before the implementation that makes them pass
  (Red → Green → Refactor).
- Unit tests MUST NOT touch the real network; scanning, name resolution, and clock/time
  sources MUST be behind interfaces that tests replace with fakes or recorded fixtures.
- The collector upload API MUST have contract tests on both the collector and server sides.
- A change is not done until its tests pass in the automated test run.

Rationale: network behavior is nondeterministic; isolating it is the only way to get fast,
reliable tests and safe refactoring.

### III. Simplicity / YAGNI

- Build the simplest thing that satisfies the current spec; no speculative features,
  plugin systems, or abstractions without a present need.
- Each new runtime dependency or deployable component MUST be justified in the plan's
  Complexity Tracking section.
- Prefer one server process and one embedded/local datastore over multiple services unless
  a spec requirement demands otherwise.

Rationale: a home tool maintained by one person must stay small enough to understand
and fix.

### IV. Distributed Collection, Central Inventory

- The system has two roles: a **server** (web UI + API + datastore) and one or more
  **collectors** that observe the network from wherever they run.
- No single host can see every subnet, so collection MUST NOT assume the server's own
  vantage point is sufficient. Collectors on other hosts (e.g., the desktop that can see
  both subnets) MUST be able to submit observations to the server.
- Collectors MUST be stateless with respect to the inventory: they report observations
  (what was seen, from where, when). The server alone merges observations into
  devices and topology.
- The collector→server upload format MUST be versioned and documented. Breaking changes
  require a version bump and server support for the previous version during transition.
- Every collection technique MUST be confirmed to work in its target runtime (a container,
  a native binary, or the browser sandbox, which cannot do raw ARP/ICMP) before a plan
  depends on it.

Rationale: the home network spans several subnets, no single host sees all of them, and the set
of subnets changes over time. That requires multiple vantage points and runtime discovery.

### V. Faithful History

- Observations MUST be recorded with their timestamp, source collector, and subnet.
  Derived state (current inventory, topology) MUST be reproducible from stored observations.
- Device identity MUST be explicit and documented (e.g., MAC as primary key, with defined
  handling for randomized MACs and devices seen on multiple subnets).
- Changes (new device, IP or hostname change, device gone, device returned) MUST be detectable
  and queryable over time; history MUST NOT be silently overwritten.
- Retention limits, if any, MUST be explicit configuration, not implicit data loss.

Rationale: tracking changes over time is a core feature, and a correct history depends on a
trustworthy observation log.

## Deployment & Environment Constraints

- The server MUST run as a container on the user's NAS (192.168.1.0/24) and MUST be
  deployable from a single container image plus a mounted data volume.
- The application MUST NOT depend on specific subnets. Subnets are discovered at runtime from
  the collectors' network interfaces. A subnet that appears tomorrow MUST be handled without any
  code or configuration change. Only private (RFC 1918) IPv4 ranges are ever scanned.
- The server address and collector credentials MUST be configuration, never hard-coded.
- The web UI MUST work in a modern desktop browser on the LAN and MUST NOT require
  installing anything on the viewing machine.
- A collector that runs outside the container MUST be distributable as a single artifact
  that needs no system-wide installation (e.g., a standalone binary), or as a page served by
  the server, if the technique is feasible in a browser (see Principle IV).
- The technology stack is not fixed by this constitution; it is chosen and justified per
  feature in `/speckit-plan`, subject to these principles.

## Development Workflow & Quality Gates

- Work follows the Spec Kit flow: specify → (clarify) → plan → tasks → implement.
- Every plan MUST include a Constitution Check that confirms compliance with Principles I–V,
  or records the justified deviations in Complexity Tracking.
- Quality gates before a feature is complete: all tests pass; contract tests cover any
  change to the upload API; no outbound non-LAN network calls are introduced; and the
  container image builds and starts with a fresh data volume.
- Changes to the observation schema or the device identity rules MUST include a migration
  path for existing data.

## Governance

- This constitution supersedes conflicting practices, templates, and guidance in this
  repository.
- Amendments are made via `/speckit-constitution`. Each amendment must state its rationale
  and update the version and Last Amended date.
- Versioning: MAJOR for removing or redefining a principle; MINOR for adding a principle
  or section, or materially expanding one; PATCH for clarifications and wording.
- Compliance is reviewed at plan time (Constitution Check) and again before implementation
  is accepted. Any unjustified violation blocks completion.

**Version**: 1.1.0 | **Ratified**: 2026-10-03 | **Last Amended**: 2026-10-05
