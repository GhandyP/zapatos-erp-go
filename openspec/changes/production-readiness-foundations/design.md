# Design: Production-readiness foundations

## Technical Approach

Keep the runtime exactly as-is (stdlib HTTP + file-backed state) and add a thin platform layer around it: CI validates every change, a multi-stage container packages the same binary, observability is added at the HTTP boundary, and docs become the public contract. The implementation stays reviewable by slicing cross-cutting work into isolated files and packages.

## Architecture Decisions

| Decision | Choice | Alternatives considered | Rationale |
|---|---|---|---|
| CI/release gate | Two GitHub workflows: `ci.yml` on PRs/branch pushes, `release.yml` on tags | One large workflow; external CI | Separates merge safety from publish flow and keeps release traceable to a tagged commit. |
| Container runtime | Multi-stage Dockerfile with a nonroot final image and writable `/data` volume | Single-stage image; `scratch` | Multi-stage keeps the binary small while preserving a predictable runtime user and a clear state directory. |
| Observability boundary | New `internal/observability` package using `log/slog`, request IDs, trace header pass-through, and JSON metrics | Third-party tracing/metrics stack | Stdlib-only instrumentation fits the repo style and avoids adding operational dependencies. |
| Error handling | Client-safe 4xx/5xx responses; panic recovery logs internals server-side only | Return raw errors everywhere | Prevents leaking stack traces, paths, tokens, or passwords while keeping operators informed. |

## Data Flow

    PR/Tag → GitHub Actions → tests/vet/build → container build → release artifact
    
    Request → observability middleware → route handler → JSON response
                    │                 └→ JSON log + counters + trace/request IDs
                    └→ health/metrics endpoints stay unauthenticated

Startup flow:

    bootstrap.json + defaults → validate config → verify writable data dir → build stores → serve

## File Changes

| File | Action | Description |
|---|---|---|
| `.github/workflows/ci.yml` | Create | PR/branch validation: `go test ./...`, `go vet ./...`, `go build ./...`. |
| `.github/workflows/release.yml` | Create | Tag-gated release that rebuilds from the tagged commit and publishes the image digest. |
| `Dockerfile` | Create | Multi-stage build; final nonroot runtime with `/data` mounted for file-backed state. |
| `.dockerignore` | Create | Exclude test/build noise from the image context. |
| `internal/observability/*` | Create | Request logging, correlation, and lightweight JSON metrics. |
| `internal/server/server.go` | Modify | Wrap handler with observability, add `/metrics`, and redact internal 500s. |
| `internal/app/config.go` | Modify | Validate bootstrap config and keep startup defaults explicit. |
| `internal/app/app.go` | Modify | Fail fast on invalid config and unwritable data dir before serving. |
| `README.md` and `docs/http-contract.md` | Create | Local run, deploy, and public HTTP contract documentation. |

## Interfaces / Contracts

```go
type BootstrapConfig struct { BindAddr, DataDir string; SeedAccounts []SeedAccount }
func (c BootstrapConfig) Validate() error

type Middleware struct{}
func (m Middleware) Wrap(next http.Handler) http.Handler

type Metrics struct{}
func (m *Metrics) Snapshot() map[string]any
```

`/health` remains unauthenticated and stable. `/metrics` returns machine-readable JSON with request counts, in-flight requests, and selected runtime stats. Trace correlation uses incoming `traceparent`/`X-Request-Id` when present and generates an ID otherwise.

## Testing Strategy

| Layer | What to Test | Approach |
|---|---|---|
| Unit | config validation, middleware logging/IDs, metrics snapshot, safe error responses | Table-driven tests in `internal/app/*` and `internal/server/*`. |
| Integration | startup with missing/malformed config, unwritable data dir, health/metrics exposure | `httptest` plus temp dirs and persisted files. |
| E2E | container boots, serves `/health`, preserves file-backed state across restart | Build/run the image locally in CI or a smoke job. |

## Migration / Rollout

No data migration required. Roll out in slices: (1) CI/release workflows, (2) Docker packaging, (3) observability + startup hardening, (4) docs. Each slice is independently mergeable and preserves the current local runtime.

## Open Questions

- None.
