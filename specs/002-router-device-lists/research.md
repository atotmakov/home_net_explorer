# Research: Router Device Lists

**Feature**: `002-router-device-lists` | **Date**: 2026-10-09

Builds on feature 001 (`specs/001-lan-inventory-topology`). Facts marked "verified" come from
the read-only session against the owner's router on 2026-10-09 (firmware variant `MGTS2`).

## R1. HG8145V5 web protocol (verified)

- **Decision**: the collector talks to the router's own web interface, exactly as its login page
  does:
  1. `POST /asp/GetRandCount.asp` returns a one-time 48-hex-character token (with a UTF-8 BOM).
  2. Set cookie `Cookie=body:Language:english:id=-1`, then `POST /login.cgi` with form fields
     `UserName`, `PassWord` (Base64 of the password), `Language=english`, `x.X_HW_Token=<token>`.
     Success: `Set-Cookie: Cookie=sid=…:Language:english:id=1` and a body redirecting to
     `index.asp`. Failure: the body redirects back to the login page; the login page then carries
     `FailStat`, `LoginTimes` and `LockLeftTime` variables (used to tell "wrong password" from
     "locked").
  3. `GET /html/bbsp/common/GetLanUserDevInfo.asp` returns JavaScript with
     `var UserDevinfo = new Array(new USERDevice("…", …), …, null)`. Strings use `\xNN`
     escapes (e.g. `:` is `\x3a`). The file holds three array variants selected by
     `ProductType` and `isRealmac`; this firmware uses `USERDevice` (`isRealmac = 0`), whose
     arguments are `Domain, IpAddr, MacAddr, Port, IpType, DevType, DevStatus, PortType, Time,
     HostName, IPv4Enabled, IPv6Enabled, DeviceType, UserDevAlias, UserSpecifiedDeviceType,
     LeaseTimeRemaining`. `USERDeviceNew` adds `RealMacAddr` after `MacAddr`.
  4. Logout: `POST /logout.cgi?RequestFile=html/logout.html` with `x.X_HW_Token=<onttoken>`,
     where `onttoken` is a hidden input on `index.asp`. The router also ends idle sessions
     within minutes (observed), so a missed logout is not fatal, but the collector always tries.
- **Parsing rule**: evaluate the same selection the page does: read `ProductType` and
  `isRealmac`, pick the matching array, decode `\xNN`, split arguments. Anything that doesn't
  match this shape is "page not understood" (spec edge case: firmware changes).
- **Verified data**: 30 records (13–14 online during the session), hostnames untruncated, `Port`
  `LAN1`/`LAN2` (Wi-Fi clients use `SSIDn`, `PortType` `WIFI`), `IpType` `DHCP`/`Static`,
  `DevType` = DHCP vendor class (device model strings), `Time` = connection duration `h:m`, plus
  a static `0.0.0.0` entry (ignored by FR-008).
- **Alternatives considered**: SNMP and TR-064 (not offered to customers on this ISP firmware);
  TR-069 (ISP-side only); the DHCP lease page `html/bbsp/dhcp/dhcp.asp` (no online status, no
  port).

## R2. Where router reads run

- **Decision**: in the **collector**, as an extra source next to its probes, configured per
  collector. The desktop collector is the one that can reach the ISP router (spec assumption);
  the NAS collector can use it too if it can reach a router.
- **Rationale**: credentials must stay on the collector machine (FR-006, decision A), and the
  server never needs to reach routers (Principle IV: the server merges observations; collectors
  observe).
- **Alternatives considered**: server-side router polling (would put the router password on the
  NAS and need the NAS to reach `192.168.0.1`, which it may not).

## R3. Contract changes (additive, schema version stays 1)

- **Decision**: extend the v1 upload contract additively:
  - `SubnetScan.method` gains `router_table`: the subnet was covered by a router's device list.
    `complete: true` only when the read succeeded, so it counts for offline detection (FR-009,
    FR-010).
  - `Observation.method` gains `router_table` (MAC required, like `arp`);
    `Observation.hostname_source` gains `router`; new optional `Observation.via` (≤ 32 chars):
    how the router says the device is connected (`LAN1`, `SSID2`, …) for the future map
    (FR-014).
  - New optional run field `sources`: one entry per configured router with `model`, `address`,
    `outcome` (`ok`, `unreachable`, `login_rejected`, `locked`, `session_busy`,
    `page_not_understood`, `skipped_after_rejection`), `online`, `offline` counts. No username,
    no password (FR-006, FR-013).
- **Compatibility**: old servers reject the new enum values, so the **server is upgraded first**
  (normal deploy order: NAS image, then collectors, which the NAS serves). New servers accept old
  collectors unchanged. This is recorded in the contract description; a major bump is not needed
  because nothing existing changes meaning (Principle IV).
- **Alternatives considered**: reporting router devices as `arp` observations (would lie about
  how they were seen and hide router failures); a separate `/api/v1/router-lists` endpoint (two
  upload paths for the same thing).

## R4. One subnet, two sources in the same run

- **Decision**: a run lists each subnet once. If a router covers a subnet and its read
  **succeeds**, that subnet is reported as `router_table` and is **not** also ICMP/TCP-probed in
  that run (the router list is better and includes non-pinging devices). If the read **fails**
  and the subnet is also an `extra_subnet`, the collector falls back to the ICMP/TCP probe for it
  in the same run (method `icmp_tcp`). On-link subnets are always ARP-scanned; if a router also
  covers an on-link subnet, its observations are merged into that ARP entry (one subnet entry,
  method `arp`, both kinds of observation).
- **Rationale**: keeps the v1 rule "a subnet appears once per run" and the offline semantics
  simple.

## R5. Which subnet a router serves

- **Decision**: the router's address with a `/24` prefix by default (`192.168.0.1` → 
  `192.168.0.0/24`), overridable per router with `subnet`. Must be private and /16–/30 like any
  subnet (FR-005).
- **Rationale**: home routers almost always serve a /24; reading the router's LAN settings page
  adds a second fragile parser for little gain.

## R6. Credentials and lockout (FR-003, FR-006, FR-011)

- **Decision**:
  - Stored as plain text in `hne-collector.json` under `routers[]` (owner decision A, spec
    FR-006). The config struct marks the password as secret: its `String()`/log form is
    `"***"`, and it is never copied into a run, a log record or `check` output.
  - After a `login_rejected` or `locked` outcome, the collector writes
    `hne-collector.router-rejected` (next to the config) holding a hash of that router's
    configuration. While the hash matches, later scans skip the router (`sources[].outcome =
    skipped_after_rejection`). Editing the router config changes the hash; `hne-collector check`
    deletes the marker and tries once more (FR-011).
- **Alternatives considered**: retry with backoff (still risks locking the router, which on this
  model can block the owner's own login for minutes).

## R7. `extra_subnets` (FR-015 – FR-017)

- **Decision**: new collector config `extra_subnets` (and env `HNE_EXTRA_SUBNETS`). Planning:
  auto-discovered (or fixed `subnets`) targets **plus** extras; an extra that is on-link becomes an
  ARP target once; others become `icmp_tcp` targets. Same validation as `subnets` (private,
  /16–/30). `check` lists them with their method.

## R8. Server side

- **Decision**:
  - Ingest stores `sources` in a new `run_sources` table (one row per router per run), so the
    Collectors page can show each collector's latest router outcome (FR-013) without re-reading
    payloads.
  - Projections: `router_table` observations identify devices by MAC like `arp` (no inventory
    rule changes); `sightings` gains `via` (part of the sighting tuple, so a move from `LAN1` to
    `LAN2` starts a new sighting).
  - Migration `0003` adds both. It is tested by the migration harness with seed rows at v2.
- **Version**: `VERSION` becomes `0.3` with this feature.

## R9. Testing

- Parser: the anonymized capture `hg8145v5_getlanuserdevinfo.fixture.asp` (no real MACs,
  hostnames or model strings) plus hand-made variants (`isRealmac = 1`, empty list, garbled page).
- Protocol: a fake HG8145V5 (`httptest`) implementing GetRandCount/login/data/logout with
  scripted failures (wrong password, locked, session busy, garbage page), asserting logout is
  always called after a successful login.
- Secrets: a collector run with a fake router whose password is a unique marker string; assert
  the marker is absent from stdout, stderr, the log file, spool files and the uploaded body
  (SC-004).
- Hardware: `-tags hwtest` reads the real router when `HNE_HWTEST_ROUTER_URL/USER/PASS` are set.
