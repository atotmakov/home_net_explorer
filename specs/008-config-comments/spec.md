# Feature Specification: Comments in hne-collector.json

**Feature Branch**: `008-config-comments`

**Created**: 2026-10-22

**Status**: Implemented

**Input**: Owner request: "add comments for each string in config json"

## Context

The owner could not remember what `subnets` in `hne-collector.json` means. Plain JSON has no
comments, so the file the server generates gives no hint about its settings.

## Requirements

- **FR-001**: The collector MUST accept `//` comments in `hne-collector.json`, on their own line or
  after a value. `//` inside a string (for example `"http://..."`) is part of the string. A file
  without comments reads as before.
- **FR-002**: The `hne-collector.json` shown and downloaded after creating a collector MUST have a
  `//` comment line (or lines) directly above each setting: `server_url`, `name`, `token`,
  `subnets`, `extra_subnets` (now always written, empty by default), `interval_seconds`,
  `routers`.
- **FR-003**: The comments explain each setting in plain words, including the meaning of empty
  `subnets` (scan every private subnet this computer is connected to, /22 or narrower) and how
  `extra_subnets` differs.

## Compatibility

A commented config needs a collector of version 0.9 or later; download the collector and its
config together from the same server. Older collectors reject it with a config error (exit 2).

## Tests

- `internal/contract/comments_test.go` `TestStripComments`: comments removed, `//` inside strings
  kept, plain JSON unchanged.
- `cmd/hne-collector/main_test.go` `TestConfigWithComments`: a commented config loads.
- `tests/integration/us2_collector_test.go` `TestCollectorConfigComments`: every setting of the
  generated config has a comment line above it; `createCollector` parses it after stripping.
