# Contract change: collector API v1 (additive)

The canonical contract stays `specs/001-lan-inventory-topology/contracts/collector-upload-api.yaml`;
this feature edits it as follows (implementation task). `schema_version` remains `1`.

```yaml
# components.schemas.PingResponse.properties (new, optional for old servers)
routers:
  type: array
  description: Routers configured in the web UI (feature 003). Never carries credentials.
  items: { $ref: "#/components/schemas/RouterRef" }

# components.schemas
RouterRef:
  type: object
  additionalProperties: false
  required: [id, model, address, subnet]
  properties:
    id: { type: integer, minimum: 1 }
    model: { type: string, maxLength: 64 }
    address: { type: string, format: ipv4 }
    subnet: { type: string }

RouterLogin:
  type: object
  additionalProperties: false
  required: [username, password]
  properties:
    username: { type: string, minLength: 1, maxLength: 128 }
    password: { type: string, minLength: 1, maxLength: 128 }

# RunSource.outcome enum adds: login_unavailable
```

```yaml
paths:
  /api/v1/routers/{id}/login:
    get:
      summary: The current login of a router configured in the web UI (feature 003)
      operationId: getRouterLogin
      security: [{ collectorToken: [] }]
      parameters:
        - { name: id, in: path, required: true, schema: { type: integer, minimum: 1 } }
      responses:
        "200":
          description: The login. Sent with Cache-Control no-store. Keep it in memory for one read only.
          content: { application/json: { schema: { $ref: "#/components/schemas/RouterLogin" } } }
        "401": { description: Missing, unknown or revoked collector token (invalid_token) }
        "404": { description: No such router (router_not_found) }
```

Rules:
- Only `Authorization: Bearer <collector token>` of an active collector is accepted; a browser
  session cookie alone gets 401 (feature 001: no session auth on `/api/v1/*`).
- Responses carry `Cache-Control: no-store`.
- The server never logs response bodies; the access log has method, path and status only.
- Deploy order: server first (old collectors ignore `routers`; new collectors against an old
  server see no `routers` and read only their file's routers).
