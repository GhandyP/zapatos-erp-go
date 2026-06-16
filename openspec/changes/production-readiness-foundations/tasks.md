# Tasks: Production-readiness foundations

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~450–650 |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR 1 → PR 2 → PR 3 → PR 4 |
| Delivery strategy | feature-branch-chain |
| Chain strategy | feature-branch-chain |

Decision needed before apply: No
Chained PRs recommended: Yes
Chain strategy: feature-branch-chain
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|------|------|-----------|-------|
| 1 | CI/release gate + validation baseline | PR 1 | Base = feature/tracker branch; keep to workflows only. |
| 2 | Reproducible container packaging | PR 2 | Base = PR 1 branch; include boot smoke verification. |
| 3 | Observability + startup hardening | PR 3 | Base = PR 2 branch; wire server/app changes and tests. |
| 4 | Docs + HTTP contract | PR 4 | Base = PR 3 branch; finish with operator-facing docs. |

## Phase 1: Delivery foundation

- [x] 1.1 Add `.github/workflows/ci.yml` to run `go test ./...`, `go vet ./...`, and `go build ./...` on PRs and protected-branch pushes.
- [x] 1.2 Add `.github/workflows/release.yml` to gate tag releases on passing validation and publish a digest-traceable build.

## Phase 2: Container packaging

- [ ] 2.1 Add a multi-stage `Dockerfile` that builds the current Go binary and runs it as a nonroot user with `/data` writable.
- [ ] 2.2 Add `.dockerignore` to exclude tests, git metadata, and local build noise from the image context.
- [x] 2.1 Add a multi-stage `Dockerfile` that builds the current Go binary and runs it as a nonroot user with `/data` writable.
- [x] 2.2 Add `.dockerignore` to exclude tests, git metadata, and local build noise from the image context.

## Phase 3: Observability + ops safety

- [ ] 3.1 Create `internal/observability/*` for structured request logs, request IDs/trace header passthrough, and lightweight JSON metrics.
- [ ] 3.2 Wire `internal/server/server.go` to wrap handlers, expose `/metrics`, and keep `/health` unauthenticated with client-safe 4xx/5xx bodies.
- [ ] 3.3 Harden `internal/app/config.go` and `internal/app/app.go` to validate startup inputs and fail fast on an unwritable data dir.

## Phase 4: Docs + verification

- [x] 4.1 Add `README.md` with local run, container run, and deployment prerequisites.
- [x] 4.2 Add `docs/http-contract.md` covering health, auth, core endpoints, and common error responses.
- [x] 4.3 Add/update tests in `internal/app/*` and `internal/server/*` for: CI commands, container boot assumptions, trace correlation, metrics shape, and secret-safe errors.

## Deferred to later changes

- Keep full monitoring stack/vendor integration, SBOM/signing, and deployment-manifest expansion out of this slice unless review budget drops below risk.
