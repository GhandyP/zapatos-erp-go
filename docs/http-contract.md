# HTTP Contract

## Probes

### `GET /health`

- Auth: none
- Success: `200 OK`
- Body: `{"ok":true,"app":"Zapatos ERP"}`
- Errors: `405 Method Not Allowed`

### `GET /metrics`

- Auth: none
- Success: `200 OK`
- Body: machine-readable JSON with request counters and latency totals
- Errors: `405 Method Not Allowed`

## Authentication

### `POST /api/login`

- Auth: none
- Request body: `{"username":"...","password":"..."}`
- Success: `200 OK` with a session token and user payload
- Errors:
  - `400 Bad Request` for invalid JSON
  - `401 Unauthorized` for invalid credentials
  - `413 Payload Too Large` for oversized bodies

### `GET /api/me`

- Auth: Bearer token
- Success: `200 OK` with the current session user
- Errors:
  - `401 Unauthorized` when the token is missing or invalid

### `GET /api/boot`

- Auth: none
- Success: `200 OK` with the supported roles list

## Core API resources

The resource endpoints follow the same pattern:

- `GET /api/raw-materials`
- `POST /api/raw-materials`
- `DELETE /api/raw-materials/{id}`
- `GET /api/finished-goods`
- `POST /api/finished-goods`
- `GET /api/finished-goods/{id}`
- `PATCH /api/finished-goods/{id}`
- `GET /api/packaging`
- `POST /api/packaging`
- `DELETE /api/packaging/{id}`
- `GET /api/logistics`
- `POST /api/logistics`
- `DELETE /api/logistics/{id}`
- `GET /api/billing`
- `POST /api/billing`
- `GET /api/billing/{id}`
- `PATCH /api/billing/{id}`
- `GET /api/audit`
- `GET /api/audit/export`
- `POST /api/foxpro/sync`
- `GET /api/foxpro/status`

### Common responses

- `200 OK` for successful reads and updates
- `201 Created` for new resources
- `204 No Content` for deletes
- `400 Bad Request` for validation errors
- `401 Unauthorized` when authentication is missing or invalid
- `403 Forbidden` when the role lacks permission
- `404 Not Found` when the resource does not exist
- `405 Method Not Allowed` for unsupported methods
- `413 Payload Too Large` when the request body exceeds limits
- `500 Internal Server Error` for operational failures

## Trace and request headers

- The server preserves `X-Request-Id` when provided.
- The server preserves `Traceparent` when provided.
- Both values are echoed back in the response for correlation.
