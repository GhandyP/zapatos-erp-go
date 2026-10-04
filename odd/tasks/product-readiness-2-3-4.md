# ERP Product Readiness: API, Data Integrity, Manufacturing

**Status:** in progress
**Feature branch:** `feat/erp-readiness-2-3-4`
**Goal:** Advance the ERP in three staged workstreams: align the HTTP contract, establish reliable data/audit behavior, and build manufacturing workflows only after the factory process is validated. This is not one combined implementation diff.

## Scope and decisions

- This plan maps the user's prior items **2, 3, and 4** to: (2) API-contract alignment, (3) data-integrity/audit, and (4) manufacturing workflow discovery and implementation.
- The API contract is the first work unit. Treat current handler behavior as the initial source of truth; change runtime behavior only if a separately justified requirement is approved.
- Data-integrity work must first define the supported concurrency/deployment model. Do not choose a database or promise multi-instance safety by assumption.
- Manufacturing discovery is a gate. Validate terminology, variants, units, roles, exceptions, and reports with factory users before committing to workflow models or broad implementation.
- Keep security remediation deferred in `docs/security-backlog.md`; document actual HTTP behavior without silently representing current auth behavior as hardened.
- FoxPro integration is outside this initiative unless its scope is explicitly reopened.
- The previous portfolio UI work is committed as `bf8cf099a8051e131b51e033be3bf631d506907e` (`feat(ui): complete raw material entry workflow`).

## Phased tasks

| ID | Task | Status | Evidence / gate |
|---|---|---|---|
| 1 | Align `docs/http-contract.md` with implemented routes and add contract-level checks. | done | RED observed; `go test ./internal/server -count=1`, `go test ./...`, `go vet ./...`, `go build ./...`, and scoped `git diff --check` passed. Native review lineage `review-c0853f7ed637764b` was approved and acknowledged. Commit `bec15a1f227f878ac4daf010587ff6ea3e83d99e` was pushed to `origin/feat/erp-readiness-2-3-4`; only the API contract and server tests were committed. |
| 2 | Map data-integrity and audit failure modes; propose supported storage/concurrency guarantees. | pending | Must cover failed writes, corrupt snapshots/seed fallback, audit durability/retention, backup/restore, and current JSON limits. |
| 3 | Resolve the operating-model decision before storage changes. | pending | Ask the user to choose the required single-process vs multi-process/multi-instance guarantee after presenting trade-offs. |
| 4 | Discover and select one end-to-end manufacturing MVP with factory users. | pending | Confirm a real workflow, roles, units, variants, exceptions, and success reports; avoid modeling the full ERP speculatively. |
| 5 | Implement the first user-validated manufacturing workflow in bounded work units. | pending | Candidate slices may include versioned BOM/routings, work orders, material movements/consumption, lot traceability, quality/scrap/rework, purchasing, or sales/delivery; selection follows task 4. |
| 6 | Verify each work unit, update this record, and record delivery decisions. | pending | Run applicable tests/builds; document skipped checks and any user-approved commit identity. |

## API contract acceptance criteria

- Every documented route, method, request/response shape, authentication rule, status, and error response matches the server/UI or is explicitly marked unsupported.
- Invoice and FoxPro paths reflect current handlers; no undocumented behavior is introduced just to match stale documentation.
- Tests cover representative route/method/auth/status behavior so contract drift is detectable.
- Existing security-related status inconsistencies are described accurately and remain in the deferred security backlog unless scope is explicitly changed.

## Data-integrity decision gate

Before implementation, provide a short decision brief comparing:

- A documented single-process, single-writable-data-directory model with JSON snapshots; and
- A transactional persistence model if concurrent processes, multiple instances, stronger recovery, or cross-resource atomicity are required.

The brief must state concrete trade-offs and the effect on write failures, corruption recovery, audit durability, backups, and migrations. No production storage migration is implied by this task plan.

## Manufacturing discovery gate

The first implementation must follow a real end-to-end scenario agreed with factory users. Record its actors, states, data required at each step, exception paths, reconciliation/reporting needs, and acceptance examples. Do not implement BOM, MRP, purchase, production, quality, and sales as one large change.

## Work-unit evidence

- Previous portfolio work: `bf8cf099a8051e131b51e033be3bf631d506907e`.
- API contract phase: `bec15a1f227f878ac4daf010587ff6ea3e83d99e` — `docs(api): align HTTP contract with handlers`; pushed to `origin/feat/erp-readiness-2-3-4`.
- Native review: lineage `review-c0853f7ed637764b` reached approved and the exact acknowledgement burned authority. The commit includes only `docs/http-contract.md` and `internal/server/server_test.go`; unrelated local `.pi`/`.codegraph` state was preserved.
- Verification: `go test ./...`, `go vet ./...`, `go build ./...`, and scoped `git diff --check` passed. Runtime behavior did not change.
- Data-integrity decision: pending assessment and user decision on supported concurrency/storage guarantees.
- Manufacturing workflow: pending factory validation.
