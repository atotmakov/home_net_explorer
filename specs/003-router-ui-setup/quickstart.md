# Quickstart & Validation: Router Setup from the Web UI

## 1. Automated checks (CI)

All run in GitHub Actions; nothing is built locally.

- Store: router settings saved, replaced, removed; address follows the device's current address;
  non-private addresses excluded; a merged device keeps its router.
- Web: the device page with a marker password never contains it; saving another type removes the
  router; the Collectors config lists routers without credentials.
- API: ping lists routers; the login endpoint answers only active collector tokens (401 for no,
  unknown and revoked tokens and for a browser session), with `Cache-Control: no-store`.
- Collector: routers from ping (and the cache when the server is down), login fetched per read,
  `login_unavailable`, rejection marker with the fetched login, precedence of file credentials,
  `check` output; the marker password appears in no file, output or upload.
- Integration: configure a router in the UI → an existing collector (fake router) reads it at the
  next scan → its devices appear; change the password → the next scan uses it.
- Migration 0004 keeps seeded v3 data.

## 2. On the real network

1. Deploy the server first: `.\deploy.ps1 -Pull` (footer `0.4.x`). Keep the desktop collector
   from 0.3 for now.
2. Open the ISP router's device page (192.168.0.1), set **Type** to `router`, save. In the
   **Router** card choose `huawei-hg8145v5`, enter the router username and password, save.
   **Expected**: "Password: set"; the password is not in the page (view source).
3. Download the new collector from **Collectors**, replace the old exe, and **remove** the
   `routers` entry with credentials from `hne-collector.json` (keep `extra_subnets`).
4. `hne-collector.exe check`. **Expected**:
   `huawei-hg8145v5 at 192.168.0.1 (login from server): OK, N online / M offline devices listed`.
5. `hne-collector.exe scan --once`. **Expected**: `router=ok`; devices as in feature 002.
6. Change the password in the UI to a wrong one, scan twice. **Expected**: the first scan says
   `login rejected`, the second `skipped` without contacting the router. Put the right password
   back in the UI and scan: `router=ok` (no `check` needed, research R6).
7. Search `hne-collector.json`, `hne-collector.routers`, the log and the spool for the password:
   not found (SC-002).
