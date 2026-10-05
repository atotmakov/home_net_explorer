---

description: "Task list for LAN Inventory & Topology Explorer"
---

# Tasks: LAN Inventory & Topology Explorer

**Input**: Design documents from `specs/001-lan-inventory-topology/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: REQUIRED. Constitution Principle II (Test-First, non-negotiable) applies. Every test task
comes before the implementation it covers, and the test MUST be run and seen to FAIL before that
implementation starts. Unit tests never touch the real network. They use the fakes from T017.

**Organization**: tasks are grouped by user story. Each story phase is a deliverable increment
that can be tested on its own.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on unfinished tasks)
- **[Story]**: US1–US4 from spec.md
- Paths are relative to the repository root (one Go module, see plan.md "Project Structure")

## Conventions for every task

- Module path: `github.com/atotmakov/home_net_explorer`. Go ≥ 1.26.
- Timestamps are UTC, RFC 3339 with milliseconds (data-model.md).
- MAC format everywhere is lower-case `aa:bb:cc:dd:ee:ff`.
- **The application must not depend on specific subnets** (FR-006, research R14, constitution
  v1.1.0). Subnets are discovered at runtime from each collector's interfaces, limited to private
  (RFC 1918) ranges with a prefix of /22 or narrower. The only place IPv4 range literals may appear
  in non-test Go code is the RFC 1918 range table in `internal/contract/private.go`. Test fixtures
  use example values (192.168.1.0/24, 192.168.8.0/24, 10.20.30.0/24), and tests must also cover a
  subnet never seen before.
- Allowed runtime dependencies (plan.md): `modernc.org/sqlite`, `github.com/mdlayher/arp`,
  `golang.org/x/sys`, `golang.org/x/net`, `golang.org/x/crypto`. Test-only:
  `github.com/santhosh-tekuri/jsonschema/v6` and `gopkg.in/yaml.v3` (to read the OpenAPI file). Adding any other dependency requires a Complexity
  Tracking entry in plan.md (Principle III).

---

## Implementation notes (recorded during /speckit-implement)

- The server assembly and the built-in scan loop live in `internal/app` (not `cmd/hne-server`),
  so integration tests start the same server as `main` (T031, T041).
- `Rebuild` lives in `internal/inventory` (`inventory.Rebuild` / `RebuildTx`), because `store`
  cannot import the projection code (T040). `app.App.Rebuild` runs it under the ingester lock.
- `contract.Validate(run)` takes no clock: skew is reported, never rejected (T008/T015).
- The over-limit fixtures (4097 observations, 17 subnets) are generated in code by
  `internal/contract/contracttest` instead of being committed as large JSON files (T006).
- `collection_runs` also stores each run's `interval_seconds`, so the offline rule uses the
  interval in force for that run (R7).

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: project initialization and basic structure

- [X] T001 Create `go.mod` (module `github.com/atotmakov/home_net_explorer`, go 1.26). Add a
  package skeleton with a `doc.go` that states each package's purpose (from plan.md) in
  `internal/contract/`, `internal/collect/`, `internal/upload/`, `internal/ingest/`,
  `internal/inventory/`, `internal/store/`, `internal/auth/`, `internal/oui/`, `internal/web/`,
  `internal/clock/`. Add empty `main` packages in `cmd/hne-server/main.go` and
  `cmd/hne-collector/main.go`, and the directories `tests/contract/`, `tests/integration/`,
  `deploy/`, `tools/gen-oui/`
- [X] T002 [P] Create `Makefile` with these targets:
  - `test` (`go test ./...`), `test-contract`, `test-integration`
  - `lint` (`gofmt -l` must be empty, plus `go vet ./...`, plus a check that no non-test `.go`
    file other than `internal/contract/private.go` contains an IPv4 CIDR literal, regex
    `[0-9]{1,3}(\.[0-9]{1,3}){3}/[0-9]{1,2}`)
  - `build-server` (linux/amd64 + linux/arm64, `CGO_ENABLED=0`)
  - `build-collector` (windows/amd64 `.exe`, linux/amd64, linux/arm64, `CGO_ENABLED=0`), output to `dist/`
  - `hwtest` (`go test -tags hwtest ./internal/collect/...`)

  CI (`.github/workflows/ci.yml`, already in the repo) runs `make lint` and, for tests, the same
  package sets as `test`/`test-contract`/`test-integration`, so keep the targets and package paths
  in sync
- [X] T003 [P] Create `.gitignore` listing `dist/`, `data/`, `tmp-data/`, `spool/`, `*.exe`,
  `hne-collector.json`, `hne-collector.log*`, and `*.db*`
- [X] T004 [P] Vendor htmx (2.x) and Cytoscape.js (3.x) minified files into
  `internal/web/static/vendor/htmx.min.js` and `internal/web/static/vendor/cytoscape.min.js`.
  Record each file's version, source URL, and SHA-256 in `internal/web/static/vendor/VERSIONS.txt`.
  These files MUST be served locally, never from a CDN (FR-031, research R9)
- [X] T005 [P] Write the OUI generator `tools/gen-oui/main.go`. It downloads IEEE MA-L/MA-M/MA-S CSVs
  **at development time only** and writes `internal/oui/oui.tsv.gz` (prefix-bits, prefix-hex,
  vendor). Add `//go:generate go run ../../tools/gen-oui` in `internal/oui/doc.go`, then run it
  once and commit the data file (research R11)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the upload contract, storage, clock, config, owner auth, the web skeleton, the probe
interfaces, and the ingest path. Every story needs these.

**⚠️ CRITICAL**: no user story work can begin until this phase is complete

### Tests first

- [X] T006 [P] Create contract fixtures in `tests/contract/fixtures/`. Valid fixtures:
  - `valid_minimal.json`: one subnet `192.168.1.0/24`, method `arp`, two observations
  - `valid_full.json`: two subnets `192.168.1.0/24` (`arp`) and `192.168.8.0/24` (`arp`), a
    vantage report with 2 interfaces and routes (`0.0.0.0/0` next hop `192.168.1.100`), and
    hostnames with sources `dns`/`mdns`
  - `valid_routed.json`: `icmp_tcp` method, observations without MAC
  - `valid_new_subnet.json`: a never-seen subnet `10.20.30.0/24` (`arp`), plus a `skipped` entry
    `10.0.0.0/16` with `skip_reason` `too_large`

  Invalid fixtures, one per rule in data-model.md "Validation summary":
  - `invalid_mac_format.json`, `invalid_arp_without_mac.json`, `invalid_ip_outside_subnet.json`
  - `invalid_observed_outside_window.json`, `invalid_window_over_1h.json`,
    `invalid_future_window.json`
  - `invalid_too_many_observations.json` (4097), `invalid_too_many_subnets.json` (17)
  - `invalid_schema_version_2.json`, `invalid_unknown_field.json`
  - `invalid_public_subnet.json` (`8.8.8.0/24`), `invalid_skipped_without_reason.json`,
    `invalid_ip_in_skipped_subnet.json` (observation inside a `skipped` entry)
- [X] T007 [P] Write `tests/contract/schema_test.go`. It loads
  `specs/001-lan-inventory-topology/contracts/collector-upload-api.yaml`, extracts
  `components.schemas`, and checks that every `valid_*.json` passes and every `invalid_*` that is
  schema-detectable fails, using `santhosh-tekuri/jsonschema/v6`. `invalid_skipped_without_reason`
  is schema-detectable via the `if`/`then` rule on `SubnetScan`, while the subnet-membership and
  RFC 1918 rules are checked only by Go validation (T008). It also checks that marshaling
  `contract.CollectionRun` built from each valid fixture produces JSON that still validates (a
  round trip)
- [X] T008 [P] Write `internal/contract/validate_test.go`, a table test over all fixtures from T006.
  `Validate(run, now)` must accept the valid ones and return the matching error code for each
  invalid one. Rules to quote exactly:
  - "`ip` must be inside a non-skipped subnet of the same run"
  - "MAC is required when method is `arp`"
  - "`observed_at` must be within the run window"
  - "The run window must be no longer than 1 hour and must not be more than 5 minutes in the
    future relative to `sent_at`"
  - "No more than 4096 observations and 16 subnets per run"
  - "`schema_version` must be supported"
  - "Every subnet `cidr` must be private (RFC 1918) with a prefix from /16 to /30"
  - `skip_reason` is required when method = `skipped`
- [X] T009 [P] Write `internal/store/store_test.go`. `Open(path)` on a temp dir must create the DB
  in WAL mode with `foreign_keys=ON` and `busy_timeout` ≥ 5000 ms. Migrations must be idempotent:
  running Open twice gives the same `user_version`. Every table from data-model.md must exist.
  Add a **migration harness**: for every migration *k*, a database seeded with sample rows at
  version *k−1* migrates to *k* with row counts preserved. The harness has to exist from 0001
  onward, so later schema changes can't skip it (Constitution, Development Workflow)
- [X] T010 [P] Write `internal/auth/owner_test.go`. Cover:
  - The password can be set only once, and must be ≥ 10 chars.
  - Verify works with bcrypt.
  - A session ID is 256-bit random, and only its SHA-256 is stored in `sessions(id_hash,
    created_at, expires_at)`.
  - Sessions expire after 30 days.
  - The login rate limit is "5 attempts per minute".
- [X] T011 [P] Write `internal/web/auth_test.go` (httptest). With no password set, every page
  redirects to `/setup`. With a password set, pages redirect to `/login` until logged in. `/setup`
  returns 404 once a password exists. A POST whose `Origin` doesn't match the Host gets 403. The
  cookie is HttpOnly + SameSite=Strict. `/healthz` returns `200 ok` without auth, and `/api/v1/*`
  is not redirected (it uses bearer auth later)
- [X] T012 [P] Write `cmd/hne-server/config_test.go`. Cover:
  - `HNE_LISTEN` defaults to `:8080`, `HNE_DATA` to `/data`, and `HNE_SCAN_INTERVAL` to `900s`.
  - With `HNE_SUBNETS` unset (the default), the config holds **no subnets**, meaning auto-discovery.
  - If set, `HNE_SUBNETS` must be comma-separated **private** IPv4 CIDRs with a prefix from /16
    to /30, and anything else fails.
  - `HNE_DNS_SERVER` is optional but, if set, MUST be a private (RFC 1918) address, or config
    fails (Principle I, research R2).
  - The `healthcheck` subcommand exits 0 against a test server returning 200 on `/healthz`, and
    exits 1 when nothing is listening.
- [X] T013 [P] Write `internal/ingest/ingest_test.go` against a temp store. Cover:
  - `Ingest(ctx, collectorID, run, receivedAt)` stores `collection_runs`, with `payload_gz` equal
    to the gzip of the original body.
  - It stores the `run_subnets` rows (`collection_id`, `cidr`, `method`, `complete`,
    `hosts_probed`).
  - It returns `stored`.
  - A second call with the same `collection_id` returns `duplicate` and changes no rows.
  - It updates the collector's `last_report_at` and `last_clock_skew_ms = sent_at - received_at`.
  - It calls the projection applier exactly once per stored run (use a fake applier), and copies
    the subnets the applier reports as new into `UploadResult.new_subnets`.
  - **Ingest never filters**: a run covering a subnet the fake applier treats as ignored is still
    stored byte-for-byte in `collection_runs`, with all its `run_subnets` rows.

### Implementation

- [X] T014 Write `internal/contract/v1.go`: Go structs matching `CollectionRun`, `Vantage`,
  `SubnetScan`, `Observation`, `UploadResult`, and `Error` in
  `contracts/collector-upload-api.yaml`. Decode JSON with `DisallowUnknownFields`. Add constants
  `SchemaVersion = 1`, `MaxObservations = 4096`, `MaxSubnets = 16`, `MaxBodyBytes = 2 << 20`,
  `MaxAutoScanPrefixBits = 22`. Also write `internal/contract/private.go` with
  `IsPrivate(netip.Prefix) bool` over the RFC 1918 ranges. It is the only file allowed to contain
  range literals (depends on T006–T008)
- [X] T015 Write `internal/contract/validate.go`: `Validate(run *CollectionRun, now time.Time)
  error`, returning typed codes (`validation_failed` with detail, `unsupported_schema`) for each
  rule in T008. Make T007 and T008 pass
- [X] T016 [P] Write `internal/clock/clock.go`: a `Clock` interface (`Now() time.Time`) with a real
  implementation, plus `internal/clock/fake.go` with a settable/advanceable fake for tests
- [X] T017 [P] Write `internal/collect/probe.go` with these interfaces:
  - `Prober`: `Probe(ctx, iface, ip) (mac string, ok bool, err)`
  - `NeighborTable`: `Entries(ctx) ([]Neighbor, error)`
  - `RouteReader`: `Vantage(ctx) (contract.Vantage, error)`
  - `Resolver`: `Lookup(ctx, ip) (host string, source string, ok bool)`

  Also write `internal/collect/collecttest/fakes.go` with scriptable fakes of each, plus a
  `FakeNetwork` that maps IP → (mac, hostname, responds bool)
- [X] T018 Write `internal/store/migrations/0001_init.sql` and `internal/store/store.go`
  (`modernc.org/sqlite`, WAL, embedded migrations via `user_version`). Create every table in
  data-model.md:
  - Facts: `collectors`, `collection_runs`, `run_subnets`, `user_device_attrs`,
    `user_identity_alias`, `user_links`, `user_acks`, `user_subnet_attrs`, `settings`, `sessions`.
    User fact tables reference **`identity_key`** (and `cidr` for subnets), never `devices.id`
    (data-model.md "User facts")
  - Projections: `subnets`, `sightings`, `devices`, `device_addresses`, `events`, `links`

  Include these constraints exactly:
  - `collectors.name` unique, 1–64 chars, `[a-z0-9-]`; `kind` in (`builtin`,`remote`)
  - `subnets.cidr` unique; `subnets.ignored` default false; `subnets.first_seen_at`,
    `subnets.discovered_by`
  - `run_subnets.method` in (`arp`,`icmp_tcp`,`skipped`); `skip_reason` in (`too_large`,`ignored`)
    or null
  - `devices.identity_key` unique
  - events unique on (`device_id`, `type`, `at`, `new_value`)
  - `devices.status` in (`new`,`new_offline`,`online`,`offline`,`merged_away`)

  Add `internal/store/projections.go` with `ResetProjections(tx)`, which truncates only the
  projection tables. Make T009 pass
- [X] T019 Write `internal/auth/owner.go`: owner password (bcrypt, cost 12) stored in `settings`;
  sessions; and a per-IP login rate limiter (5/min). Make T010 pass
- [X] T020 Write `cmd/hne-server/config.go`: parse the env/flags from T012, with the flags
  `--data`, `--listen`, and `--no-builtin-scan`, plus the `healthcheck` subcommand (GET
  `http://127.0.0.1<listen>/healthz`, exit 0 or 1). Make T012 pass
- [X] T021 Write `internal/web/server.go`: a `net/http` ServeMux with method+path patterns, plus
  this middleware: request log, panic recovery, session-required (redirect to `/setup` or
  `/login`), and an Origin check on unsafe methods. Templates come from
  `internal/web/templates/*.html` via `embed`. The base layout `internal/web/templates/layout.html`
  has nav links Home/Devices/Timeline/Map/Collectors/Settings and links only local
  `/static/...` assets. Add static file serving from `internal/web/static/` and `/healthz`
- [X] T022 Write `internal/web/auth_handlers.go` and `internal/web/templates/{setup,login}.html`:
  `GET/POST /setup`, `GET/POST /login`, and `POST /logout`. Make T011 pass
- [X] T023 Write `internal/ingest/ingest.go`: `Ingester` with
  `Ingest(ctx, collectorID int64, raw []byte, run *contract.CollectionRun, receivedAt time.Time)
  (contract.UploadResult, error)`. It runs one transaction that inserts the run and its
  run_subnets **unchanged**, then calls `Applier.Apply(tx, run, collectorID) (newSubnets
  []string, err error)` and returns those as `new_subnets`. Duplicates are detected by PK conflict
  on `collection_id`. Ingest never filters observations (data-model.md). Make T013 pass
- [X] T024 Write `cmd/hne-server/main.go`. It loads config, opens the store, and ensures the
  `builtin` collector row named `nas` (`interval_seconds` = scan interval). It seeds `settings`
  defaults (offline multiplier 3) and seeds **no subnets**. Then it starts the HTTP server with
  graceful shutdown on SIGTERM

**Checkpoint**: `make test` passes. The server starts, `/setup` → `/login` works, and the empty
layout renders.

---

## Phase 3: User Story 1 - See every device on my home subnet (Priority: P1) 🎯 MVP

**Goal**: the server on the NAS scans its own subnet (on demand and on a schedule) and shows a
sortable, filterable inventory with IP, MAC, hostname, manufacturer, first/last seen, and
friendly names that survive scans.

**Independent Test**: deploy only the server, click **Scan now**, and within 2 minutes compare
`/devices` with the router's DHCP table (quickstart.md §3).

### Tests for User Story 1 ⚠️ write first, see them fail

- [X] T025 [P] [US1] Write `internal/oui/oui_test.go`. Cover:
  - Known prefixes resolve: MA-L 24-bit, MA-M 28-bit, MA-S 36-bit, longest prefix wins.
  - An unknown prefix returns "".
  - `IsRandomized(mac)` is true when "locally-administered bit set (second-lowest bit of the first
    byte)", e.g. `da:a1:19:...` is true and `00:11:32:...` is false.
  - A randomized MAC gets manufacturer = "" ("Null for randomized or unknown MACs").
- [X] T026 [P] [US1] Write `internal/collect/engine_test.go` with `collecttest.FakeNetwork`. Cover:
  - **Auto targets** (no targets given): the engine scans every vantage interface subnet that is
    private and /22 or narrower. With fake interfaces `192.168.1.0/24`, `10.20.30.0/24`,
    `10.0.0.0/16`, and a public `203.0.113.0/24`, it scans the first two, reports `10.0.0.0/16` as
    `skipped`/`too_large`, and omits the public one entirely. Replacing the fake interfaces between
    two scans (a new subnet appears) changes the targets with no config change.
  - Subnets passed as `ignored` are reported as `skipped`/`ignored` and not probed.
  - `Engine.Scan` for `192.168.1.0/24` probes 254 hosts with concurrency ≤ 64.
  - It produces a `contract.CollectionRun` with a UUIDv4 `collection_id` and a `SubnetScan`
    {method `arp`, `complete` true, `hosts_probed` 254}.
  - There is one observation per responding host. Neighbor-cache entries not found by the probe
    are added with method `neighbor_cache`.
  - Each observation's `observed_at` is within the run window.
  - Cancelling the context mid-scan gives `complete` false.
  - The output passes `contract.Validate`.
- [X] T027 [P] [US1] Write `internal/collect/names_test.go`. Cover:
  - The resolver queries PTR only at the configured DNS server, defaulting to the subnet's gateway
    from the vantage routes.
  - `NewNameResolver` returns an error if the DNS server is not a private (RFC 1918) address,
    and a public gateway is never used as the default (Principle I).
  - mDNS reverse lookup builds a valid `x.x.x.x.in-addr.arpa` PTR query to `224.0.0.251:5353`
    using `dnsmessage` and parses the answer from a recorded fixture.
  - There is a 1-second timeout per lookup.
  - `hostname_source` is `dns` or `mdns`, and DNS is preferred when both answer.
- [X] T028 [P] [US1] Write `internal/inventory/apply_test.go` (temp store, `clock.Fake`). Cover:
  - The first run creates devices with `identity_key` `mac:<mac>` and `identity_strength`
    `strong`, plus manufacturer from OUI, `mac_randomized`, one `device_addresses` row
    (`current` true), and a sighting with `seen_count` 1.
  - A second identical run extends `last_seen` and increments `seen_count` with no new sighting
    row.
  - A changed hostname creates a new sighting row.
  - `display_name` is "user name, else hostname, else IP".
  - Device IDs are assigned in ingest order.
  - **Subnet projection (F1)**: the first run with a non-skipped `SubnetScan` for a CIDR creates a
    `subnets` row (`first_seen_at` = the run's `started_at`, `discovered_by` = its collector), and
    `Apply` returns it as new. The second run returns nothing new. A `skipped`/`too_large` entry
    creates no row and is not returned.
  - A `user_subnet_attrs` fact `ignored = true` makes `Apply` skip that subnet's observations (no
    sightings or devices), while the run remains stored. Renaming sets `subnets.name`.
- [X] T029 [P] [US1] Write `internal/inventory/offline_test.go`. The rule from research R7:
  - "A collector is **active** for subnet S at time *t* if it completed a scan of S within
    3 × its interval before *t*."
  - "After ingesting a completed scan R of subnet S, each device in S that R didn't see goes
    **offline** when both of these hold: (1) every collector that is active for S at
    `R.finished_at` has completed ≥ 3 scans of S since the device's `last_seen`, and none of those
    scans saw it, and (2) `R.finished_at − last_seen` ≥ 3 × the interval of R's collector."

  Cover:
  - Incomplete or skipped runs (`complete` false) don't count.
  - **NAS active, desktop silent**: the device goes offline after 3 NAS scans. The silent
    collector neither blocks nor triggers it.
  - Both collectors active: the device goes offline only after each has completed 3 scans
    without seeing it.
  - The `went_offline` time equals the triggering scan's `finished_at`.
  - Moving the fake clock forward with **no** scans changes nothing in the database.
  - If every covering collector is inactive, devices keep their status, and
    `store.SubnetStatus(now)` reports the subnet **stale**.
  - The multiplier comes from `settings`.
- [X] T030 [P] [US1] Write `internal/store/devices_query_test.go`. `ListDevices(filter)` supports
  `q` (matches IP, MAC, hostname, or name), `subnet`, `status`, `manufacturer`, `seen_after`,
  `seen_before`, and `sort` ∈ {ip (numeric order), mac, hostname, manufacturer, first_seen,
  last_seen} with `dir` asc/desc. `merged_away` devices are excluded
- [X] T031 [P] [US1] Write `tests/integration/us1_inventory_test.go`. Start the full server
  (httptest) with a temp DB, `--no-builtin-scan`, and a fake engine injected. Log in, then:
  - `POST /scan` returns 202. After completion, `GET /devices` lists the fake devices with IP,
    MAC, hostname, and manufacturer.
  - Sorting and filtering via query params work.
  - `POST /devices/{id}/attrs` name/notes/type persists across another scan.
  - Device type is limited to `router`, `access_point`, `switch`, `computer`, `phone`, `tablet`,
    `tv`, `printer`, `nas`, `iot`, `other`, and anything else gets 400.
  - Scheduled scans fire on interval (fake clock).
  - **Rebuild invariant**: projections after `store.Rebuild` equal the incrementally built
    projections.

### Implementation for User Story 1

- [X] T032 [P] [US1] Write `internal/oui/oui.go`: load the embedded `oui.tsv.gz` once, use a
  longest-prefix lookup, and add `IsRandomized`. Make T025 pass
- [X] T033 [P] [US1] Write `internal/collect/arp_linux.go` (`//go:build linux`): a `Prober` using
  `mdlayher/arp` on the interface whose subnet contains the target, with 1 retry and a 300 ms
  timeout, rate limited to ≤ 200 requests/s. Also write `internal/collect/neighbor_linux.go`,
  which parses `/proc/net/arp` and skips incomplete entries (flags 0x0)
- [X] T034 [P] [US1] Write `internal/collect/routes_linux.go` (`//go:build linux`): a
  `RouteReader` built from `net.Interfaces()` plus `/proc/net/route`, which fills
  `contract.Vantage` interfaces (name, ip, prefix_len, mac) and routes (destination, next_hop,
  interface)
- [X] T035 [US1] Write `internal/collect/names.go`: a `Resolver` that does PTR queries via
  `net.Resolver` with a custom `Dial` pinned to the LAN DNS server (never the system resolver),
  plus an mDNS reverse query via `golang.org/x/net/dns/dnsmessage`. Make T027 pass
- [X] T036 [US1] Write `internal/collect/engine.go`: `Engine{Prober, NeighborTable, RouteReader,
  Resolver, Clock}` and `Scan(ctx, targets)`. It works out which subnets are on-link from the
  vantage report, picks auto targets via `contract.IsPrivate` and `MaxAutoScanPrefixBits` when
  none are given, probes in parallel (≤ 64), merges neighbor entries, resolves names in parallel,
  and assembles a `contract.CollectionRun` (including `skipped` entries). Make T026 pass
- [X] T037 [US1] Write `internal/inventory/identity.go` (MAC identity key and randomized flag) and
  `internal/inventory/apply.go`, which implements `ingest.Applier`. It folds observations into
  `sightings`, `devices`, and `device_addresses` per data-model.md "Sighting" (extend `last_seen`
  when the tuple matches the latest sighting for the same collector and subnet, otherwise insert).
  It sets the manufacturer via `oui`. It also maintains the `subnets` projection: it creates
  rows for non-skipped CIDRs, returns them as new, applies `user_subnet_attrs` (name/ignored), and
  skips observations of ignored subnets. Make T028 pass
- [X] T038 [US1] Write `internal/inventory/offline.go`: `EvaluateStatus(tx, run)` sets `online` or
  `offline` per R7, using only stored scan timestamps (no `now`). Call it only at the end of
  `Apply` for completed scans. Add `store.SubnetStatus(now)` (in T039) to compute staleness when a
  page loads. Nothing writes events on a timer. Make T029 pass
- [X] T039 [US1] Write `internal/store/devices_query.go`: `ListDevices`, `GetDevice`,
  `SetDeviceAttr` (appends to `user_device_attrs` keyed by the device's `identity_key`; latest row
  wins across its aliases), `HomeCounts`, and `SubnetStatus(now)` (last scanned, by which
  collector, stale flag computed from `now`, never stored). Make T030 pass
- [X] T040 [US1] Write `internal/store/rebuild.go`: `Rebuild(ctx)` runs `ResetProjections`, then
  replays **one merged stream**: `collection_runs` ordered by `received_at` and all user fact
  tables ordered by `at`, with the scan first on ties, through the `Applier` and the user-fact
  applier (data-model.md "Rebuild invariant"). Wire `hne-server rebuild` as a subcommand in
  `cmd/hne-server/main.go`
- [X] T041 [US1] Write `cmd/hne-server/scanner.go`: the built-in collector loop. It runs
  `Engine.Scan` with auto targets (or `HNE_SUBNETS` if set), passing the currently ignored subnets,
  every `interval_seconds` and on demand via a channel,
  then hands the run to `Ingester.Ingest` as collector `nas` (the same path as uploads, Principle
  IV). Only one scan runs at a time, and it exposes the current scan status
- [X] T042 [US1] Write `internal/web/devices.go` and the templates `devices.html`,
  `devices_rows.html` (htmx partial), and `device.html` (addresses, user fields form). Routes:
  `GET /devices` (query params from contracts/web-ui.md), `GET /devices/{id}`, and
  `POST /devices/{id}/attrs`. The status column distinguishes online and offline. The list shows a
  **randomized MAC** badge (when `mac_randomized`) and a **weak ID** badge (when
  `identity_strength = weak`), and both can be used as filters
- [X] T043 [US1] Write `internal/web/scan.go`: `POST /scan` returns 202, and
  `GET /ui/scan-status` (htmx polling) shows progress and when the last scan finished
- [X] T044 [US1] Write `internal/web/settings.go` + `settings.html`. It shows the subnets discovered
  so far (read-only list with first seen and discovered by; there is no "add subnet" form), the
  too-large subnets skipped in each collector's latest run (read from `run_subnets`), the built-in
  scan interval, and the offline multiplier. Changes apply without a restart
- [X] T045 [US1] Write `internal/web/home.go` + `home.html`: counts (online/offline), subnet
  last-scanned and stale status, and a **Scan now** button. Make T031 pass
- [X] T046 [P] [US1] Write `internal/collect/arp_linux_hw_test.go` (`//go:build hwtest && linux`):
  a real ARP scan of the host's subnet finds at least the default gateway with a MAC
- [X] T047 [P] [US1] Write `deploy/Dockerfile`: a multi-stage build, `CGO_ENABLED=0`. The build
  stage MUST be `FROM --platform=$BUILDPLATFORM golang:1.26` and cross-compile with
  `GOOS=$TARGETOS GOARCH=$TARGETARCH`, because the CI runner has no QEMU emulation. The final stage
  only COPYs files (no `RUN`). Final image
  `gcr.io/distroless/static:nonroot` with the server binary,
  `HEALTHCHECK CMD ["/hne-server","healthcheck"]` (distroless has no curl), and `VOLUME /data`.
  Write `deploy/compose.yaml` with `network_mode: host`, `cap_add: [NET_RAW]`, `./data:/data`,
  and `restart: unless-stopped`, and **no subnet settings**. The binary needs `CAP_NET_RAW` as a
  file capability or the container must run as root. Pick `setcap` in the build stage and
  document it in comments. Also write `deploy/smoke.sh`. It MUST be POSIX `sh` (no bash-isms),
  use only `docker` and `curl`, and honour two variables: `HNE_IMAGE` (use this prebuilt image and
  skip the build) and `HNE_SMOKE_HOST` (default `127.0.0.1`). It runs the container with
  `-p 8080:8080 --no-builtin-scan` (not host networking), so it works in CI
  (`.github/workflows/ci.yml` job `image`). The script:
  1. builds the image (unless `HNE_IMAGE` is set),
  2. runs it on an empty temp volume and waits for healthy,
  3. completes `/setup`,
  4. restarts it on the same volume,
  5. checks it's healthy again and the password still works.

  This covers the constitution gate "starts with a fresh data volume" and FR-029

**Checkpoint**: US1 works on its own. Deploy to the NAS and run quickstart.md §3 (SC-001, SC-002,
SC-003).

---

## Phase 4: User Story 2 - Include subnets the NAS cannot see (Priority: P2)

**Goal**: a downloadable Windows collector scans the desktop's subnets and uploads authenticated,
idempotent runs. The server merges devices seen by several collectors into one record.

**Independent Test**: create the collector `desktop`, run `hne-collector.exe scan --once`, and
check that devices on subnets only the desktop sees appear within 1 minute without duplicates (quickstart.md
§4).

### Tests for User Story 2 ⚠️ write first, see them fail

- [ ] T048 [P] [US2] Write `internal/auth/token_test.go`. Cover:
  - `NewCollectorToken()` returns a 256-bit random token, base64url.
  - Only SHA-256 is stored in `collectors.token_hash`.
  - `Authenticate(token)` returns the collector, and fails for an unknown token or one whose
    `revoked_at` is set.
  - The comparison is constant-time.
- [ ] T049 [P] [US2] Write `tests/contract/server_upload_test.go` (httptest + temp store). Every
  `valid_*` fixture gets 201 `stored`, and re-posting it gets 200 `duplicate`. Other cases:
  - No, unknown, or revoked bearer → 401 `invalid_token`
  - Schema-invalid → 400 `validation_failed`
  - `schema_version` 2 → 422 `unsupported_schema`
  - Body > 2 MiB → 413
  - `GET /api/v1/ping` → 200 with `collector`, `server_time`, `supported_schema_versions` [1], and
    `ignored_subnets` (empty unless the owner has ignored subnets)
  - A subnet that is public or wider than /16 → 400 `validation_failed`

  Every response body must validate against `UploadResult` or `Error` in the OpenAPI file
- [ ] T050 [P] [US2] Write `tests/contract/collector_payload_test.go`. A collector built with
  `collecttest` fakes uploads to an httptest server that validates the request body against
  `CollectionRun` in the OpenAPI file and checks for the `Authorization: Bearer` header
- [ ] T051 [P] [US2] Write `internal/upload/spool_test.go`. The run is written to
  `spool/<collection_id>.json` before upload and deleted after 201/200. Pending files are listed
  oldest first. A partially written file (simulated crash) is ignored and cleaned up (use
  write-temp + rename)
- [ ] T052 [P] [US2] Write `internal/upload/client_test.go`. Backoff is exponential, "1 minute up to
  1 hour". A 401/403 stops retries and returns `ErrTokenRejected`. Network errors keep the file. A
  413/400 moves the file to `spool/rejected/` and logs the server's `detail`
- [ ] T053 [P] [US2] Write `internal/collect/engine_routed_test.go`. A target subnet that is not
  on-link uses method `icmp_tcp`: an ICMP echo and then TCP connect probes on ports 80, 443, 445,
  22, and 62078. Observations have no MAC, `method` is `icmp` or `tcp`, and the run still passes
  `contract.Validate`
- [ ] T054 [P] [US2] Write `internal/inventory/identity_weak_test.go`. Cover:
  - With no MAC, the identity is `host:<subnet>:<hostname>` when there is a hostname, otherwise
    `ip:<subnet>:<ip>`, with `identity_strength` `weak`.
  - When the same IP+hostname is later seen with a MAC, the weak device is folded into the
    MAC-keyed device (the weak one becomes `merged_away`).
  - The same MAC from collectors `nas` and `desktop` gives **exactly one** device with sightings
    from both (SC-006).
  - The same MAC in two subnets gives one device with two current `device_addresses`
    (multi-homed).
- [ ] T055 [P] [US2] Write `tests/integration/us2_collector_test.go`. Cover:
  - The owner creates the collector `desktop` via `POST /collectors`. The response shows the token
    once, and a `hne-collector.json` download with `server_url`, `name`, `token`, `subnets`, and
    `interval_seconds`.
  - Uploading `valid_full.json` with that token makes devices on its subnets appear on
    `/devices`.
  - `/collectors` shows the subnets and last report.
  - A `sent_at` skewed by 6 minutes flags the collector (threshold "exceeds 5 minutes").
  - **New subnet (SC-010)**: uploading `valid_new_subnet.json` (never-seen `10.20.30.0/24`) with no
    prior configuration stores it, returns it in `new_subnets`, auto-creates the `subnets` row
    (`ignored` false), shows its devices on `/devices`, and shows a "new subnet" notice on `/`.
    The skipped `10.0.0.0/16` gets **no** `subnets` row and is **not** in `new_subnets`; `/settings`
    lists it as skipped (too large) from the latest run.
  - After the owner ignores `10.20.30.0/24`: `GET /api/v1/ping` lists it in `ignored_subnets`. A
    later upload still containing observations for it is stored in full, but those observations
    don't appear in any device data, and the subnet is hidden from lists.
  - After revoke, uploads get 401.
- [ ] T056 [P] [US2] Write `cmd/hne-collector/main_test.go`. Config loading: the file
  `hne-collector.json` next to the binary, with env `HNE_SERVER_URL`/`HNE_TOKEN`/`HNE_SUBNETS`
  overriding it. Empty `subnets` (the default) means "discover every on-link **private** IPv4 subnet
  with a prefix of /22 or narrower at each scan". A non-private entry in `subnets` is a config
  error (exit 2). Exit codes must match contracts/collector-cli.md exactly: `check` → 0/2/3/4,
  `scan --once` → 0/5/4, and `--dry-run` prints valid JSON to stdout and writes no spool file

### Implementation for User Story 2

- [ ] T057 [P] [US2] Write `internal/auth/token.go`. Make T048 pass
- [ ] T058 [US2] Write `internal/web/api_upload.go`. `POST /api/v1/collections` uses
  `http.MaxBytesReader(2 MiB)` (exceeding it gives 413), bearer auth, decode, `contract.Validate`,
  and `Ingester.Ingest`, with status codes exactly as in the OpenAPI file. Add
  `GET /api/v1/ping`. Bearer auth applies to `/api/v1/*` only, with no session or Origin checks
  there. Make T049 pass
- [ ] T059 [US2] Extend `internal/ingest/ingest.go`. Compute `clock_skew_ms` and return it in
  `UploadResult`. Flag the collector when |skew| > 300000 ms. Add the skew assertion from T055
  to `internal/ingest/ingest_test.go` first. Subnet auto-creation, `new_subnets`, and
  ignored-subnet handling already exist from US1 (T023, T037), and T055 checks them over HTTP
- [ ] T060 [US2] Extend `internal/inventory/identity.go` and `internal/inventory/apply.go` with
  weak identity keys, fold-in of weak devices when the MAC becomes known, and multi-subnet
  `device_addresses`. Make T054 pass
- [ ] T061 [P] [US2] Write `internal/collect/arp_windows.go` (`//go:build windows`): a `Prober`
  using `iphlpapi.dll` `SendARP` via `golang.org/x/sys/windows` (`NewLazySystemDLL`). Also write
  `internal/collect/neighbor_windows.go`, which reads `GetIpNetTable2` (IPv4, skipping
  unreachable/incomplete states). No admin rights and no Npcap (research R3)
- [ ] T062 [P] [US2] Write `internal/collect/routes_windows.go` (`//go:build windows`): a
  `RouteReader` using `GetAdaptersAddresses` (interfaces, prefix, MAC) and `GetIpForwardTable2`
  (routes). It must report every IPv4 interface and route present, with nothing filtered by
  address. T069 checks the output against `Get-NetIPAddress`/`Get-NetRoute` on the real machine
- [ ] T063 [US2] Write `internal/collect/icmp_tcp.go`: the routed-subnet fallback. On Windows it
  uses `IcmpSendEcho` (iphlpapi, no admin). On Linux it uses `golang.org/x/net/icmp` privileged
  mode. TCP connect probes on the ports from T053 have a 500 ms timeout. Extend `engine.go` to
  pick `arp` or `icmp_tcp` per target. Make T053 pass
- [ ] T064 [US2] Write `internal/upload/spool.go` and `internal/upload/client.go`. Make T051 and
  T052 pass
- [ ] T065 [US2] Write `cmd/hne-collector/main.go`: the commands `check`, `scan --once [--dry-run]`,
  `run`, and `version`, plus the global `--json` flag. Before each scan it calls
  `GET /api/v1/ping` to fetch `ignored_subnets` (if unreachable, it uses the last known list).
  Human-readable output goes to stderr. Each
  summary line includes "subnets scanned, hosts found, run duration, and the upload result". In
  `run` mode it logs to `hne-collector.log`, rotating at 1 MiB × 3 files. Make T050 and T056 pass
- [ ] T066 [US2] Write `internal/web/collectors.go` + `collectors.html`. Routes: `GET /collectors`
  (name, kind, subnets, last report, skew flag, revoked), `POST /collectors` (name rule
  `[a-z0-9-]{1,64}`; the token is shown once along with the config download), and
  `POST /collectors/{id}/revoke`. The page shows the copy-paste `schtasks` command from
  contracts/collector-cli.md "Scheduling"
- [ ] T067 [US2] Write `internal/web/downloads.go`:
  `GET /downloads/hne-collector-{os}-{arch}[.exe]` serves files from `HNE_DOWNLOADS` (default
  `/app/downloads`), and returns 404 if they are missing. Update `deploy/Dockerfile` to build the
  collector for windows/amd64, linux/amd64, and linux/arm64 in the build stage and copy them to
  `/app/downloads`. Make T055 pass
- [ ] T068 [US2] Extend `internal/web/settings.go`, `settings.html`, and `home.html`. Show newly
  discovered subnets with a notice. Add **Rename** and **Ignore/Unignore** actions, written as
  `user_subnet_attrs` facts, and list skipped too-large subnets per collector. Ignoring takes
  effect on the collectors' next ping
- [ ] T069 [P] [US2] Write `internal/collect/arp_windows_hw_test.go`
  (`//go:build hwtest && windows`): `SendARP` to the default gateway returns a MAC, the run needs
  no elevation, and the `RouteReader` output matches the interfaces and routes Windows reports
  for the machine

**Checkpoint**: US1 and US2 both work. Run quickstart.md §4, including the negative checks.

---

## Phase 5: User Story 3 - Track what changed over time (Priority: P3)

**Goal**: change events, the new/acknowledged flow, a filterable timeline, per-device history,
manual merge/split, and correct handling of late uploads. History is kept indefinitely; there is no
retention setting (FR-024).

**Independent Test**: from a baseline, add a device, change an IP, and power off a device. All
three events appear on `/timeline` with correct times (quickstart.md §5).

### Tests for User Story 3 ⚠️ write first, see them fail

- [ ] T070 [P] [US3] Write `internal/inventory/events_test.go`. Scripted run sequences must emit:
  - Event types: `new_device`, `ip_changed` (old/new IP), `hostname_changed`, `mac_changed`,
    `went_offline`, `came_online`.
  - **IP reuse**: when an IP moves from MAC A to MAC B, both devices get `ip_changed` and A's
    address becomes `current` false.
  - **Late upload**: a run whose `observed_at` is earlier than already-ingested runs produces
    events at the **observation time**, not arrival time.
  - Re-applying the same run creates no duplicate events (unique on `device_id`, `type`, `at`,
    `new_value`).
- [ ] T071 [P] [US3] Write `internal/inventory/status_test.go`, covering the data-model.md state
  machine:
  - **Baseline**: devices in the first completed scan of a subnet → `online` (a `new_device`
    event is still recorded). A device first appearing in a later scan → `new`.
  - first observation after the baseline → `new`; ack → `online`
  - `new` + offline rule → `new_offline`; `new_offline` + seen → `new`
  - `online` ↔ `offline`; `merged_away` is terminal and hidden from lists
  - Ack appends to `user_acks`.
- [ ] T072 [P] [US3] Write `internal/inventory/merge_test.go`. Cover:
  - A user merge (`user_identity_alias` kind `merge`) of device B into A sets B to `merged_away`,
    moves its sightings to A, and emits `merged`.
  - A split restores B with its original identity_key sightings and emits `split`.
  - Aliases are applied **before** automatic matching, and they survive `store.Rebuild`.
- [ ] T073 [P] [US3] Write `internal/store/events_query_test.go`. `ListEvents` filters by `from`,
  `to`, `type` (repeatable), `device`, and `subnet`, ordered by `at` descending, with pagination
  (50 per page). `DeviceHistory(id)` returns sightings, events, and collectors ordered by time
- [ ] T074 [P] [US3] Write `internal/inventory/late_upload_test.go` (data-model.md "Monotonic
  device state"). Cover:
  - NAS records IP .20 for a device at 10:00. The desktop then uploads a 09:00 scan showing .10.
    Expect no `ip_changed` event, the current IP stays .20, and only a sighting is added.
  - An unknown device in a late scan gets `new_device` at its observation time.
  - A merge done between two scans, followed by `store.Rebuild`, gives data identical to the
    incremental result.
  - The friendly name stays on the right device after a rebuild, even when a late upload changes
    the order in which devices are first seen.
- [ ] T075 [P] [US3] Write `tests/integration/us3_history_test.go`. Through HTTP:
  - A baseline of 3 runs, then a run with a new device, an IP change, and a missing device,
    followed by 3 more runs. `/timeline` shows `new_device`, `ip_changed`, and `went_offline`
    with the right times. Filters work.
  - `/devices/{id}` shows the full history.
  - The home page lists "joined in last 7 days" (SC-009).
  - `POST /devices/{id}/ack` clears **new**.
  - Merge and split via HTTP work.
  - The rebuild invariant holds, including events and user facts.

### Implementation for User Story 3

- [ ] T076 [US3] Write `internal/inventory/events.go`. Emit events inside `Apply` (from sighting
  transitions) and inside `EvaluateStatus` (offline/online), using `INSERT ... ON CONFLICT DO
  NOTHING`. Make T070 pass
- [ ] T077 [US3] Write `internal/inventory/status.go`: the full state machine (`new`,
  `new_offline`, `online`, `offline`, `merged_away`), replacing the online/offline-only logic in
  `offline.go`. Make T071 pass
- [ ] T078 [US3] Write `internal/inventory/merge.go` and `internal/store/userfacts.go`: append-only
  writes to `user_identity_alias` and `user_acks`, keyed by `identity_key`. Apply aliases at the
  start of identity
  resolution, and re-apply them in `store.Rebuild`. Make T072 pass
- [ ] T079 [US3] Write `internal/store/events_query.go` (`ListEvents`, `DeviceHistory`). Make T073
  pass
- [ ] T080 [US3] Implement monotonic device state in `internal/inventory/apply.go` and
  `internal/inventory/events.go`. An observation older than the current value's timestamp for
  that subnet only extends or inserts a sighting, and never emits change events or rolls back
  current addresses or hostname. Make T074 pass
- [ ] T081 [US3] Write `internal/web/timeline.go` + `timeline.html` + `timeline_rows.html` (htmx
  partial). `GET /timeline` takes the filters from contracts/web-ui.md
- [ ] T082 [US3] Extend `internal/web/devices.go` and `device.html`. Add a history section
  (sightings + events + collectors), `POST /devices/{id}/ack`, `POST /devices/{id}/merge`
  (`into={id}`, with a picker that suggests devices sharing a hostname or flagged randomized), and
  `POST /devices/{id}/split`. Highlight **new** devices on `/devices`
- [ ] T083 [US3] Extend `internal/web/home.go` and `home.html` with a count of new devices, a
  "joined in last 7 days" list linking to the filtered timeline, and recent events. Make T075 pass

**Checkpoint**: US1–US3 work. Run quickstart.md §5.

---

## Phase 6: User Story 4 - Visualize how devices connect (Priority: P4)

**Goal**: a map of subnets as groups with devices inside them, gateways highlighted, bridges
between subnets, and manual "connected via" links that survive scans. There is no switch or
access-point querying (FR-028).

**Independent Test**: with both subnets in the inventory, open `/map`. Each device is in the right
subnet, the gateways and the desktop bridge are visible, and a manual link persists after a scan
(quickstart.md §6).

### Tests for User Story 4 ⚠️ write first, see them fail

- [ ] T084 [P] [US4] Write `internal/inventory/links_test.go`. Using the vantage from
  `valid_full.json`, cover:
  - Each route `next_hop` becomes a `gateway` link (device ↔ subnet), e.g. 192.168.1.100 ↔
    192.168.1.0/24 and 192.168.8.1 ↔ 192.168.8.0/24.
  - A device with current addresses in 2 subnets, or a collector with interfaces in 2 subnets,
    gets `bridge` links to both.
  - A user link (`user_links`) creates a `manual` link with source `user`, and "User links
    override inferred links that point at the same device".
  - A removed user link (`removed` true) disappears.
  - Links survive `store.Rebuild`.
- [ ] T085 [P] [US4] Write `internal/web/map_test.go`. `GET /ui/map.json?scope=online|all`
  returns JSON exactly in the shape from contracts/web-ui.md "Map JSON shape". Cover:
  - Subnet nodes have `stale`. Device nodes have `parent`, `status`, `type`, and `is_gateway`.
    Edges have `kind` ∈ {gateway, bridge, manual} and `origin` ∈ {inferred, user}.
  - `scope=online` excludes offline and new_offline devices, and drops their edges.
  - A multi-subnet device appears once, in its first subnet, with `bridge` edges to the others.
  - `merged_away` devices never appear.
- [ ] T086 [P] [US4] Write `tests/integration/us4_map_test.go`. Ingest NAS and desktop fixtures,
  `POST /devices/{id}/link via={ap}`, ingest another run, and `GET /ui/map.json` still contains the
  manual edge. `DELETE /devices/{id}/link` removes it

### Implementation for User Story 4

- [ ] T087 [US4] Write `internal/inventory/links.go`. Recompute inferred links in `Apply` from the
  run's vantage and the current `device_addresses`, and apply `user_links`. Write
  `internal/store/userfacts.go` `AddUserLink` and `RemoveUserLink` (append-only rows keyed by
  `from_identity_key`/`to_identity_key`, with the `removed` flag). Make T084 pass
- [ ] T088 [US4] Write `internal/web/map.go`: `GET /ui/map.json` and `GET /map`, plus
  `POST /devices/{id}/link` (`via={id}`) and `DELETE /devices/{id}/link`. Make T085 pass
- [ ] T089 [US4] Write `internal/web/templates/map.html` and `internal/web/static/map.js`. Draw
  Cytoscape compound nodes (subnet = parent), style gateways distinctly, dash `bridge` edges, and
  color `manual` edges separately. Add an **Online only / All** toggle that refetches
  `map.json?scope=...`. Clicking a device loads `/devices/{id}?partial=1` into a side panel via
  htmx, showing details and recent history (FR-027)
- [ ] T090 [US4] Add a "Connected via" picker to `device.html` (any device; `access_point`/`switch`/
  `router` types listed first). Make T086 pass

**Checkpoint**: all four stories work on their own. Run quickstart.md §6.

---

## Phase 7: Polish & Cross-Cutting Concerns

- [ ] T091 [P] Write `tests/integration/export_test.go` first, then `internal/web/export.go`.
  `GET /export` streams `hne-export-<YYYY-MM-DD>.json.gz` containing devices, user facts, events,
  sightings, and raw runs (base64 of `payload_gz`) (FR-032)
- [ ] T092 [P] Write `tests/integration/local_only_test.go`. Run the server, including the
  built-in scanner with fake probes but a **real** name resolver, with a `net.Dialer` hook that
  records every destination. Assert that every destination is a private (RFC 1918) address or
  `224.0.0.251:5353`, and that setting `HNE_DNS_SERVER=8.8.8.8` fails config. Also give the fake
  host a public interface, and assert it is never probed (SC-008, Principle I)
- [ ] T093 [P] Write `tests/integration/perf_test.go` (`-short` skips it). Seed projections for 250
  devices, about 1 year of sightings (≈ 10 changes per device), and ≈ 20k events, then assert that
  `/devices`, `/timeline`, `/devices/{id}`, and `/ui/map.json` each respond in < 2 s (SC-007).
  Add indexes in `internal/store/migrations/0002_indexes.sql` as needed
- [ ] T094 [P] Write `README.md`: what it is, the privacy guarantees, NAS deployment (compose), the
  desktop collector setup and `schtasks` scheduling, `hne-server rebuild`, and backup (copy
  `/data` or `/export`). Explain that subnets are discovered automatically and how to ignore one.
  State the accepted risk from research R8: tokens travel over plain HTTP on the LAN, can only
  upload, and can be revoked. Link to `specs/001-lan-inventory-topology/quickstart.md`
- [ ] T095 Security pass. Cover:
  - Every template output is auto-escaped (hostnames are attacker-controlled data from the LAN).
  - Responses include `Content-Security-Policy: default-src 'self'` with no inline scripts (move
    any inline JS to `static/`).
  - Responses include `X-Frame-Options: DENY`.
  - Tokens and passwords are never logged.

  Add `internal/web/security_test.go` asserting the headers and that the hostname
  `<script>alert(1)</script>` is rendered escaped
- [ ] T096 Run `make lint test test-contract test-integration` and `deploy/smoke.sh`, and fix
  anything flagged. Confirm the GitHub Actions CI run on `main` is green, including the `image`
  job. Then run the `hwtest` targets on the NAS and the desktop
- [ ] T097 Run quickstart.md §1–§7 end to end on the real NAS and desktop, including the
  new-subnet check in §4. Record the results (SC-001…SC-010) in
  `specs/001-lan-inventory-topology/checklists/validation.md`

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies.
- **Foundational (Phase 2)**: depends on Setup. It BLOCKS all stories.
- **US1 (Phase 3)**: depends on Foundational. It provides the projection applier, the collect
  engine, and the device pages that later stories extend.
- **US2 (Phase 4)**: depends on Foundational and on US1's `engine.go`, `apply.go`, and device
  pages (T036, T037, T042).
- **US3 (Phase 5)**: depends on US1 (T037, T038, T042). It does **not** need US2; its fixtures
  can be NAS-only.
- **US4 (Phase 6)**: depends on US1. It works best after US2 (two subnets), but its tests use
  fixtures and run without it.
- **Polish (Phase 7)**: after the stories you want to ship.

### Story order

```text
Setup → Foundational → US1 (MVP) ─┬─→ US2 ─┐
                                  ├─→ US3 ─┼─→ Polish
                                  └─→ US4 ─┘
```

### Within each story

The tests ([P], all in different files) are written first and must fail. Then:
identity/projection code → store queries → web handlers/templates → integration test passes →
checkpoint.

### Shared-file notes (tasks that are not [P] for this reason)

- `internal/inventory/apply.go`: T037 → T060 → T076/T077 → T080 → T087
- `internal/web/devices.go`, `device.html`: T042 → T082 → T090
- `internal/web/settings.go`: T044 → T068
- `deploy/Dockerfile`: T047 → T067
- `internal/store/userfacts.go`: T078 → T087

## Parallel Examples

### Phase 2

```text
T006 fixtures | T008 validate_test | T009 store_test | T010 owner_test | T011 web auth_test | T012 config_test | T013 ingest_test
then: T016 clock | T017 probe interfaces (in parallel with T014→T015, T018, T019, T020)
```

### User Story 1

```text
Tests:  T025 oui | T026 engine | T027 names | T028 apply | T029 offline | T030 devices_query | T031 integration
Impl:   T032 oui | T033 arp_linux | T034 routes_linux | T046 hw test | T047 Dockerfile   (all [P])
        then T035 → T036 engine, T037 → T038 inventory, T039 → T040 store, T041–T045 web
```

### User Story 2

```text
Tests:  T048–T056 all [P]
Impl:   T057 token | T061 arp_windows | T062 routes_windows | T069 hw test   ([P])
        then T058 → T059 server side;  T063 → T064 → T065 collector side;  T066–T068 UI
```

### User Story 3 / User Story 4

```text
US3 tests T070–T075 [P]  |  US4 tests T084–T086 [P]
Once US1 is done, US3 and US4 can proceed in parallel. They meet only at apply.go and devices.go
(see the shared-file notes).
```

## Implementation Strategy

### MVP first (User Story 1)

1. Phase 1 → Phase 2 → Phase 3.
2. **Stop and validate**: deploy to the NAS (T047), then run quickstart.md §3. This alone gives a
   working inventory of the NAS's own subnet(s), discovered automatically.

### Incremental delivery

1. MVP (US1): NAS-subnet inventory.
2. US2: the subnets only the desktop can see, plus automatic discovery of new subnets.
3. US3: change history and new-device awareness.
4. US4: the map.
5. Polish: export, the privacy test, performance, security headers, README.

## Notes

- [P] means different files with no dependency on unfinished tasks.
- Run every test task and confirm it **fails** before starting its implementation task
  (Constitution II).
- Commit after each task or logical group. The repository is not a git repo yet, so run
  `git init` first.
- Any new runtime dependency, extra service, or deviation from the plan needs a Complexity Tracking
  entry in plan.md.
