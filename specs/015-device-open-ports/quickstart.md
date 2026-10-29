# Quickstart: Open Ports per Device (real network)

## Prerequisites

- Server 0.20 deployed on the NAS (`.\deploy.ps1 -Pull`); desktop collector 0.20 in Downloads.
- Known services: the NAS (DSM web UI, SMB), the Axis cameras (web UI, RTSP), the desktop.

## 1. Gate G1: the technique works where it runs

1. On the desktop: `hne-collector-windows-amd64.exe scan --once --ports --dry-run`. Expected: a
   port run whose checks list, for the NAS, its web and SMB ports; for an Axis camera 80/tcp and
   554/tcp; duration within SC-004.
2. On the NAS: press **Check ports now** on the home page. Expected: "Checking ports: n of m
   devices", then the NAS's own collector's checks of 192.168.1.x arrive (Collectors page shows a
   new upload).

## 2. Device page (US1)

Open an Axis camera's page: "Open ports" lists 80/tcp http and 554/tcp rtsp with the address and
time. Open a device never checked (a 192.168.0.x device only the router lists): "Not reachable for
port checks".

## 3. Changes (US2)

On the desktop, start a listener on a free port (for example `python -m http.server 8765`), run
`scan --once --ports`: the desktop's page shows 8765/tcp open and its history "Port 8765/tcp
opened". Stop it, run again: 8765/tcp moves to "Previously open", history "Port 8765/tcp closed".

## 4. Control (US3)

Turn port checks off on one device page, press **Check ports now**: that device's "Last checked"
does not change and the page says checks are off. Settings shows the port list and schedule.

## 5. Schedule

After a day, every online device on 192.168.1.x shows a "Last checked" within the last 24 hours,
spread over different times of day.
