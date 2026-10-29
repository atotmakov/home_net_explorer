# Contract change: upload API v1 (port checks)

Additive and optional; `schema_version` stays 1. Extends
`specs/001-lan-inventory-topology/contracts/collector-upload-api.yaml`. The server is deployed
before collectors that send the new fields.

## `POST /api/v1/collections`: `port_checks`

A *port run* (research R7) has `subnets: []`, `observations: []` and:

```json
"port_checks": [
  {
    "ip": "192.168.1.79",
    "mac": "ac:cc:8e:36:8e:e5",
    "started_at": "2026-10-27T03:12:00Z",
    "finished_at": "2026-10-27T03:12:41Z",
    "complete": true,
    "all_open": false,
    "open": [
      { "port": 80,  "proto": "tcp", "service": "http", "service_source": "table" },
      { "port": 22,  "proto": "tcp", "service": "ssh",  "service_source": "banner" },
      { "port": 554, "proto": "tcp", "service": "rtsp", "service_source": "table" },
      { "port": 5353, "proto": "udp", "service": "mdns", "service_source": "table" }
    ]
  }
]
```

Validation (rejected with `validation_failed`):

- at most 256 `port_checks` per run; at most 512 `open` entries per check;
- `ip` a private IPv4 address; `mac` empty or a unicast MAC;
- `started_at` ≤ `finished_at`, both inside the run's window;
- `port` 1–65535; `proto` `tcp` | `udp`; no duplicate (port, proto) in a check;
- `service` ≤ 32 printable characters; `service_source` `table` | `banner`;
- `all_open` only with `complete`.

A run may carry presence data and port checks together; collectors send them apart (R7).

## `GET /api/v1/ping`: `port_scan`

```json
"port_scan": {
  "enabled": true,
  "due": ["192.168.1.57", "192.168.1.79"],
  "excluded": ["192.168.1.54"]
}
```

- `enabled`: false when the owner turned port checks off everywhere (all subnets off).
- `due`: current addresses of due devices (research R5) on subnets the collector scanned in its
  latest run; may be empty. Collectors ask for it after uploading a presence run.
- `excluded`: addresses the collector must never check, even when forced (devices or subnets the
  owner turned off).

Collectors that do not know `port_scan` ignore it. A collector that gets no `port_scan` (older
server) checks nothing unless forced, and then sends `port_checks` that an older server rejects:
deploy the server first.
