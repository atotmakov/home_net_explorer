# Feature Specification: Router Setup from the Web UI

**Feature Branch**: `003-router-ui-setup`

**Created**: 2026-10-10

**Status**: Draft

**Input**: User description: "Router setup from the web UI (builds on feature 002 router device lists). On a device's page, when the owner sets the device type to "router", they can also choose the router model from the list of supported models (currently huawei-hg8145v5), and enter the router's username and password; the router address is the device's current IP. The server stores these router settings, with the password in plain text in its database (owner decision: plain text, no encryption at rest; never shown again in the UI after saving, only replaced or cleared). Every collector receives every configured router (no per-collector assignment); collectors that cannot reach a router report it as unreachable. The next time a collector config (hne-collector.json) is created or downloaded on the Collectors page, its "routers" list is already filled with each configured router's model and URL but WITHOUT username/password. Before each router read, the collector fetches the username and password from the server over its authenticated collector API and keeps them only in memory (never written to disk or logs); changing the password in the UI takes effect at the next scan. This reverses feature 002 FR-006 (credentials only on the collector machine) for routers configured in the UI; routers with credentials in hne-collector.json keep working as before. The rejection/lockout protection of feature 002 (FR-011) must still hold: after a rejected login, the router is not retried until its credentials or settings change (in the UI or the config) or the owner runs check."

## Context

Feature 002 lets a collector read a router's device list, but the owner must type the router's
model, address, username and password into each collector's `hne-collector.json` by hand, and
the password lives only in that file. This feature moves router setup into the web UI: the owner
marks a device as a router, picks its model and enters its login once, and collectors get
everything they need from the server.

**Decisions taken with the owner (2026-10-10)**, changing feature 002:
- Router passwords set in the UI are stored on the server, **in plain text** in its database
  (no encryption at rest). Feature 002 FR-006 ("credentials only on the collector machine")
  no longer holds for these routers.
- **Every collector** receives every router configured in the UI; there is no per-collector
  assignment.
- Collectors **fetch the login from the server before each read** and keep it in memory only;
  `hne-collector.json` lists the router without username or password.
- Collectors **get the current list of UI-configured routers from the server at each scan**
  (clarified 2026-10-10, option B), so routers added or changed in the UI are picked up without
  downloading a new configuration.

## Clarifications

### Session 2026-10-10

- Q: Should collectors pick up routers added or changed in the UI at their next scan, without a
  new configuration download? -> A: Yes. At each scan the collector gets the current list of
  UI-configured routers (model and address, no credentials) from the server; the list in a
  downloaded `hne-collector.json` is informational and only used when the server is unreachable.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Configure a router from its device page (Priority: P1)

As the owner, I open my ISP router's device page, set its type to "router", pick its model from
the supported list and enter its username and password. The router is now configured on the
server; I never have to type its password into a file.

**Why this priority**: It is the entry point of the feature: without it there is nothing for
collectors to use.

**Independent Test**: On a device page, set type "router", choose `huawei-hg8145v5`, enter a
username and password, save. Reopen the page: the model, address and username are shown, the
password is shown only as "set".

**Acceptance Scenarios**:

1. **Given** a device whose type is set to "router", **When** the owner opens its page, **Then**
   router settings are offered: a model chosen from the supported models, a username and a
   password, and the router address taken from the device's current address.
2. **Given** saved router settings, **When** the owner reopens the device page (or any other
   page), **Then** the password is never displayed; the page only states that a password is set.
3. **Given** saved router settings, **When** the owner enters a new password, or changes the
   model or username, **Then** the new values replace the old ones; leaving the password field
   empty keeps the current password.
4. **Given** saved router settings, **When** the owner removes them (or changes the device type
   away from "router"), **Then** the router is no longer configured and collectors stop reading
   it at their next scan.
5. **Given** a device whose current address is not a private address, **When** the owner tries
   to save router settings, **Then** saving is refused with a clear message.

---

### User Story 2 - Collectors use UI-configured routers without editing files (Priority: P1)

As the owner, once I configure a router in the UI, every collector starts reading it at its next
scan, without any file editing or download. New collector configurations also list it. The
collector asks the server for the router's login before each read, so I never copy the password,
and changing anything in the UI is enough.

**Why this priority**: It delivers the value of feature 002 without manual config editing, and
it is what makes US1 useful.

**Independent Test**: Configure a router in the UI and run one scan on an existing collector
(no new download): the router's devices appear in the inventory. Download a new
`hne-collector.json`: it lists the router without username or password. Change the password in the UI (to the router's new
password): the next scan succeeds without touching the collector.

**Acceptance Scenarios**:

1. **Given** one or more routers configured in the UI, **When** the owner creates a collector,
   **Then** the configuration lists every configured router with its
   model and address, and contains no router username or password.
2. **Given** a router configured in the UI, **When** any collector starts its next scan, **Then**
   it learns about the router from the server, whether or not its configuration file lists it.
3. **Given** a collector about to read a UI-configured router, **When** it is about
   to read that router, **Then** it obtains the current username and password from the server
   and uses them for that read only.
4. **Given** the owner changed the router's password, address or model in the UI, **When** the collector's next
   scan runs, **Then** it uses the new password.
5. **Given** the server is unreachable, or no longer has settings for that router, **When** the
   collector is about to read it, **Then** the router read is reported as failed with a clear
   reason and the rest of the scan still completes (as in feature 002 FR-010).
6. **Given** a collector that cannot reach the router (e.g. the NAS), **When** it scans, **Then**
   the router read is reported as unreachable for that collector, and other collectors are not
   affected.

---

### User Story 3 - Keep the login safe and the router unlocked (Priority: P2)

As the owner, I want the router password to appear nowhere except where it is needed, and I want
a wrong password to never lock my router.

**Why this priority**: The password now sits on the server and travels to collectors; the
safety rules must be explicit and testable. They support US1 and US2.

**Independent Test**: Search every page, downloaded config, collector file, collector log and
upload for the configured password: it appears in none of them. Save a wrong password: the
router is tried once, then skipped until the password is changed in the UI or `check` runs.

**Acceptance Scenarios**:

1. **Given** a router configured in the UI, **When** the owner looks at any page or downloads any
   collector configuration, **Then** the password does not appear.
2. **Given** a collector that fetched a router login, **When** it finishes the read, **Then** the
   username and password are not in any file it writes (configuration, spool, log, rejection
   marker), not in its output, and not in its upload.
3. **Given** a wrong password saved in the UI, **When** a collector's login is rejected, **Then**
   that collector does not try the router again until the router's settings change in the UI or
   the configuration, or the owner runs the collector's check command (feature 002 FR-011).
4. **Given** a request for router logins that does not come from an active collector (no token,
   a revoked token, or a browser session), **When** it reaches the server, **Then** it is
   refused.

### Edge Cases

- **Router configured both in the UI and in `hne-collector.json` with credentials**: the
  credentials in the file are used for that collector; the UI settings still apply to other
  collectors.
- **Router added, changed or removed in the UI after a collector's configuration was
  downloaded**: every collector uses the server's current list at its next scan; no new download
  is needed.
- **Router's address changes** (e.g. the device gets a new IP): the UI follows the device's
  current address (FR-002) and collectors use the new address at their next scan.
- **Server unreachable at scan time**: the collector uses the last router list it received (model
  and address only) and the routers in its configuration file, but cannot obtain logins for
  UI-configured routers, so those reads fail with a clear reason (FR-012).
- **Device with several current addresses**: the owner chooses which address is the router's
  address; by default the address on the subnet the device was seen on most recently.
- **Device type changed away from "router"**: its router settings are removed, including the
  stored password.
- **Device merged into another device or split**: router settings follow the device's identity
  as the owner's other notes do (feature 001).
- **Unsupported model**: only supported models can be chosen; a router of another model can't be
  configured until support is added.
- **Several collectors reading the same router at once**: the router may allow only one session;
  the losing collector reports "busy" for that scan and retries next scan (feature 002).

## Requirements *(mandatory)*

### Functional Requirements

**Router settings in the UI**

- **FR-001**: The device page MUST offer router settings for a device whose type is "router": a
  model chosen from the supported router models, a username, a password, and the router address.
- **FR-002**: The router address MUST be one of the device's current addresses and MUST be a
  private (RFC 1918) address; by default it is the address on the subnet where the device was
  seen most recently. The owner may choose another current address.
- **FR-003**: The server MUST store router settings, including the password **in plain text**
  in its database (owner decision 2026-10-10; encryption at rest is out of scope).
- **FR-004**: After saving, the password MUST NOT be displayed by any page; the UI only states
  whether a password is set. Leaving the password field empty when saving keeps the stored one.
- **FR-005**: The owner MUST be able to change the model, username, password and address, and to
  remove the router settings; changing the device's type away from "router" removes them.
- **FR-006**: Router settings MUST only be changed by the logged-in owner, with the same
  protections as other owner edits (feature 001).

**Collector configuration and logins**

- **FR-007**: The configuration shown when a collector is created on the Collectors page (the
  only time it is offered, because it contains the token) MUST list every router configured in the UI, with its model and address, and MUST NOT contain a
  router username or password. This list is informational: collectors use the server's current
  list (FR-007a).
- **FR-007a**: At the start of each scan, a collector MUST obtain from the server the current list
  of UI-configured routers (model and address, no credentials) and read every router on it, plus
  the routers in its configuration file. When the server is unreachable it MUST use the last list
  it received; that cached list MUST NOT contain credentials.
- **FR-008**: Before each read of a UI-configured router (one without credentials in the
  configuration file), a collector MUST obtain that router's current username and password from the server, over its
  authenticated collector connection.
- **FR-009**: Router logins MUST be provided only to active collectors (a valid, non-revoked
  collector token); every other request MUST be refused. They MUST NOT be provided to browser
  sessions.
- **FR-010**: A collector MUST keep fetched router logins in memory only, for the duration of the
  read; they MUST NOT appear in its configuration file, spool, log, rejection marker, output or
  uploads.
- **FR-011**: Adding, removing or changing a router in the UI (model, address, username or
  password) MUST take effect at each collector's next scan, without downloading a new
  configuration.
- **FR-012**: If a collector cannot obtain a router's login (server unreachable, router no longer
  configured, login refused), the router read MUST be reported as failed with a reason
  distinguishable from the other outcomes, and the rest of the scan MUST complete and upload
  (feature 002 FR-010).
- **FR-013**: Routers listed in `hne-collector.json` with their own username and password MUST
  keep working as in feature 002, and their credentials take precedence for that collector.

**Lockout protection**

- **FR-014**: After a rejected or locked login, a collector MUST NOT try that router again until
  the router's settings change (in the UI or in the configuration) or the owner runs the
  collector's check command (feature 002 FR-011, extended to UI settings).
- **FR-015**: The collector's check command MUST report, per router, whether its login came from
  the configuration or from the server, and the read result, without printing credentials.

### Key Entities

- **Router settings** (server): belongs to a device of type "router"; holds the model, the router
  address (one of the device's current private addresses), the username, the password (plain
  text, never displayed), and when it was last changed.
- **Router login** (transient): the username and password a collector obtains from the server for
  one read; exists only in the collector's memory.
- **Collector configuration** (`hne-collector.json`): now lists UI-configured routers by model and
  address only (informational).
- **Router list** (server to collector, each scan): the current UI-configured routers, model and
  address only; the collector caches the last one for when the server is unreachable.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: The owner can go from "this device is my router" to a successful router read by an
  existing collector without editing or downloading any file, in under 3 minutes (configure on the
  device page, run one scan).
- **SC-002**: The router password appears in none of: any web page, any downloaded collector
  configuration, any file a collector writes, any collector output or log, any upload (verified
  by searching all of them for the configured password).
- **SC-003**: After a router is added or its password changed in the UI, the next scan of every
  collector uses it; no collector needs a new configuration download.
- **SC-004**: A wrong password saved in the UI causes at most one login attempt per collector
  until the settings change or check runs.
- **SC-005**: Every request for router logins without a valid collector token is refused (0
  logins disclosed in tests with missing, unknown and revoked tokens and with a browser session).
- **SC-006**: Collectors configured as in feature 002 (credentials in their file) behave exactly
  as before.

## Security TODO (deferred, revisit later)

Accepted for now by the owner (2026-10-10) to keep this feature simple; to be revisited in a
later feature. None of these blocks this feature.

- **TODO-SEC-1 Encryption at rest**: router passwords are plain text in the server database and
  its backups. Revisit: encrypt with a key kept outside the data volume (env var or key file).
- **TODO-SEC-2 Transport**: logins travel from the server to collectors over plain HTTP on the
  LAN. Revisit: HTTPS between collectors and server, or encrypting the login per collector.
- **TODO-SEC-3 Scope of disclosure**: every collector can obtain every router's login. Revisit:
  per-collector assignment, so only collectors that need a router get its login.
- **TODO-SEC-4 Audit**: there is no record of which collector fetched which login, and when.
  Revisit: log credential fetches (without the secret) and show them to the owner.
- **TODO-SEC-5 Router account**: the owner's full router login is used. Revisit: recommend or
  require a read-only router account where the model supports one.

## Assumptions

- Only the owner uses the web UI (single-owner app, feature 001); there are no user roles.
- Collector-to-server traffic is plain HTTP on the home LAN (feature 001), so router logins
  travel unencrypted on the LAN, like the router's own web page (feature 002 assumption).
- The router's address is plain `http://<address>`; HTTPS routers and custom ports keep using
  `hne-collector.json` entries (feature 002).
- The NAS's built-in collector also receives UI-configured routers; if it can't reach a router it
  reports it as unreachable.
- Anyone with access to the server's database file (or a backup of it) can read router
  passwords; the owner accepted this.
- Supported router models are those of feature 002 (`huawei-hg8145v5`).
