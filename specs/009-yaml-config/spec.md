# Feature Specification: Collector Config in YAML

**Feature Branch**: `009-yaml-config`

**Created**: 2026-10-22

**Status**: Implemented

**Input**: Owner request: "switch to yaml" (after feature 008 added `//` comments to
`hne-collector.json`, which standard JSON does not allow).

## Context

Feature 008 explained each setting of `hne-collector.json` with `//` comments, but those are not
standard JSON: editors flag them and other tools cannot read the file. YAML has comments (`#`)
as part of the standard. Supersedes the file format of feature 008; the comment texts stay.

## Requirements

- **FR-001**: The collector MUST read its config from `hne-collector.yaml` next to the binary,
  with the same settings and environment overrides as before (`server_url`, `name`, `token`,
  `subnets`, `extra_subnets`, `interval_seconds`, `routers`).
- **FR-002**: An unknown setting in `hne-collector.yaml` (for example a misspelling) MUST be a
  config error naming it (exit 2).
- **FR-003**: When `hne-collector.yaml` does not exist, the collector MUST read an existing
  `hne-collector.json` as before (with or without feature 008 `//` comments). When both exist,
  `hne-collector.yaml` is used.
- **FR-004**: After a collector is created, the Collectors page MUST show and offer for download
  `hne-collector.yaml`, with a `#` comment line (or lines) directly above each setting; the
  `routers` comment shows an example entry with credentials.
- **FR-005**: Router passwords read from the YAML file MUST stay masked in any output, as in
  JSON (feature 002 FR-006).

## Compatibility

A collector older than 0.10 does not read `hne-collector.yaml`; download the collector and its
config together from the same server. Existing installations keep working with their
`hne-collector.json` until it is replaced.

## Tests

- `cmd/hne-collector/main_test.go`: `writeConfig` writes YAML, so every collector test uses it;
  `TestYAMLConfig` (comments, routers with a password, YAML wins over JSON, unknown setting is an
  error); `TestConfigWithComments` (JSON fallback).
- `internal/collect/router/router_test.go`: a router read from YAML keeps a usable password;
  YAML output masks it.
- `tests/integration/us2_collector_test.go`: the generated config parses as strict YAML, has a
  `#` comment line above every setting, and downloads as `hne-collector.yaml`.
