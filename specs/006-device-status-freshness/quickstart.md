# Quickstart: Device Status Freshness

Validation on the real deployment (ocean, desktop collector `karlo` scanning `192.168.0.0/24`
with the router). Automated coverage runs in CI (see tasks.md).

1. **Upgrade**: deploy; the server log shows the one-time rebuild (`projection_rules` → 6). The
   Settings page no longer has the "missed scans" field.
2. **Green**: run `hne-collector-windows-amd64.exe scan --once`. Devices page: the
   `192.168.0.0/24` devices that answered are green "online".
3. **Stale**: wait more than 30 minutes (2 × 15-minute interval) without scanning. The same
   devices show "online" in the normal text color, the tooltip "last seen … ago"; the home page
   counts them as stale.
4. **Offline**: switch a device off (e.g. a phone's Wi-Fi), run one scan. It shows gray "offline" and
   its history has "went offline" at that scan; the filter `status=offline` lists it.
5. **Back**: switch it on, scan again: green "online", history "came online".
6. **Counts**: the home page online/stale/offline numbers equal the Devices filter results.
