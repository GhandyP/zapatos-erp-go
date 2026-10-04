# HTTP Contract

This document describes the routes and behavior currently implemented by `internal/server/server.go`; it is not a desired or hardened API design. The application UI remains Spanish. This technical contract is written in English.

## General behavior

- JSON success and error responses use `Content-Type: application/json; charset=utf-8`. JSON errors have the shape `{"error":"..."}`. Lists are JSON arrays; single resources are JSON objects.
- Endpoints that decode JSON expect a JSON body, but the handlers do not enforce the request `Content-Type`. The request-body limit is 1 MiB. Invalid JSON returns `400`; an oversized decoded body returns `413`.
- Successful list/detail reads return `200`. Successful resource POSTs return `201` (including an upsert to an existing ID); stock adjustment and invoice issue return `200`; deletes return `204` with no response body.
- Validation failures return `400`, missing resources return `404`, and operational failures return `500` with `{"error":"internal server error"}`. Missing/invalid-auth behavior on protected routes is described below; it does not consistently follow a `401`/`403` split.
- `X-Request-Id` is echoed when supplied and generated when absent. A non-empty `Traceparent` is echoed. The server also sets baseline browser headers; see [`security-backlog.md`](security-backlog.md) for the limits of those protections.

## Public routes and probes

| Route | Auth | Current response |
|---|---|---|
| `GET /health` | None | `200`, `{"ok":true,"app":"Zapatos ERP"}` |
| `GET /metrics` | None | `200` JSON snapshot with `requests_total`, `in_flight`, `duration_ms` (`total`, `average`), and `status_counts` (`1xx`–`5xx`). Also sets `Cache-Control: no-store`. |
| `GET /api/boot` | None | `200`, `{"roles":[...]}` with the role names listed below. |
| `/` and `/app` | None | Serve the Spanish HTML UI with `Content-Type: text/html; charset=utf-8`; these handlers do not restrict the HTTP method. |

`/metrics` returns `405` for other methods and sets `Allow: GET`. `/health` and `/api/boot` return a bare `405` for other methods without an `Allow` header.

## Authentication

### `POST /api/login`

No authentication is required. Send `{"username":"...","password":"..."}`. A successful login returns `200` with `{"token":"...","user":{"username":"...","role":"..."}}`. Use the token as `Authorization: Bearer <token>` on authenticated requests.

- `400`: body cannot be decoded as JSON.
- `401`: credentials are rejected.
- `413`: decoded body exceeds 1 MiB.
- Other methods return a bare `405` with `Allow: POST`.

### `GET /api/me`

Requires a valid bearer token. Returns `200` with the session user object `{"username":"...","role":"..."}`. Missing or invalid authentication returns `401` with `{"error":"unauthorized"}`. Other methods return a bare `405` without `Allow`.

### Current auth behavior on protected resources

Resource handlers look up the bearer token and then check the resulting role's capability. Today, missing or invalid authentication resolves to an empty role and protected resource requests return `403` `{"error":"forbidden"}`, just like an authenticated role without the required capability. This differs from `/api/me`, which returns `401`; it is a description of current behavior, **not** a hardened auth contract. The inconsistency and deferred remediation are tracked in [`security-backlog.md`](security-backlog.md#4-make-auth-status-rbac-and-read-only-ui-consistent); this work unit does not change it.

## Core API resources

All routes below, except where identified as public, are capability-protected as shown in the role matrix; callers are expected to send a bearer token. Current handlers do not consistently reject missing/invalid tokens as unauthenticated, as detailed above. The methods in this table are the implemented operations; it does not imply that other methods are consistently rejected with `405`.

| Route | Capability | Request / success |
|---|---|---|
| `GET /api/raw-materials` | `raw-materials:read` | `200` JSON array of materials. |
| `POST /api/raw-materials` | `raw-materials:write` | JSON material; `201` saved material. |
| `DELETE /api/raw-materials/{id}` | `raw-materials:write` | `204`; missing ID returns `404`. |
| `GET /api/finished-goods` | `finished-goods:read` | `200` JSON array of variants. |
| `POST /api/finished-goods` | `finished-goods:write` | JSON variant; `201` saved variant. |
| `GET /api/finished-goods/{id}` | `finished-goods:read` | `200` variant; missing ID returns `404`. |
| `PATCH /api/finished-goods/{id}` | `finished-goods:write` | JSON stock adjustment; `200` updated variant. |
| `GET /api/packaging` | `packaging:read` | `200` JSON array of packaging specs. |
| `POST /api/packaging` | `packaging:write` | JSON packaging spec; `201` saved spec. |
| `DELETE /api/packaging/{id}` | `packaging:write` | `204`; missing ID returns `404`. |
| `GET /api/logistics` | `logistics:read` | `200` JSON array of storage slots. |
| `POST /api/logistics` | `logistics:write` | JSON storage slot; `201` saved slot. |
| `DELETE /api/logistics/{id}` | `logistics:write` | `204`; missing ID returns `404`. |
| `GET /api/invoices` | `billing:read` | `200` JSON array of invoices. |
| `POST /api/invoices` | `billing:write` | JSON invoice; `201` saved invoice. |
| `GET /api/invoices/{id}` | `billing:read` | `200` invoice; missing ID returns `404`. |
| `POST /api/invoices/{id}/issue` | `billing:write` | No body required; `200` issued invoice. |
| `GET /api/foxpro` | `foxpro:read` | `200` FoxPro adapter result object. |
| `POST /api/foxpro/sync` | `foxpro:sync` | No body required; `200` FoxPro adapter result object. |
| `GET /api/audit` | `audit:read` | `200` JSON array of filtered audit events. |
| `GET /api/audit/export` | `audit:read` | `200` filtered CSV attachment. |

### Request fields and validation

Dimensions are nested objects with `lengthCm`, `widthCm`, `heightCm`, and `weightKg`. Length, width, and height must be positive; weight must be non-negative.

| Resource | JSON fields | Current validation / defaults |
|---|---|---|
| Raw material | `id`, `name`, `unit`, `minStock`, `dimensions` | `id` and `name` required; `minStock` must be non-negative; dimensions required by validation. |
| Finished-goods variant | `id`, `style`, `size`, `color`, `stock`, `dimensions` | `style`, `size`, and `color` required; `stock` must be non-negative; dimensions validated. An omitted/blank `id` is generated from style, size, and color. Responses may include `adjustments`, whose entries contain `id`, `delta`, `note`, and `at`. |
| Finished-goods stock adjustment | `delta`, `note` | `delta` must be non-zero and the resulting stock cannot be negative; `note` may be empty. |
| Packaging spec | `id`, `name`, `transportMode`, `maxUnits`, `dimensions` | `id`, `name`, and `transportMode` required; `maxUnits` must be positive; dimensions validated. |
| Storage slot | `id`, `name`, `zone`, `capacityUnits`, `dimensions` | `id`, `name`, and `zone` required; `capacityUnits` must be positive; dimensions validated. |
| Invoice | `id`, `customerId`, `total`, `tax`, `status` | `customerId` required; `total` and `tax` must be non-negative. A blank `id` is generated. `status` defaults to `draft` and accepts `draft`, `issued`, `paid`, or `void`. The issue operation only accepts a draft invoice. |

The invoice POST handler upserts by ID but still responds `201`. A successful issue changes the invoice status to `issued`; attempting to issue a non-draft invoice is a validation error (`400`).

### FoxPro adapter result

Both FoxPro routes return JSON with these fields:

```json
{
  "lastSyncAt": null,
  "imported": 0,
  "updated": 0,
  "failed": 0,
  "pendingIssued": 0,
  "syncedIssued": 0
}
```

`lastSyncAt` is `null` before a sync and a timestamp string after one. The other fields are integer counts. The current adapter is an **in-memory simulation; it performs no external FoxPro transport**. Its sync state is process-local; a restart does not preserve the adapter's sync history.

## Audit filters and CSV

`GET /api/audit` and `GET /api/audit/export` accept the same optional query parameters:

- `q`: case-insensitive substring search over actor, action, entity, and timestamp.
- `actor` and `action`: case-insensitive substring filters.
- `entity`: case-insensitive substring of the entity value or its module prefix (the text before `:`).
- `from` and `to`: accepted as RFC3339 timestamps or `YYYY-MM-DD` dates. Invalid values are ignored. `from` is inclusive. The current `to` comparison adds 24 hours minus 1 nanosecond to the parsed value: a date therefore includes that whole day, while an RFC3339 value also includes the following 24 hours. This reflects the current filter implementation, not a normalized timestamp-range convention.

JSON audit events have `at`, `actor`, `action`, and `entity` string fields. The CSV response uses `Content-Type: text/csv; charset=utf-8`, an attachment filename based on the current UTC date, and this header/column order:

```text
at,actor,action,entity,module
```

`module` is the portion of `entity` before the first colon. The export applies the same filters as the JSON endpoint.

## Role and capability matrix

The public `/api/boot` route currently lists `administrador`, `almacen`, `produccion`, `ventas`, `contabilidad`, and `auditoria`. This matrix describes the server-side capability checks for protected resource routes; it does not imply a consistent authentication status code.

| Role | Raw materials | Finished goods | Packaging | Logistics | Invoices | Audit | FoxPro |
|---|---|---|---|---|---|---|---|
| `administrador` | Read + write | Read + write | Read + write | Read + write | Read + write | Read | Read + sync |
| `almacen` | Read + write | Read + write | Read + write | Read + write | — | — | — |
| `produccion` | Read + write | Read + write | Read + write | Read only | — | — | — |
| `ventas` | — | Read only | — | — | Read + write | — | — |
| `contabilidad` | — | — | — | — | Read + write | — | — |
| `auditoria` | Read only | Read only | Read only | Read only | Read only | Read | — |

Here, write covers resource creation/update operations and deletes where those routes exist. For invoices it includes creation/upsert and issuing; for FoxPro the two capabilities are read status and trigger sync.

## Status and method details

- `400 Bad Request`: malformed JSON on decoded request bodies or service validation failure.
- `401 Unauthorized`: rejected login or missing/invalid bearer token on `/api/me`.
- `403 Forbidden`: insufficient role capability, and currently also missing/invalid authentication on protected resource routes.
- `404 Not Found`: unknown route/resource. The current `/api/finished-goods/{id}` handler returns `404` for unsupported methods rather than `405`.
- `405 Method Not Allowed`: used by some handlers, but method handling is not uniform. Some method errors have no `Allow` header (including `/health`, `/api/me`, `/api/boot`, and the audit routes); do not treat `Allow` as a complete route inventory.
- `413 Payload Too Large`: decoded JSON body exceeds 1 MiB.
- `500 Internal Server Error`: operational failure; response body is generic and does not expose the underlying error.

For resource requests, the method check may run before the capability check. Do not infer that every unsupported method will produce `405`, or that a `403` proves a valid identity. The auth discrepancy is deferred as described above.
