# Contract change: upload API v1 (additive)

The canonical contract stays `specs/001-lan-inventory-topology/contracts/collector-upload-api.yaml`;
this feature edits it as follows (implementation task). `schema_version` remains `1`.

```yaml
# components.schemas.SubnetScan.properties
method: { type: string, enum: [arp, icmp_tcp, skipped, router_table] }

# components.schemas.Observation.properties
method: { type: string, enum: [arp, neighbor_cache, icmp, tcp, router_table] }
hostname_source: { type: string, enum: [dns, mdns, router] }
via: { type: string, maxLength: 32, description: "Router port or Wi-Fi interface, e.g. LAN1, SSID2" }

# components.schemas.CollectionRun.properties (optional)
sources:
  type: array
  maxItems: 8
  items: { $ref: "#/components/schemas/RunSource" }

# components.schemas
RunSource:
  type: object
  additionalProperties: false
  required: [type, model, address, subnet, outcome, online, offline]
  properties:
    type: { type: string, enum: [router] }
    model: { type: string, maxLength: 64 }
    address: { type: string, format: ipv4 }
    subnet: { type: string }
    outcome:
      type: string
      enum: [ok, unreachable, login_rejected, locked, session_busy, page_not_understood, skipped_after_rejection]
    online: { type: integer, minimum: 0 }
    offline: { type: integer, minimum: 0 }
```

Rules added to `contract.Validate` (data-model.md):
- `router_table` observations require a MAC.
- `via` only on `router_table` observations.
- `sources[].address` and `sources[].subnet` private; at most 8 sources.

Deploy order: upgrade the server first (it accepts both old and new collectors), then collectors.
