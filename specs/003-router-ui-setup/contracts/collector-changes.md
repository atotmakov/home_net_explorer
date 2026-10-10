# Contract change: hne-collector

Extends `specs/002-router-device-lists/contracts/collector-config-and-cli.md`.

## Configuration

```json
"routers": [
  { "model": "huawei-hg8145v5", "url": "http://192.168.0.1" }
]
```

- An entry **without** `username` and `password` is server-managed and informational (research
  R7). An entry with only one of them is a config error (exit 2).
- An entry **with** both works as in feature 002 and wins over a server router with the same
  address for this collector.

## Each scan

1. `GET /api/v1/ping` → `routers` (saved to `hne-collector.routers`; on failure the cache is used).
2. For each server router not overridden by the file: `GET /api/v1/routers/{id}/login`, then the
   feature 002 read (marker check uses the fetched login, research R6). The login is dropped after
   the read.
3. Outcome `login_unavailable` when the login can't be fetched; the rest of the scan continues.

## `check` output

```text
Routers:
  huawei-hg8145v5 at 192.168.0.1 (login from server): OK, 14 online / 16 offline devices listed
  huawei-hg8145v5 at 10.20.30.1 (login from config): unreachable
```

New phrase: `login unavailable from the server`. Credentials are never printed.

## Files

- `hne-collector.routers`: cached router list (no credentials).
- Router logins from the server are never written to any file.
