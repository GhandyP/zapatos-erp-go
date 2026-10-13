# Tasks: First viability slice for Zapatos ERP

## Review Workload Forecast

| Field | Value |
|---|---|
| Estimated changed lines | 450-650 |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR 1 bootstrap + seeds → PR 2 durable session/audit → later hardening |
| Delivery strategy | ask-on-risk |
| Chain strategy | pending |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: pending
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|---|---|---|---|
| 1 | Boot/config + seed unblock | PR 1 | Base on main; includes config and seed JSON |
| 2 | Durable sessions + audit | PR 2 | Base on PR 1; restart-safe state only |
| 3 | Server hardening | Later change | Defer from this viability slice |

## Phase 1: RED — startup and seed tests

- [x] 1.1 Add config tests in `internal/app/app_test.go` or `internal/app/config_test.go` for missing/partial bootstrap values and default bind/data paths.
- [x] 1.2 Add seed-fallback tests in `internal/store/jsonfile_test.go` for missing, empty, and malformed `data/packaging.json` and `data/logistics.json`.
- [x] 1.3 Add restart-safety tests in `internal/auth/auth_test.go` and new `internal/audit/audit_test.go` for persisted sessions and ordered audit reload.

## Phase 2: GREEN — implementation

- [x] 2.1 Create `internal/app/config.go` and wire `internal/app/app.go` to `config/bootstrap.json`, `BindAddr`, and `DataDir`-derived paths.
- [x] 2.2 Extend `internal/store/jsonfile.go` to self-heal on missing/malformed module files and keep atomic temp-file writes.
- [x] 2.3 Extend `internal/auth/auth.go` to snapshot sessions to `data/sessions.json` and reload them on startup.
- [x] 2.4 Extend `internal/audit/audit.go` to snapshot events to `data/audit.json` and reload in timestamp order.
- [x] 2.5 Add `config/bootstrap.json`, `data/packaging.json`, and `data/logistics.json` with first-run seed content.

## Phase 3: Integration / verification

- [x] 3.1 Update `internal/app/app_test.go` and `internal/server/server_test.go` to prove Packaging/Logistics boot from seeds and restart-safe auth/audit still work.
- [x] 3.2 Run `go test ./...` and `go build ./...`, then fix any regressions before merge.

## Deferred to later change

- [x] D.1 Add `internal/server/server.go` timeouts, panic recovery, security headers, and request-size limits.
- [x] D.2 Add the matching header/body-limit/panic coverage in `internal/server/server_test.go`.
