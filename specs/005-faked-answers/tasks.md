---

description: "Task list for Faked Answers Defence"
---

# Tasks: Faked Answers Defence

**Input**: `specs/005-faked-answers/` (spec.md, plan.md)

**Tests**: REQUIRED (Principle II): tests first, red in CI, then implementation.

## Phase 1: Setup

- [X] T001 Set `VERSION` to `0.6`

## Phase 2: Foundational (contract and storage)

- [X] T002 [P] Tests: `tests/contract/fixtures/valid_faked_answers.json` (a run with a routed
  subnet `method: skipped`, `skip_reason: faked_answers`, no observations in it) validates against
  the schema and `contract.Validate`; `tests/contract/schema_test.go` checks an unknown reason
  still fails; `TestMigration0006` in `internal/store/store_test.go` with `testdata/seed_5.sql`:
  seeded rows survive, `run_subnets` accepts `faked_answers` and still rejects `bogus`
- [X] T003 `contract.SkipFakedAnswers = "faked_answers"` accepted by `Validate`; OpenAPI enum and
  description; migration `0006_faked_answers.sql` (rebuild `run_subnets` with the wider CHECK).
  Make T002 pass

## Phase 3: User Story 1 - Fake answers never become devices (P1) 🎯 MVP

- [X] T004 [P] [US1] Tests in `internal/collect/engine_faked_test.go`: `AnswerAll` network → a
  routed default-route subnet is skipped `faked_answers`, 0 observations, and is not probed (only
  the canary); a network answering 240 of 254 routed addresses but not the canary → skipped by the
  90% rule; a subnet with its own route is probed even when the canary answers; on-link ARP
  subnet unaffected; router fallback (failed read) with `AnswerAll` → skipped `faked_answers`;
  normal routed scan unchanged; the run validates
- [X] T005 [US1] Implement in `internal/collect/engine.go` (`collecttest`: `FakeNetwork.AnswerAll`,
  `FakeRoutes.AddRoute`). Make T004 pass

## Phase 4: User Story 2 - The owner sees the failed scan (P1)

- [X] T006 [P] [US2] Tests: `internal/store/collectors_test.go` `TestScanProblems` (latest run
  only; a later normal run clears it; removed collectors excluded; `CollectorOverviews` carries
  `Problems`); `tests/integration/faked_answers_test.go`: upload `valid_faked_answers.json`, the
  Collectors page shows `scan error` and the subnet with "answers faked", the home page shows
  "Scan problems"; a later normal upload clears both
- [X] T007 [US2] Implement `store.ScanProblems`, `CollectorOverview.Problems`, the home notice and
  Collectors status/column in `internal/web`. Make T006 pass

## Phase 5: User Story 3 - The collector says why (P2)

- [X] T008 [P] [US3] Tests in `cmd/hne-collector`: a scan with a `faked_answers` subnet logs a
  warning naming the subnet; `check` with a routed subnet prints the network-path line (OK, or
  the warning when the canary answers)
- [X] T009 [US3] Implement in `cmd/hne-collector/main.go` (engine exposes `PathFakesAnswers`).
  Make T008 pass

## Phase 6: Polish

- [X] T010 Docs: `specs/001-lan-inventory-topology/contracts/collector-cli.md` (check line,
  warning), `web-ui.md` (scan problems)
- [ ] T011 CI green, merge to `main`, image built (standing instruction)
- [ ] T012 Real network (owner): with Happ not bypassing LAN, the desktop scan shows the scan
  problem and adds no devices; with "bypass LAN", the scan is normal

## Dependencies

T001 → T002/T003 → US1 (T004–T005) and US2 (T006–T007) in parallel → US3 (T008–T009) → Polish.
