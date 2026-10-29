# Contract change: collector command line

Extends `specs/001-lan-inventory-topology/contracts/collector-cli.md`.

| Command | Change |
|---------|--------|
| `hne-collector scan --once --ports` | New flag. After the presence scan, check the ports of every address found on directly scanned subnets (ARP or ICMP/TCP), except the server's `excluded` list, in the foreground; upload the presence run, then the port run. Logs `port check finished devices=N open=M duration=…`. With `--dry-run`, prints both runs and uploads nothing. |
| `hne-collector scan --once` | Unchanged, plus: checks the server's `due` addresses found in this scan, in the foreground, and uploads the port run. |
| `hne-collector run` | After each presence run, starts a background port job for the `due` addresses found (none if one is still running). `--ports` is accepted and forces the first job only. |
| `hne-collector check` | Prints `Port checks: on (N due)` or `off`, from ping. Never checks ports. |

Exit codes are unchanged. A port job's upload failure is spooled like any run.
