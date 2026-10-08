# Quickstart & Validation: Router Device Lists

## 1. Automated checks (CI)

All of these run in GitHub Actions; nothing is built locally.

- Parser: the anonymized HG8145V5 capture decodes to 30 records, 13 online, with MACs, ports and
  hostnames; garbled pages give `page_not_understood`.
- Protocol: against a fake router, a full read logs in, lists, and **always logs out**; wrong
  password → `login_rejected`, then skipped until the config changes or `check` runs; locked and
  busy are reported, and the rest of the scan still uploads.
- Secrets (SC-004): a unique marker password never appears in stdout, stderr, the log, the spool
  or the upload.
- Contract: runs with `router_table` subnets/observations and `sources` validate against the
  schema; old runs still validate.
- Server: router observations fold weak devices into MAC devices (SC-002) and complete router
  reads drive offline detection (SC-003); the Collectors page shows the latest router outcome.
- Migration 0003 keeps seeded v2 data.

## 2. On the real network (desktop + ISP router)

1. Deploy the new server first: `.\deploy.ps1 -Pull` (footer shows `0.3.x`).
2. Download the new `hne-collector-windows-amd64.exe` from **Collectors**.
3. Add to `hne-collector.json`:
   `"extra_subnets": ["192.168.0.0/24"]` and a `routers` entry for `huawei-hg8145v5` at
   `http://192.168.0.1` with your **new** router password.
4. `hne-collector.exe check`. **Expected**: `192.168.0.0/24` listed via the router;
   `Routers: huawei-hg8145v5 at 192.168.0.1: OK, N online / M offline`. No password printed.
5. `hne-collector.exe scan --once`. **Expected**: summary `router=ok`. On **Devices**, every device
   the router shows as online on its *User Device Information* page appears with its MAC
   (SC-001), including phones that ignore ping. The earlier IP-only `192.168.0.*` records are gone
   (merged, SC-002).
6. Log into the router's own page right after the scan. **Expected**: login works (SC-006).
7. Put a wrong password in the config and run `scan --once`. **Expected**: exit code unchanged,
   other subnets uploaded, Collectors page shows "login rejected"; the next scan doesn't contact
   the router; `check` tries again once (SC-005, FR-011).
8. Hardware test: `go test -tags hwtest ./internal/collect/router/` with
   `HNE_HWTEST_ROUTER_URL`, `HNE_HWTEST_ROUTER_USER`, `HNE_HWTEST_ROUTER_PASS` set (on a machine
   with Go; optional).
