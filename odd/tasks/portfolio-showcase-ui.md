# Portfolio Showcase UI and Product Roadmap

**Status:** implementation, verification, and native review complete; uncommitted  
**Feature branch:** `feat/portfolio-showcase-ui`  
**Goal:** Make the raw-material entry flow reliable and presentable as the first portfolio showcase, then leave a sequenced product roadmap and an explicit deferred-security backlog in `docs/`.

## Scope and decisions

- The raw-material creation flow is the first product priority.
- Include the dimensions required by the domain, non-negative stock validation, visible API failures, and role-appropriate write controls.
- Document the remaining API-contract, data-integrity/audit, footwear-manufacturing, integration, and operational work as prioritized follow-up; do not implement those follow-ups in this unit.
- Defer security remediation. Record it separately with concrete acceptance criteria; do not imply the application is production-safe.
- Do not use Perplexity or other external research for this work.
- Preserve pre-existing `.pi/settings.json`, `.pi/subagents.json`, and `.codegraph/` state.

## Tasks

| ID | Task | Status | Evidence |
|---|---|---|---|
| 1 | Add a focused failing test for the raw-material entry form and its required dimension controls. | done | RED observed: `go test ./internal/web -run '^TestRawMaterialFormRequiresValidDimensions$' -count=1` failed at the missing `lengthCm` control. |
| 2 | Complete the raw-material UI flow with valid dimensions, stock constraints, visible request errors, and role-appropriate controls. | done | `go test ./internal/web -count=1` passed after final fix. RED was observed for missing dimensions and fractional minimum-stock steps. Browser smoke was unavailable because the installed Playwright driver could not start (`playwright/cli.js` missing). |
| 3 | Write `docs/product-roadmap.md` and `docs/security-backlog.md` with prioritized scope and acceptance criteria. | done | Both approved docs were written and reviewed; minimum stock is documented as integer and file modes are qualified by umask. |
| 4 | Run focused and repository checks; inspect final diff and record any unavailable checks. | done | `go test ./...`, `go vet ./...`, `go build ./...`, and `git diff --check` passed; final source diff reviewed. Browser smoke unavailable (Playwright driver missing). Native review completed and acknowledged; one informational reliability warning remains. |

## Acceptance criteria

- A user with write permission can enter a raw material with valid dimensions and submit a request matching the domain/API contract.
- Invalid numeric values are blocked or explained before submission; API failures are shown in the UI.
- A read-only role is not offered a misleading create action.
- Roadmap and deferred-security documents are clear, non-duplicative, consistent with implemented code and existing OpenSpec, and distinguish implemented baseline from future work.
- Applicable tests and checks pass; limitations are recorded.

## Work-unit evidence

- Tests: `go test ./internal/web -count=1` and `go test ./...` passed; `go vet ./...`, `go build ./...`, and `git diff --check` passed.
- Documentation review: `docs/product-roadmap.md` and `docs/security-backlog.md` read and reviewed.
- Browser check: not run; Playwright driver could not load `/usr/share/nodejs/playwright/cli.js`.
- Native review: lineage `review-4c8d7457688d7513` reached `approved` and the exact acknowledgement burned authority for the reviewed candidate. Four lenses completed; advisory `R3-001` is an informational reliability warning at `internal/web/ui.go:22` (no correction transition was offered). The user explicitly authorized including the pre-existing `.pi/settings.json` change in this review.
- Commit identity: none; no explicit commit request was given.
