# Quickstart & Validation: Maintenance Actions

## 1. Automated checks (CI)

All run in GitHub Actions; nothing is built locally.

- Migration 0005 keeps seeded v4 data (ids, runs, revoked collector) and passes
  `foreign_key_check`; names are unique among active collectors only.
- Store: each action removes exactly the rows of the data-model table, in one transaction, and
  reports its counts; after each, a rebuild from the remaining facts equals the live projections.
- Ingest: a run started before the reset point (server time, skew-corrected) is answered
  `discarded` and not stored; a later run is stored.
- Subnets: names and ignore choices kept by "remove all devices" apply again when the subnet is
  rediscovered, live and after a rebuild; `ping` lists ignored subnets right after the reset.
- Scanner (fake clock): no scan while paused, including at start-up; "Scan now" refused; a scan in
  progress finishes; resume scans within one interval; the pause survives a new `app.New` on the
  same data directory.
- Web/integration: every route refuses a missing or wrong confirmation, no session and a
  cross-site request with nothing changed; removed tokens get `invalid_token` on upload, ping and
  router login; a removed name can be reused; "drop all data" keeps the owner logged in and the
  password valid, with default settings and the scanner running.
- Contract: `UploadResult.status` enum includes `discarded`.

## 2. On the real network

Back up the data volume first if you want to keep the current inventory: these actions cannot be
undone.

1. Deploy: `.\deploy.ps1 -Pull` (footer `0.5.x`).
2. **Pause**: Settings → Maintenance → **Pause**. **Expected**: home page shows "Paused" with
   Resume; Collectors shows `nas` as paused; after more than one scan interval, the NAS has not
   scanned (Collectors "Last report" unchanged). Restart the container
   (`.\deploy.ps1` again): still paused.
3. Run the desktop collector (`hne-collector.exe scan --once`): **Expected** `result=stored`, its
   devices update as usual.
4. **Remove all devices**: type `devices`, submit. **Expected**: notice with counts; Devices page
   empty; Settings subnets list empty, then after the next desktop scan the subnets come back with
   their names, ignored subnets still ignored, devices back without names/types; router card
   needs to be set up again (feature 003).
5. **Resume**: **Expected**: the NAS scans within one interval; "Scan now" works.
6. **Remove all collectors**: type `collectors`. **Expected**: only `nas` listed; the desktop
   collector's next run fails with `invalid_token`; devices unchanged; create a collector with the
   desktop's old name, download the new config, and the desktop works again.
7. **Drop all data**: type `everything`. **Expected**: still logged in; Devices, Collectors (only
   `nas`), Settings subnets all empty; scan interval and offline threshold back to defaults; the
   NAS scans again and rediscovers its subnets.
8. Submit any destructive form with an empty or wrong word: **Expected**: "Type … to confirm.",
   nothing removed.
