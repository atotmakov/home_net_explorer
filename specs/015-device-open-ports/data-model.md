# Data Model: Open Ports per Device

## Migration 0010 (`0010_ports.sql`)

```text
port_checks
  id            INTEGER PK
  collection_id TEXT  → collection_runs (the port run)
  collector_id  INTEGER → collectors
  device_id     INTEGER → devices          (resolved at apply time, R10)
  subnet_id     INTEGER → subnets
  ip            TEXT
  started_at    TEXT
  finished_at   TEXT
  complete      INTEGER 0/1                (1: every TCP port and the UDP services answered or timed out)
  all_open      INTEGER 0/1                (more than 512 open: list cut, R2)
  INDEX (device_id, finished_at)

port_findings
  check_id       INTEGER → port_checks
  port           INTEGER 1–65535
  proto          TEXT 'tcp' | 'udp'
  service        TEXT ('' = unknown)
  service_source TEXT 'table' | 'banner'
  PK (check_id, port, proto)

device_ports                                (derived, R9)
  device_id     INTEGER → devices
  port          INTEGER
  proto         TEXT
  service       TEXT
  state         TEXT 'open' | 'closed'
  ip            TEXT                        (address of the latest finding)
  collector_id  INTEGER                     (collector of the latest finding)
  first_open    TEXT
  last_open     TEXT
  closed_at     TEXT ('' while open)
  PK (device_id, port, proto)
```

Rebuilt tables (CHECK changes, R6):

- `events.type` adds `port_opened`, `port_closed` (`new_value` = `<port>/<proto>`).
- `user_device_attrs.field` adds `ports`; `user_subnet_attrs.field` adds `ports` (`value` `on` |
  `off`; latest row wins; absent = on).

## Entities

- **Port check** (`port_checks` + `port_findings`): one check of one address by one collector in
  one port run, with its open ports. Stored for every check in the run, complete or not.
- **Device port** (`device_ports`): the derived state per device and (port, proto).
- **Port setting**: the owner's `ports` fact on a device or a subnet.
- **Port plan** (not stored): what a collector must check now: due addresses and excluded
  addresses (R5), computed on each `ping` and each built-in scan.

## Rules

- A check's device: by MAC, else the device holding `ip` on `subnet` at `started_at`; no device →
  the check is dropped (R10).
- Only a `complete` check may close a port (FR-005). `all_open` checks are complete.
- Opened: a finding for (device, port, proto) with no `device_ports` row, or with state `closed` →
  state `open`, `first_open` (first time only) and `last_open` set, event `port_opened` at the
  check's `finished_at`.
- Still open: `last_open`, `ip`, `collector_id`, `service` updated.
- Closed: a complete check by collector C of the device without (port, proto) closes it if no other
  collector's latest complete check of the device still has it open → state `closed`, `closed_at`,
  event `port_closed`.
- Due (R5): ports on for the device and its subnet; and (no complete check) or (last complete check
  ≤ now − 20 h and minute-of-day(now) ≥ slot(identity key)).
- Merge: a merged device's rows move to the surviving device on rebuild (`merged_into`).
- Removal (feature 004): deleting a device deletes its `port_checks`, `port_findings` and
  `device_ports`; "remove all" clears the three tables.

## Projection rules

`ProjectionRules` = 9: the derived port tables and port events are rebuilt from the stored runs.
