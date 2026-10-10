# Contract change: hne-collector configuration and CLI

Extends `specs/001-lan-inventory-topology/contracts/collector-cli.md`.

## Configuration (`hne-collector.json`)

```json
{
  "server_url": "http://192.168.1.100:8181",
  "name": "desktop",
  "token": "<shown once>",
  "subnets": [],
  "extra_subnets": ["192.168.0.0/24"],
  "interval_seconds": 900,
  "routers": [
    {
      "model": "huawei-hg8145v5",
      "url": "http://192.168.0.1",
      "username": "root",
      "password": "<router password>",
      "subnet": "192.168.0.0/24"
    }
  ]
}
```

- `extra_subnets`: private subnets scanned **in addition to** auto-discovery (or to `subnets`).
  Env: `HNE_EXTRA_SUBNETS` (comma-separated). Non-private → config error (exit 2).
- `routers`: opt-in router sources. `subnet` is optional (default: router address /24). Invalid
  model, non-private URL host or missing credentials → config error (exit 2).
- The password is stored as plain text in this file (owner decision, spec FR-006). Keep the
  folder private.

## `check` output additions

```text
Subnets to scan:
  192.168.0.0/24     router huawei-hg8145v5 at 192.168.0.1 (fallback: ICMP/TCP)
  192.168.1.0/24     on-link via home: ARP
  192.168.8.0/24     on-link via internet: ARP
Routers:
  huawei-hg8145v5 at 192.168.0.1 (login from config): OK, 14 online / 16 offline devices listed
```

Feature 003 adds the `(login from config)` / `(login from server)` label, routers set up in the
web UI (listed by the server at each scan, login fetched before each read), entries without
credentials in `routers[]`, the cache file `hne-collector.routers` and the phrase
`login unavailable from the server`. See `specs/003-router-ui-setup/contracts/collector-changes.md`.

Possible router results: `OK, N online / M offline devices listed`, `unreachable`,
`login rejected (router skipped until its config changes or you run check)`, `locked by the
router`, `busy (someone is logged into the router)`, `page not understood (firmware?)`.
Credentials are never printed. `check` clears the rejection marker and tries the router once.

## `scan` / `run`

The summary line adds `router=<outcome>`. Exit codes are unchanged: a router failure never
changes the exit code (the scan and upload still happen, FR-010).
