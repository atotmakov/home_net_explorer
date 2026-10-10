# Contract change: collector API (v1, additive)

Canonical file: `specs/001-lan-inventory-topology/contracts/collector-upload-api.yaml`.

## `POST /api/v1/collections`

`UploadResult.status` gains the value **`discarded`**:

| Status | HTTP | Meaning |
|--------|------|---------|
| `stored` | 201 | unchanged |
| `duplicate` | 200 | unchanged |
| `discarded` | 200 | **new**. The run started before the owner's latest data reset ("remove all devices" or "drop all data"), judged by the run's own `started_at`. It is not stored; the collector should treat it as accepted and drop it from its spool |

Existing collectors already treat every `200`/`201` as accepted, so no collector change is
required; the collector prints the status it receives (`result=discarded`).

## Removed collectors

A token of a removed collector gets exactly the answer of a revoked token on every endpoint
(`401`, `{"error":"invalid_token"}`): `POST /api/v1/collections`, `GET /api/v1/ping`,
`GET /api/v1/routers/{id}/login`. No wire change.

## `GET /api/v1/ping`

`ignored_subnets` now comes from the owner's latest ignore choice per subnet (research R4), so it
also lists ignored subnets that have not been rediscovered since a "remove all devices". No
format change.
