# Product Roadmap

**Status:** approved direction; proposed work is not implemented unless explicitly marked **Current** below.

This roadmap orders work toward a credible, bounded portfolio demo and a more useful shoe-manufacturing product. It records repository evidence, not validated factory requirements. Confirm terminology, controls, and workflow order with factory users before committing to the manufacturing scope.

> **Security is deferred.** This roadmap does not implement security work or imply that security is complete. See [`security-backlog.md`](security-backlog.md). Do not expose the app to untrusted networks until that backlog is addressed.

## Current state

- **Current — raw-material entry flow:** the inline UI served at `/` and `/app` can submit raw materials to `POST /api/raw-materials`. It collects ID, name, unit, non-negative integer minimum stock, and positive length/width/height plus non-negative weight. The service validates required fields and dimensions; the UI displays submit errors. This is a focused entry slice, not a purchasing, inventory-movement, or consumption workflow.
- The Go application has basic raw-material, finished-goods, packaging, logistics, billing/invoice, audit, and FoxPro-adapter areas. Domain records are JSON-file-backed; invoices have totals/status but not invoice lines. Finished-goods stock adjustments exist, but they are not a manufacturing material ledger.
- `docs/http-contract.md` does not fully match the handlers/UI: it documents `/api/billing` and `/api/foxpro/status`, while the current UI and server use `/api/invoices` (including `POST /api/invoices/{id}/issue`) and `GET /api/foxpro`; sync is `POST /api/foxpro/sync`. Align the contract before treating it as a reliable integration guide.
- The documented runtime defaults are bind address `:3001` and data directory `data/`; bootstrap settings are described in `config/bootstrap.json`. `:3001` is a wildcard listener, not a loopback-only promise.
- CI, Docker packaging, a README, and request/metrics observability already exist (`.github/workflows/`, `Dockerfile`, `README.md`, `internal/observability/`). Their existence is not proof of a verified operational demo or production readiness.

## Prioritized phases

### 0. Current — make the raw-material entry slice demoable

**Code status:** implemented in the current UI/service; remaining work is user validation and a repeatable demo run-through.

- Confirm the fields, units, dimension requirements, and minimum-stock meaning with a factory user.
- Demonstrate a valid create with a non-negative integer minimum stock, rejection of invalid dimensions/stock, and a visible API failure using a resettable demo dataset.
- Keep server-side validation and authorization authoritative; the browser checks are only usability aids.

**Exit criteria:** a user can complete the agreed raw-material entry scenario; invalid data is rejected by the server; the UI explains failed submissions; the demo dataset can be restored. Do not represent this as inventory accounting or production planning.

### 1. API-contract alignment (#4)

**Proposed.** Reconcile `docs/http-contract.md`, the server handlers, and the inline UI before adding more consumers or screens.

- Document actual paths, methods, authentication, request/response shapes, validation, status codes, and error behavior for login, probes, raw materials, finished goods, packaging, logistics, invoices, audit, and FoxPro status/sync.
- Replace stale billing/FoxPro paths with the current invoice and adapter routes, or deliberately change the implementation in a separately scoped work unit.
- Add contract-level examples and checks so future handler and documentation changes remain in agreement.

**Acceptance criteria:** every documented route is implemented with the documented method and auth rule; UI requests match the contract; errors and response payloads are represented accurately; stale aliases are explicitly described as unsupported or intentionally retained.

### 2. Audit and storage data integrity (#2)

**Proposed.** Decide what reliability the product needs before adding workflows that depend on trustworthy stock and audit history. The current JSON repositories use in-process synchronization and snapshot writes; that does not decide or provide safe multi-process access. Audit persistence errors are currently ignored by the recording path, and history is bounded.

- Define the supported operating model: one process on one writable data directory, or concurrent/multi-instance operation. Select and document JSON constraints or a migration target (for example, a transactional database) based on that decision.
- Make failed writes visible and define what happens when a write, rename, disk-full event, or restart interrupts a change. Prevent seed fallback or malformed-file recovery from silently appearing to preserve live records.
- Define audit ordering, retention, durability, and recovery expectations; ensure important business mutations cannot report success while their required audit record is silently lost.
- Specify consistent backup/restore and migration behavior for all business and audit/session state. Test crash/restart, malformed snapshots, concurrent writers within the supported model, and restoration from backup.

**Acceptance criteria:** the single- or multi-process guarantee is explicit; mutations and audit events have a documented durability boundary; recoveries preserve or clearly identify lost/corrupt data; backup/restore is repeatable; storage failures are observable. File-permission and credential-protection work is tracked separately in [`security-backlog.md`](security-backlog.md), not duplicated here.

### 3. Validate and phase in manufacturing workflows

**Proposed; factory discovery is a gate, not an assumption.** First map the actual factory's roles, vocabulary, units, variants, exception paths, paper/spreadsheet records, and required reports. Prototype one end-to-end path with users before expanding the model.

| Order | Workflow slice | Acceptance criteria |
|---|---|---|
| 3a | Bills of materials (BOMs) and routings | A product/version identifies required materials, quantities, units, and operation sequence; revisions are traceable and usable for a real agreed example. |
| 3b | Work orders | A work order identifies product/variant, planned quantity, dates, status, and applicable BOM/routing revision; status transitions are validated with users. |
| 3c | Material movements and consumption | Receipts, issues, returns, and consumption update stock through attributable movements; duplicate or invalid movements are rejected and balances can be reconciled. |
| 3d | Lot traceability | Material lots can be followed from receipt through consumption to finished lots/orders, with a usable forward/backward trace for a sample. |
| 3e | Quality, scrap, and rework | Inspections and dispositions capture pass/fail, scrap, rework, and the effect on quantities/status; an agreed exception scenario is auditable. |
| 3f | Purchasing and suppliers | Supplier and purchase-order lifecycle covers the factory's agreed request/order/receipt cases and links receipts to lots and stock. |
| 3g | Customer orders, delivery, and invoice lines | Customer orders connect to available/produced goods, delivery confirmation, and invoice line items; quantities and totals reconcile through the flow. |
| 3h | Costing | Material, labor/operation, scrap/rework, and other agreed cost inputs are traceable to a product/order; users can explain a sample calculation. |

**Acceptance criteria for the phase:** factory users validate the modeled happy path and material exception cases; every slice has role-specific acceptance examples and reconciled quantities; reports and audit history support the agreed operational questions. Do not start all slices as one large redesign.

### 4. FoxPro integration — scope decision first

**Proposed decision gate.** The current adapter reports issued invoices as pending/synced in application memory; it does not establish a connection or exchange data with an external FoxPro system.

- If factory users confirm an external FoxPro dependency, define ownership, data direction, field mapping, identifiers, conflict handling, retry/idempotency behavior, and reconciliation before building a connector.
- If it is not in scope, label the adapter as a simulation or remove it from product claims and the demo path.

**Acceptance criteria if approved:** a documented mapping and repeatable test fixture cover retries, duplicate delivery, partial failure, and reconciliation; demo sync cannot accidentally write to a live external system.

### 5. Operational and portfolio-demo readiness

**Proposed.** Reuse existing CI, Docker packaging, and observability rather than duplicating them as new baseline features.

- Complete the backup/restore runbook and a restore rehearsal using the full configured data directory.
- Resolve the storage/migration and concurrency decision from Phase 2 and document the supported deployment topology.
- Perform a Docker smoke run with a writable volume and the documented bootstrap/default configuration; confirm startup, the chosen demo flow, persistence after restart, and clean shutdown.
- Create resettable, clearly fictional demo data and a short scripted run-through covering the raw-material entry slice and only the other workflows that actually exist.
- Reconcile OpenSpec plans with repository reality before opening follow-up work. The Spanish `openspec/changes/la idea es ver que le falta a este programa para estar listo y funcional (usa perplexity concil para buscar idea que pudieran ser utiles , ideas elementos y estructuras que sean utiles )/` proposal/design/tasks and `openspec/changes/la-idea-es-ver-que-le-falta-a-este-programa-para-estar-listo-y-funcional/exploration.md` contain stale statements that modules or CI/Docker/README/observability are absent, alongside tasks now marked complete. The repository already contains those modules and platform files/specs. Reconcile or archive stale/duplicate tasks; do not revive the dropped Perplexity request or count implemented CI, Docker, and observability as missing work.

**Acceptance criteria:** a fresh documented demo setup is repeatable; a restore has been exercised; the demo script works from a clean/reset state; known limitations and the security warning are visible to reviewers.

## Readiness definitions and non-goals

- **Portfolio-demo-ready** means a bounded, scripted demonstration works on a controlled/local environment with fictional data, a known reset/restore path, accurate route documentation, and clear limitations. It is not evidence of factory fit, safe public exposure, or production suitability.
- **Production-ready** additionally requires factory-validated workflows, the approved storage/concurrency and recovery model, operational support and monitoring, and completion/review of the deferred security backlog. **Security is not complete today.**
- This roadmap does not authorize security implementation, external research, a FoxPro connection without an explicit scope decision, or a production storage migration without the Phase 2 decision. See [`security-backlog.md`](security-backlog.md) for deferred security work.
