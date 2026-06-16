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

- `GET /health` and `GET /metrics` are unauthenticated probes.
- Request logs include request ID and trace correlation fields.
- 5xx responses are redacted to avoid leaking internal details.
- The first-run seed accounts are intended for local or controlled environments only.
