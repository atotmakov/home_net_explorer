# Contract change: web UI

Extends `specs/001-lan-inventory-topology/contracts/web-ui.md`.

## Device page (`GET /devices/{id}`)

When the device's saved type is `router`, a **Router** card is shown:

```text
Router
  Model      [huawei-hg8145v5 ▾]          (supported models only)
  Address    192.168.0.1 (192.168.0.0/24)  [subnet ▾ only when several current addresses]
  Username   [root            ]
  Password   [                ]  Password: set   (empty = keep the current one)
  Latest read  desktop: OK, 14 online / 16 offline (2026-10-10 10:15)
  [Save router]  [Remove router]
```

- The password input is `type=password`, never prefilled, `autocomplete=new-password`.
- When the device has no current private address: "No current private address: the router can't
  be read until the device is seen on a private subnet."
- No page, form value, attribute or script ever contains the stored password.

## Routes

| Route | Behavior |
|-------|----------|
| `POST /devices/{id}/router` | Owner session + Origin check (feature 001). Fields `model`, `username`, `password` (empty keeps the stored one; required when none is stored), `subnet` (optional). Validates model ∈ supported models, 1–128 character username/password, `subnet` ∈ the device's current subnets. 400 with a message on error; 303 back to the device page on success |
| `POST /devices/{id}/router/remove` | Deletes the router settings; 303 back |
| `POST /devices/{id}/attrs` | Unchanged, except: saving a type other than `router` also deletes the device's router settings |

## Collectors page

- The config shown and downloaded after creating a collector adds
  `"routers": [{"model": "huawei-hg8145v5", "url": "http://192.168.0.1"}]` for every router in
  the list, without `username` and `password`.
- The router column (feature 002) adds the phrase `login unavailable from the server` for the
  outcome `login_unavailable`.
