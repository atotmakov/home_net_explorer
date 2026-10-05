# Research: LAN Inventory & Topology Explorer

**Feature**: `001-lan-inventory-topology` | **Date**: 2026-10-04

Each section resolves an unknown from the plan's Technical Context.

## R1. Language and runtime

- **Decision**: Go (current stable, ≥ 1.26) for both the server and the collector, in one module
  that builds two binaries.
- **Rationale**:
  - The constitution requires the desktop collector to be one file that needs no install. Go
    builds a static `.exe` for Windows and a Linux binary for the NAS from the same code, with no
    runtime to install.
  - Server and collector share the upload contract types, so contract tests on both sides use the
    same definitions (Principle IV).
  - Goroutines make it simple to probe 254 addresses in parallel within the 2-minute budget
    (SC-002).
  - The standard library covers HTTP routing (`net/http` method+path patterns), templates,
    embedding static assets (`embed`), and testing. That keeps dependencies minimal (Principle III).
- **Alternatives considered**:
  - **Python**: has the best scanning ecosystem (scapy), but scapy needs Npcap plus admin rights on
    Windows, and packaging a single-file collector (PyInstaller) is fragile and often flagged by
    antivirus.
  - **Node/TypeScript**: lets the UI share a language, but raw ARP/ICMP needs native addons, and a
    single-binary collector requires extra bundling tools.
  - **Rust**: comparable binaries, but slower to iterate for a one-person home project. Go's
    standard library already covers the server's needs.

## R2. Discovery on the NAS (server's built-in collector)

- **Decision**: The server runs the same collector engine in-process, against its directly attached
  subnet(s):
  1. **ARP sweep** of each on-link /24 using raw ARP requests (Linux, needs `CAP_NET_RAW`). Replies
     give IP and MAC.
  2. **Kernel neighbor table** (`/proc/net/arp`) is merged in, to catch hosts that answered recently
     but missed the sweep.
  3. **Hostname resolution**: reverse DNS (PTR) via the LAN resolver (normally the router, which
     knows DHCP names), plus **mDNS** reverse queries (`*.local`, common for Apple, printers, and
     IoT devices). Each lookup has a 1-second budget and runs in parallel.
     **Privacy rule**: PTR queries go only to a resolver with a private (RFC 1918) address. The
     default is the gateway of the subnet being resolved, and it can be overridden with
     `HNE_DNS_SERVER`, which must itself be a private address. The
     system resolver is **not** used, because the host may be configured with a public DNS server
     (e.g., 8.8.8.8). That would send the list of LAN addresses outside the home network,
     violating Principle I. The same rule applies to the collector.
- **Container requirements**: `network_mode: host` (the container shares the NAS's network, so it
  can send ARP on the real LAN) and `cap_add: [NET_RAW]`. Without host networking, the container
  would see only the Docker bridge.
- **Rationale**: ARP is the most reliable presence signal on a local segment. Devices that drop
  ping still answer ARP. It is also the only way to obtain MAC addresses, which are the identity
  key (Principle V).
- **Alternatives considered**: ICMP ping sweep alone (misses firewalled hosts and gives no MAC);
  nmap in the container (large external dependency, and it does far more than needed); passive
  sniffing only (slow to converge, misses quiet devices).

## R3. Discovery on the Windows desktop (remote collector)

- **Finding (2026-10-04)**: on the desktop, `Get-NetIPAddress`/`Get-NetRoute` show **two directly
  attached interfaces**:
  - `home`: 192.168.1.10/24 (default gateway 192.168.1.100)
  - `internet`: 192.168.8.176/24 (default gateway 192.168.8.1)

  This is only **today's example layout**, and nothing in the design depends on it (see R14).
  Both are on-link, so the desktop can get MAC addresses for both subnets.
- **Decision**: On Windows, the collector uses the built-in IP Helper API (`iphlpapi.dll`), called
  via `golang.org/x/sys/windows`:
  1. `SendARP` for each address in each on-link subnet, run in parallel. It sends a real ARP request
     and returns the MAC. **No admin rights and no Npcap needed.**
  2. `GetIpNetTable2` (ARP/neighbor cache) is merged in.
  3. For a subnet the user explicitly lists in the collector config that is **not** on-link
     (routed), the collector falls back to
     `IcmpSendEcho` (ping without admin rights) plus a TCP connect probe on a few common ports (80,
     443, 445, 22, 62078). These devices have no MAC, so they are recorded as weakly identified
     (spec edge case).
  4. Hostnames come from reverse DNS plus mDNS, as on the server.
- **Rationale**: this needs no install, no driver, and no elevation, which meets the constitution's
  "single artifact" deployment constraint. A browser-based collector was ruled out in the spec
  (browsers cannot read MACs or send ARP/ICMP). That satisfies Principle IV's requirement to confirm
  feasibility.
- **Alternatives considered**: raw packets via Npcap (needs driver install and admin); running
  `arp -a`/`ping` and parsing their output (fragile, depends on the system language).

## R4. Topology inference (subnet/gateway level only, per spec option C)

- **Decision**: Every collection run includes a short **vantage report**: the collector's
  interfaces (IP, prefix, MAC) and its routes (destination → next hop / on-link). The server infers
  links from these facts:
  - **Subnet membership**: a device belongs to every subnet in which it was observed.
  - **Gateway**: a route next hop (e.g., 192.168.1.100, 192.168.8.1) is marked as that subnet's
    gateway on the map.
  - **Bridge between subnets**: a device with addresses in two subnets (the same MAC seen in both,
    or a collector that has interfaces in both) is drawn as a link between those subnets. In this
    network, the desktop itself is the only known bridge.
  - **Manual links** ("connected via") are stored as user facts and always override inferred
    links.
- **Rationale**: this uses only data the collectors already have. It needs no traceroute, no
  SNMP, and no queries to network equipment (FR-028).
- **Alternatives considered**: traceroute between subnets (adds a probe type for little value at
  this scale); SNMP/LLDP (deferred to a later feature).

## R5. Storage and the history model

- **Decision**: A single **SQLite** database file on the mounted data volume, opened in WAL mode
  through `modernc.org/sqlite` (pure Go, no C compiler, so the static build still works). The data
  has two layers:
  1. **Source of truth**: an append-only `collection_runs` table. Each uploaded run is stored once
     (keyed by `collection_id`), along with its metadata and the gzip-compressed original payload.
  2. **Projections**, all of which can be rebuilt from layer 1 (Principle V):
     - `sightings`: one row per distinct (device, IP, MAC, hostname, collector, subnet) tuple, with
       `first_seen`, `last_seen`, and `seen_count`. A run where nothing changed only updates
       `last_seen`.
     - `devices`, `device_addresses`, `events`, `links`.
     - User edits (names, notes, merges, manual links) are their own append-only facts, so a
       rebuild re-applies them.
- **Sizing**: about 250 devices × 96 runs/day × 2 collectors gives about 48k observations per day.
  Storing one row per observation would be about 17M rows per year. Compressed runs are about
  5 KB each, or roughly 350 MB per year. Sightings grow only when something changes. List, timeline,
  and map queries run on the small projections, which supports SC-007 (< 2 s).
- **Rationale**: one process, one file, no extra service (Principle III). Backup and export
  (FR-032) means copying the database file or exporting JSON. The raw runs keep history faithful,
  and the projections keep queries fast.
- **Alternatives considered**: PostgreSQL (an extra container to run on the NAS for no benefit at
  this scale); one row per raw observation (correct, but unbounded growth and slower queries); a
  plain file store (no query capability).

## R6. Device identity and merge rules

- **Decision**:
  - The primary identity is the **MAC address**. A MAC whose locally-administered bit is set
    (second-lowest bit of the first byte) is flagged as **randomized**. Randomized MACs still key a
    device, but the UI suggests merging them.
  - When no MAC is available (routed subnets), the identity is **(subnet, hostname)** if there is a
    hostname, otherwise **(subnet, IP)**. Such devices are flagged **weak identity**. Once a MAC
    becomes known for the same IP and hostname, the weak record is automatically folded into the
    MAC-keyed device, and a `merged` event is recorded.
  - Merges and splits made by the user are stored facts (an alias table: identity key →
    device_id). They are applied before automatic matching, so they survive rebuilds.
  - **IP reuse**: an IP that moves to a different MAC causes an `ip_changed` event on both devices.
    The old device loses that address, and the new device gains it.
- **Rationale**: this fulfills FR-013/FR-015 and the constitution's requirement that identity rules
  be explicit.

## R7. Online/offline detection

- **Decision**:
  - A collector is **active** for subnet S at time *t* if it completed a scan of S within
    3 × its interval before *t*.
  - Offline status is evaluated **only when a completed scan is ingested**, using the timestamps
    stored with scans, never the current time.
  - After ingesting a completed scan R of subnet S, each device in S that R didn't see goes
    **offline** when both of these hold:
    1. every collector that is active for S at `R.finished_at` has completed ≥ 3 scans of S since
       the device's `last_seen`, and none of those scans saw it, and
    2. `R.finished_at − last_seen` ≥ 3 × the interval of R's collector.
  - The `went_offline` event gets time `R.finished_at`. When a device is seen again,
    `came_online` gets that observation's time.
  - **Stale** (S has no active collector) is computed when a page loads, from the current time,
    and is never stored. That means it can't affect rebuilds.
- **Rationale**:
  - This implements FR-018 and the spec's "gaps are visible" assumption.
  - A collector that stopped (e.g., the desktop is off) is not active, so it neither blocks nor
    triggers offline detection. The NAS alone can still mark devices on its subnet offline.
  - Deciding everything from stored scan data keeps the derived tables reproducible
    (Principle V). A timer-based check would not be.

## R8. Collector → server upload protocol

- **Decision**: HTTP JSON `POST /api/v1/collections` with `Authorization: Bearer <token>`. The body
  is described in [contracts/collector-upload-api.yaml](contracts/collector-upload-api.yaml).
  - It is **idempotent**: the collector creates a UUIDv4 `collection_id` for each run. A re-upload
    returns `200 duplicate` and changes nothing (FR-011).
  - It is **versioned**: the URL is `/api/v1/` and the body includes `schema_version: 1`. The
    server rejects unknown major versions with 422 (Principle IV).
  - **Clock skew**: the body includes `sent_at`. The server compares it with its own clock and flags
    the collector when the difference exceeds 5 minutes.
  - **Late uploads**: observations carry their `observed_at` time. Projections order by observation
    time, not arrival time.
- **Collector spool**: each finished run is written to a `spool/` folder before upload and deleted
  after a 200/201 response. Pending files are retried with exponential backoff (1 minute up to
  1 hour). A 401/403 response stops retries and shows an error (FR-010, story 2 scenario 2).
- **Transport security**: plain HTTP within the LAN by default. The token is the authentication. An
  optional HTTPS URL is supported if the user puts a TLS proxy in front. Certificate management is
  out of scope (Principle III).
  **Accepted risk**: on plain HTTP, a device on the LAN that can watch traffic could capture a
  collector token. Tokens can only upload observations; they can't read data or log in. The
  owner can revoke and re-issue a token at any time. This is stated in the README.
- **New subnets**: the response lists `new_subnets` (subnets first seen in this run, now tracked
  automatically). `GET /api/v1/ping` returns `ignored_subnets` so collectors skip subnets the owner
  has ignored.
- **Alternatives considered**: gRPC (more tooling for one endpoint); MQTT (needs a broker
  service); shared folder sync (no authentication, and duplicate handling is awkward).

## R9. Web UI approach

- **Decision**: The server renders HTML with Go `html/template`. **htmx** handles partial updates
  (filtering, sorting, acknowledging). **Cytoscape.js** draws the topology map. Both JavaScript
  files are **vendored and embedded** in the binary, with no CDN (Principle I, FR-031). There is
  no frontend build step and no Node toolchain.
- **Rationale**: this is the smallest stack that delivers sortable tables, a timeline, and an
  interactive graph. Cytoscape.js has built-in compound nodes (subnet groups containing devices),
  which match FR-025 directly.
- **Alternatives considered**: a React/Vue single-page app (adds a build pipeline and a second
  language toolchain); D3 (lower-level, so more code for the same map).

## R10. Authentication

- **Decision**:
  - **Owner login**: on first start, the UI requires setting a password, stored as a bcrypt hash
    (`golang.org/x/crypto/bcrypt`). After that, an HttpOnly, SameSite=Strict session cookie holds
    a random 256-bit session ID, stored server-side. State-changing forms are protected against
    cross-site requests (CSRF) by SameSite plus an Origin header check. Login attempts are
    rate-limited.
  - **Collector tokens**: 256-bit random tokens, shown once when created. Only a SHA-256 hash is
    stored. Each collector has its own revocable token (FR-008).
- **Rationale**: this fulfills FR-030 for a single owner without any external identity provider.

## R11. MAC vendor lookup

- **Decision**: Embed the IEEE OUI registry (MA-L, plus MA-M/MA-S where practical) as a compressed
  file inside the binary. It is refreshed by a `make`/`go generate` step at release time, never at
  runtime (FR-016, Principle I).

## R12. Deployment to the NAS

- **Decision**:
  - A multi-arch image (`linux/amd64`, `linux/arm64`), built from `scratch` or `distroless/static`
    and containing only the static server binary.
  - A `compose.yaml` with `network_mode: host`, `cap_add: [NET_RAW]`, the volume `./data:/data`,
    the port set by `HNE_LISTEN` (default `:8080`), and `restart: unless-stopped`.
  - The server also hosts the matching collector binaries for download (`/downloads/...`), so the
    desktop always gets a collector that speaks the server's contract version.
- **Open item for the user**: the NAS brand/OS (Synology DSM, QNAP, Unraid, or plain Linux) only
  affects how compose is installed, not the design.

## R13. Testing strategy (Principle II)

- **Unit tests**: `go test`, next to the code. All network access goes through interfaces
  (`Prober`, `Resolver`, `NeighborTable`, `RouteReader`, `Clock`), which have fakes and recorded
  fixtures. Identity, merge, event, and offline logic is tested with scripted run sequences.
- **Contract tests**: `tests/contract/`. JSON fixtures are checked against the OpenAPI schema on the
  collector side (serialization) and the server side (handler accepts valid fixtures and rejects
  invalid, unauthorized, and duplicate ones).
- **Integration tests**: `tests/integration/`. A real server with a temporary SQLite file is fed
  scripted runs over HTTP. The tests check the device list, timeline, map JSON, and the rebuild
  check (projections rebuilt from runs must equal the incrementally updated projections).
- **Local-only guard**: a test runs the server with a dialer that fails on any non-LAN destination
  (SC-008).
- **Platform probes** (raw ARP, `SendARP`) are covered by build-tagged smoke tests that run only on
  real hardware (`-tags hwtest`).

## R14. Subnet discovery (no fixed subnets)

- **Decision**: the application contains no subnet list (FR-006, Constitution "Deployment").
  - **Collector default targets**: every IPv4 interface whose subnet is private (RFC 1918:
    10/8, 172.16/12, 192.168/16) and /22 or narrower, taken from the collector's own vantage
    report at the start of each scan. The built-in NAS collector and the desktop collector work
    the same way.
  - **Wider on-link subnets** (e.g., a /16) are reported as `skipped` in the run and in the UI,
    and are never scanned automatically.
  - **Non-private subnets** are never scanned, even if listed explicitly (Principle I, FR-007).
  - **Optional restriction**: a collector's `subnets` config (or `HNE_SUBNETS` for the built-in
    collector) limits scanning to the listed private subnets. Explicitly listed routed subnets use
    the `icmp_tcp` fallback (R3).
  - **Server side**: the first run that reports a subnet creates it (auto-tracked) and records
    it in `new_subnets`. The owner sees a notice and can **ignore** the subnet. Ignored subnets
    are returned by `/api/v1/ping` and skipped by collectors. Any observations for them that
    still arrive are stored in the raw run but not folded into the derived tables (data-model.md
    "Ingest never filters").
  - Skipped subnets (too large or ignored) never become subnet records. They are visible only
    in the per-run scan list.
- **Rationale**: the owner adds routers, VLANs, and access points over time, and the system must
  follow without a configuration change or restart. Restricting scans to private ranges keeps
  the tool from ever probing outside the home.
- **Alternatives considered**: an owner-maintained subnet list (rejected by the owner: "must not
  depend on specific subnets"); scanning every attached interface including public ones (privacy
  and FR-007 risk).

## Environment notes (examples only; nothing depends on them)

- Desktop today: `home` 192.168.1.10/24 (gateway 192.168.1.100), `internet` 192.168.8.176/24
  (gateway 192.168.8.1). The NAS is on 192.168.1.0/24. These values are used only in test
  fixtures and the quickstart, as realistic samples.
