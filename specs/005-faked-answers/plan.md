# Implementation Plan: Faked Answers Defence

**Branch**: `005-faked-answers` | **Date**: 2026-10-13 | **Spec**: [spec.md](spec.md)

## Summary

The collector engine checks routed probing two ways: a canary probe of `192.0.2.1` (TEST-NET-1)
for subnets that use the default route, and a saturation rule (≥ 90% of ≥ 16 probed addresses
answering). A failing subnet is reported as `method = skipped`, `skip_reason = faked_answers`,
with no observations. The server accepts the new reason (migration 0006 widens the
`run_subnets.skip_reason` CHECK), and the Collectors page and home page show the latest run's
`faked_answers` subnets as scan problems.

## Technical Context

**Language/Version**: Go ≥ 1.26. **Dependencies**: none new. **Storage**: SQLite migration
`0006` rebuilds `run_subnets` (nothing references it) to allow `faked_answers`. **Testing**: CI
only; engine tests with `collecttest.FakeNetwork` (new `AnswerAll` mode, like the VPN), contract
fixture and schema test, migration harness (`seed_5.sql`), store test for scan problems,
integration test for the pages. **Version**: `0.6`.

## Design decisions

- **Canary only for default-route subnets.** The canary travels the default route; it proves
  nothing about a subnet with its own route. `collect` decides with the vantage routes: a subnet
  uses the default route when no non-default route contains it.
- **Canary computed lazily, once per scan**, only if some routed subnet needs it (no extra probe
  for ARP-only collectors).
- **Saturation buffer**: routed probing records into a local buffer and commits only if the
  subnet passes the rule.
- **Router fallback** probing goes through the same function, so it gets both checks.
- **Reporting as skipped** (not as an incomplete scan): skipped subnets already never count for
  offline detection, never create subnet rows, and validation already forbids observations in
  them. The reason is the "scan finished with an error" signal.
- **Server UI**: `store.ScanProblems` reads the latest run of each collector not removed;
  `CollectorOverview.Problems` for the Collectors page.
- **Deploy order**: server first (old servers reject the unknown reason with 400).

## Constitution Check

| Principle | Result |
|-----------|--------|
| I. Local-only | ✅ The canary is a single probe to a reserved address that never leaves a correct LAN path; no data is sent anywhere new |
| II. Test-first | ✅ Tests listed before implementation in tasks.md |
| III. Simplicity | ✅ One function in the engine, one migration, one store query |
| IV. Distributed collection | ✅ Additive v1 contract change (new skip reason), server deployed first |
| V. Faithful history | ✅ The raw run (with the skipped subnet) is stored; nothing fake is projected |

**Result**: PASS.

## Source changes

```text
internal/contract/v1.go, validate.go        SkipFakedAnswers
specs/001-.../contracts/collector-upload-api.yaml   skip_reason enum
tests/contract/fixtures/valid_faked_answers.json
internal/collect/engine.go                  canary + saturation for routed probing
internal/collect/collecttest/fake.go        FakeNetwork.AnswerAll; FakeRoutes.AddRoute
cmd/hne-collector/main.go                   warning log; check "network path" line
internal/store/migrations/0006_faked_answers.sql, testdata/seed_5.sql
internal/store/collectors.go                ScanProblems, CollectorOverview.Problems
internal/web/pages.go, collectors.go, templates/home.html, collectors.html
VERSION                                     0.6
```
