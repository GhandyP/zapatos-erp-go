# Design: First viability slice for Zapatos ERP

## Technical Approach

Add a thin bootstrap layer in `internal/app` that resolves defaults, loads optional bootstrap config, and wires all runtime dependencies from one place. Keep the current JSON-file repository model, but make module seed loading self-healing when files are missing or malformed. Move session and audit state to small JSON-backed stores with atomic snapshot writes, then wrap the stdlib HTTP server with conservative limits, recovery, and security headers.

## Architecture Decisions

### Decision: Keep bootstrap logic in `internal/app`

| Choice | Tradeoff | Decision |
|---|---|---|
| New `internal/app/config.go` with `AppConfig` and `LoadConfig(root)` | Slightly more code in `app`, but no new package boundary or dependency | Use the smallest bootstrap layer possible; `app.Run()` stays the composition root |
| Env-only config | Simpler deployment, but no explicit bootstrap artifact and harder to test defaults | Reject for this slice |
| Third-party config library | More features, but unnecessary surface area | Reject |

Rationale: the codebase is a small modular monolith, so the config layer should stay close to wiring and not become a framework.

### Decision: Retain JSON file storage and make seed fallback non-fatal

| Choice | Tradeoff | Decision |
|---|---|---|
| Keep `internal/store.JSONFileRepository` | No schema migration, but still file-based limits and no concurrent multi-process safety | Keep it for this slice |
| Migrate to SQLite/Postgres | Better durability, but too large for the first viability pass | Reject |

Rationale: the proposal is a viability slice, not a storage migration. Missing or malformed module files should load seed data in memory, let Packaging/Logistics initialize, and rewrite a clean JSON snapshot on the next save.

### Decision: Persist sessions and audit as small JSON snapshots

| Choice | Tradeoff | Decision |
|---|---|---|
| Full-file rewrite with temp file + rename | Simple and safe locally; write cost is fine at this scale | Use this for both sessions and audit |
| Append-only log / WAL | Better for growth, but more recovery logic | Reject |

Rationale: sessions and audit events are small enough that atomic snapshot writes give restart safety with minimal code. If a snapshot cannot be loaded, start empty and keep the app bootable.

### Decision: Wrap `net/http` instead of changing frameworks

| Choice | Tradeoff | Decision |
|---|---|---|
| Middleware around current `ServeMux` | Minimal churn, preserves existing handlers/tests | Use this |
| Framework migration | More features, but unnecessary risk | Reject |

Rationale: the current routing and SSR HTML flow already work; the slice only needs safer defaults.

## Data Flow

`main.go` → `app.Run()` → `LoadConfig()` → build repos/services → create file-backed session/audit stores → `server.NewHandler()` → middleware → `http.Server`.

On startup, repository load order is: file contents → seed fallback → clean in-memory state. On mutation, the service saves through the repository/store, which writes to `*.tmp` and renames atomically. On restart, sessions and audit reload from their JSON snapshots.

    bootstrap config ──→ app wiring ──→ repositories/stores ──→ HTTP handlers
                               │                                   │
                               └────────── atomic JSON files ──────┘

## File Changes

| File | Action | Description |
|---|---|---|
| `internal/app/config.go` | Create | Bootstrap config loading, defaults, and path resolution |
| `internal/app/app.go` | Modify | Use config-driven wiring and pass store paths into constructors |
| `internal/store/jsonfile.go` | Modify | Treat missing/malformed files as seed fallback, keep atomic writes |
| `internal/auth/auth.go` | Modify | Add file-backed session persistence and reload on startup |
| `internal/audit/audit.go` | Modify | Add durable audit snapshot storage |
| `internal/server/server.go` | Modify | Add middleware, body limits, recovery, and server timeouts |
| `config/bootstrap.json` | Create | Optional bootstrap defaults for bind address, data paths, and seed accounts |
| `data/packaging.json` | Create | First-run seed data for Packaging |
| `data/logistics.json` | Create | First-run seed data for Logistics |

## Interfaces / Contracts

```go
type AppConfig struct {
    BindAddr string
    DataDir   string
    Seeds     []auth.UserAccount
}
```

`limitedBody` should take `http.ResponseWriter` so oversized bodies return a real 413 instead of relying on a nil writer.

## Testing Strategy

| Layer | What to Test | Approach |
|---|---|---|
| Unit | config defaults, repo seed fallback, atomic persistence helpers | Table-driven tests in `internal/app`, `internal/store`, `internal/auth`, `internal/audit` |
| Integration | restart-safe session/audit reload, missing/malformed module JSON, server headers/body limits | `httptest` with temp dirs and real files |
| E2E | app boots, Packaging/Logistics remain available, login/audit survive restart | `go test ./...` plus restart-style handler tests |

## Migration / Rollout

No migration required. Existing JSON files remain the source of truth; this slice only adds safe bootstrap defaults and durable snapshots.

## Open Questions

- [ ] Should `config/bootstrap.json` be the default source, or should environment variables override it first?
- [ ] Should security headers stay minimal in this slice to avoid breaking the inline UI, or should the UI be externalized first for stricter CSP?
