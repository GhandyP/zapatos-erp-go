# Product Roadmap

**Status:** approved direction; proposed work is not implemented unless explicitly marked **Current** below.

This roadmap orders work toward a credible, bounded portfolio demo and a more useful shoe-manufacturing product. It records repository evidence, not validated factory requirements. Confirm terminology, controls, and workflow order with factory users before committing to the manufacturing scope.

> **Security is deferred.** This roadmap does not implement security work or imply that security is complete. See [`security-backlog.md`](security-backlog.md). Do not expose the app to untrusted networks until that backlog is addressed.

## Current state

- **Current — storage:** all business records, audit events, and sessions live in `data/zapatos.sqlite` (pure-Go SQLite, WAL, one host, one writable volume, every instance under the same UID). Startup runs a strict one-time JSON import gate that validates and backs up the legacy files byte-for-byte before creating the database, and ignores them once its completion marker exists. A business mutation and its audit event commit in the same transaction. See the [README](../README.md) operational notes and the [backup and restore runbook](backup-restore-runbook.md).
- **Current — HTTP contract:** `docs/http-contract.md` matches the handlers and the UI, including `/api/invoices` with `POST /api/invoices/{id}/issue` and `GET /api/foxpro` with `POST /api/foxpro/sync`; the stale `/api/billing` and `/api/foxpro/status` paths are gone, and contract checks cover representative route, method, authorization, and status behaviour.
- **Current — modules and UI:** the single-page Spanish UI covers raw materials, finished goods with stock adjustments, packaging, logistics, billing/invoice, manufacturing, audit with CSV export, and the FoxPro adapter. Roles are granted in `internal/core/permissions.go` and enforced again on every request.
- **Current — provisional manufacturing prototype:** one material, one BOM line per finished variant, and a work-order lifecycle (`draft → released → completed`) in which completion consumes `planned quantity × quantity per unit` from material stock and increments finished stock inside one transaction. Administrator-only opening stock and BOM writes; production may read manufacturing data and run work orders. Browser-verified against an isolated instance, and **not validated with factory users**.
- **Current — runtime defaults:** the checked-in `config/bootstrap.json` binds `:3001` (a wildcard listener, not a loopback-only promise) with data directory `data/`; if that file is missing or unparsable, the built-in fallback is `:3000`.
- Invoices still carry totals and status but no line items; there are no lots, scrap, rework, or costing.
- CI, Docker packaging, the README, and request/metrics observability exist (`.github/workflows/`, `Dockerfile`, `README.md`, `internal/observability/`). Their existence is not proof of a verified operational demo or production readiness.

## Prioritized phases

### 0. Current — make the raw-material entry slice demoable

**Code status:** implemented in the current UI/service, now backed by SQLite; remaining work is user validation and a repeatable demo run-through.

- Confirm the fields, units, dimension requirements, and minimum-stock meaning with a factory user.
- Demonstrate a valid create with a non-negative integer minimum stock, rejection of invalid dimensions/stock, and a visible API failure using a resettable demo dataset.
- Keep server-side validation and authorization authoritative; the browser checks are only usability aids.

**Exit criteria:** a user can complete the agreed raw-material entry scenario; invalid data is rejected by the server; the UI explains failed submissions; the demo dataset can be restored. Do not represent this as inventory accounting or production planning.

### 1. API-contract alignment (#4)

**Done.** Commit `bec15a1f227f878ac4daf010587ff6ea3e83d99e` reconciled `docs/http-contract.md` with the server and the UI, and later work units extended it with the manufacturing endpoints and their role gates. The contract now documents actual paths, methods, authentication, request/response shapes, validation, status codes, and error behaviour for login, probes, raw materials, finished goods, packaging, logistics, invoices, manufacturing, audit, and FoxPro status/sync, and it has native review approval.

**Acceptance criteria:** met for the documented surface — every documented route is implemented with the documented method and authorization rule, UI requests match the contract, and tests cover representative route, method, authorization, and status behaviour so drift is detectable.

### 2. Audit and storage data integrity (#2)

**Resolved and implemented.** The supported operating model is decided and documented: several instances may share one local durable data directory on a single host under the same UID, and cross-host or shared-network storage is unsupported. Business and audit state moved to SQLite; every mutation runs through one transaction scope so a business change and its audit event commit together or not at all, and a failure surfaces as a `500` instead of a silent success. After the migration marker is complete there is no fallback to stale JSON, so a missing or unreadable database fails closed rather than appearing to preserve records.

**Remaining, explicitly not verified:** no crash-injection or disk-full test proves the durability boundary at the filesystem level; multi-instance concurrency is covered by the migration lock and WAL behaviour but not by a sustained concurrent-writer test; and the audit cap of 500 events applies to the writer, not to rows inserted by the one-time import.

**Acceptance criteria:** the operating model is explicit; mutations and audit events share one durability boundary; recovery is documented and exercised; storage failures are observable. Backup and restore is covered by [`backup-restore-runbook.md`](backup-restore-runbook.md) and its rehearsed evidence. File-permission and credential-protection work stays in [`security-backlog.md`](security-backlog.md).

### 3. Validate and phase in manufacturing workflows

**Prototype implemented; factory discovery is still the gate.** The provisional slice below is built and browser-verified, and it must not be presented as a validated manufacturing model: one material, one BOM line per variant, integer quantities, and consumption based on the planned quantity. Map the actual factory's roles, vocabulary, units, variants, exception paths, paper/spreadsheet records, and required reports, then adjust the prototype with users before expanding it.

| Order | Workflow slice | Status |
|---|---|---|
| 3a | Bills of materials (BOMs) and routings | Prototype: one BOM line per variant with a revision; no operation sequence |
| 3b | Work orders | Prototype: draft → released → completed with planned/good quantities; no dates or routing revision |
| 3c | Material movements and consumption | Partial: opening stock and consumption movements update one integer balance; no receipts, returns, or lots |
| 3d | Lot traceability | Proposed |
| 3e | Quality, scrap, and rework | Proposed |
| 3f | Purchasing and suppliers | Proposed |
| 3g | Customer orders, delivery, and invoice lines | Proposed |
| 3h | Costing | Proposed |

**Acceptance criteria for the phase:** factory users validate the modeled happy path and material exception cases; the slice entities in the prototype table are revised where the factory disagrees; every expanded slice gets role-specific acceptance examples and reconciled quantities. Do not start all slices as one large redesign.

### 4. FoxPro integration — scope decision first

**Proposed decision gate.** The current adapter reports issued invoices as pending/synced in application memory; it does not establish a connection or exchange data with an external FoxPro system.

- If factory users confirm an external FoxPro dependency, define ownership, data direction, field mapping, identifiers, conflict handling, retry/idempotency behavior, and reconciliation before building a connector.
- If it is not in scope, label the adapter as a simulation or remove it from product claims and the demo path.

**Acceptance criteria if approved:** a documented mapping and repeatable test fixture cover retries, duplicate delivery, partial failure, and reconciliation; demo sync cannot accidentally write to a live external system.

### 5. Operational and portfolio-demo readiness

**Partly done.** Reuse existing CI, Docker packaging, and observability rather than duplicating them as new baseline features. The backup/restore runbook and the supported topology are complete; the Docker smoke run and the demo data and scripted run-through are pending.

- **Done — backup and restore runbook:** [`backup-restore-runbook.md`](backup-restore-runbook.md) documents what the data directory holds, how to take a consistent backup, how to restore it, and what to verify; the procedure was rehearsed end to end (identical work orders, material balance, and finished stock after a wipe and restore, with the original file modes restored).
- **Done — supported topology:** the single-host, same-UID, local-durable-volume contract is documented in the README operational notes, the runbook, and the [PostgreSQL plan](postgresql-migration-plan.md).
- **Pending — Docker smoke run:** build the image, run it with a writable volume, confirm startup, a write, persistence across restart, and a clean shutdown.
- **Pending — resettable demo data and a scripted run-through:** clearly fictional data plus a short walkthrough covering the manufacturing slice and the modules that exist, starting from a clean state.
- **Pending — OpenSpec reconciliation:** the Spanish `openspec/changes/la idea es ver que le falta a este programa para estar listo y funcional (usa perplexity concil para buscar idea que pudieran ser utiles , ideas elementos y estructuras que sean utiles )/` proposal/design/tasks and `openspec/changes/la-idea-es-ver-que-le-falta-a-este-programa-para-estar-listo-y-funcional/exploration.md` contain stale statements that modules or CI/Docker/README/observability are absent, alongside tasks now marked complete. The repository already contains those modules and platform files/specs. Reconcile or archive stale/duplicate tasks; do not revive the dropped Perplexity request or count implemented CI, Docker, and observability as missing work.

**Acceptance criteria:** a fresh documented demo setup is repeatable; a restore has been exercised; the demo script works from a clean/reset state; known limitations and the security warning are visible to reviewers.

## Readiness definitions and non-goals

- **Portfolio-demo-ready** means a bounded, scripted demonstration works on a controlled/local environment with fictional data, a known reset/restore path, accurate route documentation, and clear limitations. It is not evidence of factory fit, safe public exposure, or production suitability.
- **Production-ready** additionally requires factory-validated workflows, the approved storage/concurrency and recovery model, operational support and monitoring, and completion/review of the deferred security backlog. **Security is not complete today.**
- This roadmap does not authorize security implementation, external research, a FoxPro connection without an explicit scope decision, or a production storage migration without the Phase 2 decision. See [`security-backlog.md`](security-backlog.md) for deferred security work.
