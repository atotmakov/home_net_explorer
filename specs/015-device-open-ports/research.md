# Research: Open Ports per Device

## R1 Check technique: TCP connect

- **Decision**: a TCP *connect* to each port (`net.Dialer`, 1 s timeout), closed at once. Connected
  = open; refused (reset) = closed; timeout or unreachable = no answer (filtered), treated as not
  open. No retries: a silent port stays silent.
- **Rationale**: needs no raw sockets, so it works for the Windows collector without Npcap and in
  the NAS container without extra capabilities. It is the least intrusive scan type a device can
  see: a normal connection attempt (001 FR-007).
- **Alternatives**: SYN ("half-open") scan: faster and quieter in logs, but needs raw sockets
  (Npcap on Windows, `CAP_NET_RAW` in the container) and crafted packets; not worth it at ~30
  devices once a day. An external scanner (nmap): a new deployable dependency (Principle III).

## R2 Load limits and timing

- **Decision**: per device at most 128 connection attempts in flight; at most 4 devices checked at
  once; ports in ascending order. A device that resets closed ports (most LAN devices) takes well
  under a minute; a firewalled device (every attempt times out) takes 65,535 / 128 × 1 s ≈ 9
  minutes, so with 4 in parallel a forced check of ~30 devices fits in 30 minutes only if few are
  firewalled. To bound that, a device whose first 1,024 ports all time out is checked from then on
  with a 300 ms timeout (silent devices on a LAN answer in well under 10 ms when they answer at
  all), which brings a firewalled device to about 3 minutes.
- **Device that answers on every port**: if more than 512 ports are open, the check stops listing
  them and is marked `all_open` (spec edge case "answers on all ports"); it still counts as complete.
- **Windows**: a refused connection uses no TIME_WAIT slot; only the few open ports do, so the
  ephemeral port range is not exhausted.
- **Alternatives**: one global rate (packets per second): simpler to reason about for the network
  but a single fragile device would still get the whole rate; the per-device cap protects it.

## R3 UDP

- **Decision**: UDP is checked only for services that answer a standard, credential-free request:
  DNS 53 (a query for the root NS), NTP 123 (client request), NetBIOS 137 (node status), SSDP 1900
  (unicast M-SEARCH), mDNS 5353 (unicast query, as names.go does). Any reply = open.
- **Not included**: SNMP 161: every request carries a community string, which is a credential
  guess ("public"), excluded by 001 FR-007. Other UDP ports: silence cannot tell open from
  filtered, so a blind sweep would report nothing reliable.

## R4 Service names

- **Decision**: a table in the collector (`internal/collect/ports/services.go`) of well-known and
  common home-network ports: the IANA names for the usual services (ssh 22, http 80, https 443,
  smb 445, …) plus devices found at home (rtsp 554, ipp 631, jetdirect 9100, afp 548,
  synology-dsm 5000/5001, http-alt 8080, https-alt 8443, mqtt 1883, plex 32400, googlecast 8009,
  upnp 49152, …). An unknown port is reported with an empty name and shown as "unknown".
- **Device reply**: on an open TCP port the checker reads, for at most 500 ms and without sending
  anything, the greeting a server sends first (SSH, FTP, SMTP, POP3, IMAP, Telnet). Only the
  service name derived from it is kept (`SSH-` → ssh), never the text (it can carry versions); the
  source is `banner`, else `table`.
- **Alternatives**: sending an HTTP request to every open port to tell http from other services:
  more traffic and closer to fingerprinting; out of scope (spec Assumptions).

## R5 Who decides what is due

- **Decision**: the server. A device is due when port checks are on for it and its subnet and
  either no collector has completed a check of it, or its last complete check is at least 20 hours
  old and the current time of day is past its slot. The slot is a minute of the day derived from a
  hash of the device's identity key (FNV-1a mod 1440), which spreads the devices over the day
  (FR-008). The server lists due addresses to each collector in `ping` (`port_scan.due`), with the
  addresses it must never check (`port_scan.excluded`: owner turned them off).
- **Rationale**: collectors must stay stateless (Principle IV) and the server already knows every
  device's history; ping already carries the server's instructions (ignored subnets, routers,
  access points).
- **Built-in collector**: reads the same plan from the store directly.

## R6 Storage and history

- **Decision**: migration 0010:
  - `port_checks`: one row per address checked in a port run (collector, device, ip, subnet,
    started/finished, complete, all_open).
  - `port_findings`: the open ports of a check (port, proto, service, service_source).
  - `device_ports`: derived per device and (port, proto): state, service, first/last open, the
    address and collector of the last finding, closed_at.
  - `events.type` gains `port_opened` and `port_closed` (new_value `80/tcp`); `user_device_attrs`
    and `user_subnet_attrs` gain the field `ports` (`on`/`off`). SQLite cannot change a CHECK, so
    these three tables are rebuilt in the migration, as 0008 rebuilt `run_sources`.
- **Rationale**: closed ports are not stored (65,535 rows per check); a complete check with its open
  list is enough to know every other port was closed at that time (Principle V).

## R7 Port runs: a separate upload

- **Decision**: a port job runs in the background after a presence run and is uploaded as its own
  v1 run: new collection id, `subnets` and `observations` empty, `port_checks` filled. At most one
  port job per collector at a time; a job still running when the next presence run ends is not
  started twice. A job stops itself before the 1-hour run window (`MaxRunWindow`): addresses not
  finished are reported with `complete: false`.
- **Rationale**: the presence scan keeps its 15-minute rhythm (FR-010); the upload, spool, token
  and duplicate handling are reused unchanged; older servers reject the unknown field only if
  validation is strict, so the server is deployed first (as for 012).
- **Server**: a run without subnets skips the presence steps (no offline evaluation, no subnet
  freshness); it still counts as the collector's latest upload.

## R8 Forcing a check now

- **Decision**: `hne-collector scan --once --ports` runs the presence scan, then a port job over
  every address it found on directly scanned subnets (minus excluded), in the foreground, and
  uploads both runs. On the server, the home page gets "Check ports now" (POST `/scan/ports`): the
  built-in collector's next scan starts at once and its port job covers every address found. While
  a port job runs, the home page shows "Checking ports: n of m devices" and the button answers 409.
- **Alternatives**: a server-side "force" in ping for every collector: not asked for; the desktop
  is forced from its own command line.

## R9 Open, previously open, closed

- **Decision**: for each device and (port, proto):
  - *open* when the latest complete check of the device's address by some collector found it open;
  - *closed* when every collector that last found it open has since completed a check without it
    → `closed_at` = time of that check, event `port_closed`;
  - an incomplete check changes nothing (FR-005);
  - a port found open for the first time, or again after being closed, → event `port_opened`.
- The device page shows open ports, then "previously open" (closed, with last seen open). For an
  offline device the ports are labelled "last known" (its state comes from feature 006).

## R10 Identity, rebuild, removal

- **Decision**: a port check carries the address and the MAC from the presence observation; the
  server finds the device by MAC, else by the device holding that address on that subnet at that
  time; a check matching no device is dropped. Port runs are runs, so `Rebuild` replays them in time
  order with the others; `port_checks`, `port_findings`, `device_ports` and the port events are
  derived and cleared before a rebuild. `ProjectionRules` goes to 9 so the first start-up rebuilds
  once (no port data yet, but the event table is rebuilt by the migration). Removing devices or all
  data (feature 004) deletes their port rows. Merged devices' ports follow `merged_into`.
