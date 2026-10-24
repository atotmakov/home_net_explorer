# Quickstart: Manual Devices (real-network validation)

Prerequisite: 0.14 deployed to the NAS (`.\deploy.ps1 -Pull`); the desktop collector running.

1. **Add the switch**: Devices → Add device → Name `TL-SL1218P`, Type `switch`, no MAC → Save.
   Expect its device page with "Added by hand"; in Devices it shows "manual" and no address.
2. **Counts**: home page total = Devices list count; online + stale + offline + manual = total
   (SC-004).
3. **Wire it**: on the switch's page, Connected via → `HG8145V5`. On two wired 192.168.0.x
   devices, Connected via → `TL-SL1218P`. Map: Huawei → switch → the two devices, in the
   192.168.0.0/24 group, dashed outline on the switch, no LAN1 edge for the two devices (SC-002).
4. **Loop**: on the switch's page choose one of the two devices as Connected via → refused with
   the loop message.
5. **Online only**: toggle it; the switch stays while one of its devices is online.
6. **Survives scans**: after 3 scans the switch and its links are unchanged (SC-003).
7. **Remove** (optional, re-add afterwards): Remove → type `remove` → the switch is gone and the
   two devices are back on the Huawei's LAN1.

Record the results (counts only, no MACs) in tasks.md.
