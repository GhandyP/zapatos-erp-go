# Proposal: Production-readiness foundations

## Intent

Close the remaining delivery/ops gaps so Zapatos ERP can be built, packaged, observed, and run consistently outside a dev shell. Runtime foundations already exist; what’s missing is the release and support layer needed for safe deployment.

## Scope

### In Scope
- Add CI/CD workflows for test/build/release gating.
- Add Docker packaging for a reproducible runtime image.
- Add observability baseline: structured logs, metrics/health hooks, trace-ready middleware.
- Add README/docs for local run, deployment, and API/contract overview.
- Add minimal security/ops follow-ons where they fit naturally (env validation, secret/runtime hygiene).

### Out of Scope
- New business features or UI workflows.
- Storage migration away from file-backed state.
- Full monitoring stack/vendor integration.

## Capabilities

### New Capabilities
- `ci-cd-foundation`: automated build/test/release workflow.
- `container-packaging`: reproducible Docker image/runtime packaging.
- `observability-baseline`: structured logging, metrics, and trace hooks.
- `docs-api-contract`: onboarding, run, and API contract documentation.
- `operations-security-baseline`: minimal operational hardening and deployment hygiene.

### Modified Capabilities
- None.

## Approach

Implement this as a platform slice, not a single monolith: first CI and image build, then observability at the app/server boundary, then docs/contracts. Keep each capability isolated so implementation can be split into reviewable PRs.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `.github/workflows/*` | New | CI/release automation |
| `Dockerfile` | New | Container runtime packaging |
| `internal/server/*`, `internal/app/*` | Modified | Logging/metrics/health/tracing hooks |
| `README.md`, `docs/*` | New | Runbook, API contract, onboarding |
| `go.mod`, `go.sum` | Modified | Any support dependencies |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| CI or image changes block delivery | Medium | Keep workflows minimal and verify locally first |
| Observability adds noise or coupling | Medium | Use thin, interface-friendly hooks |
| Docs drift from runtime behavior | Low | Generate docs from the implemented contract where possible |

## Rollback Plan

Revert the workflows, Dockerfile, observability hooks, and docs in one change or per slice. The app should continue to run with the current stdlib HTTP/file-backed runtime.

## Dependencies

- No external platform is required to start; deployment targets can be chosen during implementation.

## Success Criteria

- [ ] CI validates test/build on every change.
- [ ] The app builds and runs from a container image.
- [ ] Operators can find run, API, and observability guidance in repo docs.
