# Quickstart: Access Point Client Lists

Validation on the real network. Tests run in CI only.

## 1. Update (server first)

1. Merge; `.\deploy.ps1 -Pull`; the NAS reports 0.12.
2. On the desktop, download the 0.12 collector from the Collectors page and replace
   `hne-collector.exe` (keep `hne-collector.yaml`). A 0.11 server would reject the new runs,
   hence the order.

## 2. Configure the access points (US1, SC-001)

1. Open the device page of `EAP225-50-C7-BF-30-C8-F6` (192.168.0.10), set type **access point**.
2. On both EAP225 pages, in the **Access point** card choose `tplink-eap225`, enter the AP's
   username and password, Save. Reopen: the password shows only as "set".

## 3. Confirm the reader on the real units (gate G1, research R2)

On the desktop: `hne-collector.exe check`. Expected, per access point: reachable, login OK, the
endpoint used (`status.client.user` for .5, `monitor.client.client` for .10), the number of
clients, and the key names of the first client entry. If the keys differ from the fixtures, the
fixtures and parser are fixed before continuing (tasks.md G1).

## 4. A scan (US2, SC-002, SC-003)

1. Run a scan (`Scan now`, or wait for the desktop collector).
2. Collectors page: both access points `ok` with client counts; the scan took ≤ 10 s longer than
   before.
3. The device pages of phones at home (Pixel-9a, Pixel-10a, Kates-iPhone) say
   "via EAP225-… · <SSID> (<band>)".

## 5. Map (US3)

Open **Map**: the phones hang under their access point through an "<SSID> (<band>)" node; none of
them is under the Huawei's LAN1. Wired devices stay on LAN1 (or under the manual switch, once
feature 011 exists).

## 6. Roaming (SC-005)

Walk a phone from one access point's area to the other, wait for the next scan: its device page and
the Map show the other access point.

## 7. Rejected login (SC-004)

Save a wrong password on one AP's card, run a scan: outcome `login_rejected`; the next scans show
`skipped_after_rejection` (no new login attempts; on .5 the AP's `retryChances` drops by one only).
Save the right password: the next scan reads it again.
