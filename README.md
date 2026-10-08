# zapatos-erp-go

Go-based ERP prototype for shoe manufacturing and operations.

## Requirements

- Go 1.25+
- Writable `data/` directory
- `config/bootstrap.json` present in the repository root

## Run locally

```bash
go run ./cmd/zapatos-erp
```

Defaults:

- bind address: `:3001`
- data directory: `data/`

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

If the file is missing, the app falls back to safe defaults.

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
