# zapatos-erp-go

Go-based ERP prototype for shoe manufacturing and operations.

## What the prototype does

A single-page Spanish UI is served at `/` and `/app`. Users sign in with a seeded account (the login panel lists the local accounts), and the UI renders the modules their role allows. Authorization is enforced on the server for every request, so hiding a control is only cosmetic.

| Module | What it covers | Who can use it |
| --- | --- | --- |
| Raw materials | Material entry with id, name, unit, minimum stock, and length/width/height/weight. | `administrador`, `almacen`, `produccion` (write); `auditoria` (read) |
| Finished goods | Variant entry plus stock adjustments. | `administrador`, `almacen`, `produccion` (write); `ventas`, `auditoria` (read) |
| Packaging | Packaging specs with transport mode, maximum units, and dimensions. | `administrador`, `almacen`, `produccion` (write); `auditoria` (read) |
| Logistics | Storage slots with zone and capacity. | `administrador`, `almacen`, `produccion` (write); `auditoria` (read) |
| Billing | Invoice entry and issuing with customer, total, tax, and status. | `administrador`, `ventas`, `contabilidad` (write); `auditoria` (read) |
| Manufacturing | The provisional production slice described below. | `administrador` (full); `produccion` (read and work-order actions) |
| Audit | Recent audit events, newest first, and a CSV export. | `administrador`, `auditoria` |
| FoxPro adapter | External adapter status and a manual sync trigger. | `administrador` |

### Provisional manufacturing flow

1. An administrator records opening stock for a material and configures one BOM line per finished variant (`POST /api/manufacturing/material-stock/openings`, `POST /api/manufacturing/boms`).
2. An administrator or a `produccion` user creates a work order for a BOM, releases it, and completes it with the good quantity (`POST /api/manufacturing/work-orders`, then `.../release`, then `.../complete`).
3. Completion consumes `planned quantity × quantity per unit` from the material, adds the good quantity to the finished variant, updates the order state, and writes the audit event in the same transaction. Material balance is available at `GET /api/manufacturing/materials/{materialId}/balance`.

Quantities are integers, and each action is atomic: insufficient stock, an unknown reference, a repeated transition, or an unauthorized call changes nothing and reports the failure to the UI. These rules are provisional and still need factory validation — one material, one BOM line, and one finished variant are enough for the prototype, while scrap, lots, multiple BOM lines, purchasing, and costing are out of scope. Nothing here claims factory acceptance or production readiness.

The endpoint-by-endpoint contract, payloads, role gates, and status codes are documented in [`docs/http-contract.md`](docs/http-contract.md).

## Requirements

- Go 1.25+
- Writable `data/` directory
- `config/bootstrap.json` present in the repository root

## Run locally

```bash
go run ./cmd/zapatos-erp
```

Defaults:

- bind address: `:3001`, taken from the checked-in `config/bootstrap.json`
- data directory: `data/`

If `config/bootstrap.json` is missing or cannot be parsed, the built-in fallback is bind address `:3000` with the default seed accounts, so set `bindAddr` explicitly whenever that file is absent or replaced.

## Build

```bash
go build ./...
```

## Run in Docker

```bash
docker build -t zapatos-erp-go .
docker run --rm -p 3001:3001 -v zapatos-erp-data:/data zapatos-erp-go
```

The container expects `/data` to be writable. The image ships with a symlink from `/app/data` to `/data` so the existing bootstrap contract keeps working.

## Bootstrap configuration

`config/bootstrap.json` may define:

- `bindAddr`
- `dataDir`
- `seedAccounts`

If the file is missing or invalid, the app falls back to a `:3000` bind address, the repository `data/` directory, and the built-in seed accounts.

## Deployment prerequisites

- A writable data directory
- The repository configuration or an equivalent mounted bootstrap file
- Network access to the configured bind address

## Operational notes

- The app stores all shared business data, audit events, and sessions in `data/zapatos.sqlite`. SQLite, its migration lock, and migration backups must live on a same-host local durable volume; instances sharing the data directory must run under the same UID. Cross-host/shared-network filesystems are not supported by this storage setup.
- On first startup, strict preflight validates `raw-materials.json`, `finished-goods.json`, `packaging.json`, `logistics.json`, `invoices.json`, and `audit.json` before SQLite schema creation. Missing business files use the existing bootstrap seeds (where defined); valid `[]` files remain empty. The exact source bytes are published under `data/migration-backups/legacy-json-v1/` before import. If `sessions.json` exists, its raw bytes are backed up but its tokens are never imported; users must sign in again after cutover.
- After the migration marker is complete, startup ignores legacy JSON entirely. There is no fallback to stale JSON if SQLite is unavailable or legacy files are changed.
- The canonical data directory, backup parent, and `legacy-json-v1` backup directory are owner-only (`0700`). The SQLite database, migration lock, backup files, and SQLite WAL/SHM sidecars are owner-only (`0600`). Keep all participating instances under the same UID so they can access these private files.
- `GET /health` and `GET /metrics` are unauthenticated probes.
- Request logs include request ID and trace correlation fields.
- 5xx responses are redacted to avoid leaking internal details.
- The first-run seed accounts are intended for local or controlled environments only.
- Owner-only SQLite/data/backup modes are supported on Linux, macOS, and BSD targets covered by the host-local file lock; startup fails closed on other platforms.
- The storage contract described above is what runs today. A later move to PostgreSQL is planned on paper only: nothing is implemented, configured, or connected. See [`docs/postgresql-migration-plan.md`](docs/postgresql-migration-plan.md).

## Documentation

- [`docs/product-roadmap.md`](docs/product-roadmap.md) — approved direction and the boundary between current and proposed work.
- [`docs/http-contract.md`](docs/http-contract.md) — HTTP endpoints, request fields, validation rules, role gates, and status codes.
- [`docs/security-backlog.md`](docs/security-backlog.md) — deferred security work; read it before exposing the app to untrusted networks.
- [`docs/postgresql-migration-plan.md`](docs/postgresql-migration-plan.md) — future PostgreSQL integration: storage seams and obstacles, configuration and connection, schema and migrations, data cutover, backups and rollback, testing, rollout, and the open questions that need an owner. Plan only; SQLite remains the running storage.
- [`docs/backup-restore-runbook.md`](docs/backup-restore-runbook.md) — what the data directory holds, how to take a consistent backup, how to restore it, the verification checklist, and the rehearsed evidence for that procedure.
- [`odd/tasks/product-readiness-2-3-4.md`](odd/tasks/product-readiness-2-3-4.md) — tracked work plan, decisions, checks, and commit and review evidence for this workstream.
