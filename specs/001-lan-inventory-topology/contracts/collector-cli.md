# Contract: `hne-collector` CLI

The remote collector is a single file (`hne-collector.exe` on Windows, `hne-collector` on Linux)
that the user downloads from the server's **Collectors** page. No installation and no admin rights
are needed (research R3).

## Configuration

The config file is `hne-collector.json`, placed next to the binary. The server generates it when a
collector is created in the UI, and the user downloads it together with the binary.

```json
{
  "server_url": "http://192.168.1.x:8080",
  "name": "desktop",
  "token": "<shown once>",
  "subnets": [],
  "interval_seconds": 900
}
```

- `subnets` is **empty by default**, meaning: at the start of every scan, discover every on-link
  **private (RFC 1918)** IPv4 subnet with a prefix of /22 or narrower. Wider on-link subnets are
  reported as `skipped` / `too_large`. Non-private subnets are never scanned. A subnet that
  appears later (a new adapter, VLAN, or router) is picked up on the next scan with no config
  change (research R14).
- A non-empty `subnets` list restricts scanning to those private subnets. Listed subnets that are
  not on-link use the `icmp_tcp` fallback. Non-private entries are a config error (exit 2).
- Subnets returned in `ignored_subnets` by `GET /api/v1/ping` are skipped (`skip_reason`
  `ignored`).
- Environment variables `HNE_SERVER_URL`, `HNE_TOKEN`, and `HNE_SUBNETS` override the file.
- Feature 002 adds `extra_subnets` (env `HNE_EXTRA_SUBNETS`): private subnets scanned **in
  addition to** auto-discovery or `subnets`, and `routers`: opt-in router sources (model, URL,
  username, password, optional subnet) whose device lists are read at every scan. The router
  password is stored only in this file and is never logged, printed or uploaded. See
  [002 collector-config-and-cli.md](../../002-router-device-lists/contracts/collector-config-and-cli.md).

## Commands

| Command | Behavior | Exit code |
|---------|----------|-----------|
| `hne-collector check` | Prints the interfaces and routes it can see, which subnets are on-link or routed, and the result of `GET /api/v1/ping` | 0 ok; 2 config error; 3 server unreachable; 4 token rejected |
| `hne-collector scan --once` | Runs one collection, saves it to the spool, uploads everything pending, then exits | 0 uploaded; 5 scan done but upload deferred (spooled); 4 token rejected |
| `hne-collector run` | Repeats `scan` every `interval_seconds` until stopped (Ctrl-C or a service stop). Retries pending uploads with backoff | 0 on clean stop |
| `hne-collector scan --once --dry-run` | Scans and prints the JSON payload to stdout, without spooling or uploading | 0 |
| `hne-collector version` | Prints the collector version and the supported `schema_version`s | 0 |

## Output

- Human-readable progress goes to stderr. `--json` switches to one JSON object per line, for
  scripting.
- Every summary line includes: subnets scanned, hosts found, run duration, and the upload result
  (`stored`, `duplicate`, or `spooled`). With routers configured it also has `router=<outcome>`
  (comma-separated for several routers). A router failure never changes an exit code.
- `check` lists router subnets (`router <model> at <address>`, with `(fallback: ICMP/TCP)` when
  the subnet is also an extra subnet) and a `Routers:` section with each router's result
  (feature 002).

## Files

- `spool/<collection_id>.json`: runs that have not been uploaded yet. Each file is deleted after a
  201/200 response. If a 401 is received, the file is kept and the collector stops with exit code 4.
- `hne-collector.log`: the rotating log for `run` mode (1 MiB × 3 files).
- `hne-collector.router-rejected` (feature 002): SHA-256 hashes of router configs whose login was
  rejected or locked. Those routers are skipped until their config changes or `check` runs (which
  deletes the file and tries once).

## Scheduling (documentation only; no built-in installer, per Principle III)

On Windows, a Task Scheduler task runs `hne-collector.exe run` "At log on", with "Run whether user
is logged on or not" left unchecked. The Collectors page shows a copy-paste `schtasks` command.
