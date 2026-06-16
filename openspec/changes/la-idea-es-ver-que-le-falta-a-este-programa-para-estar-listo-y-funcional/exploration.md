## Exploration: la idea es ver que le falta a este programa para estar listo y funcional (usa perplexity concil para buscar idea que pudieran ser utiles , ideas elementos y estructuras que sean utiles )

### Current State
The app boots a modular monolith from `cmd/zapatos-erp/main.go` → `internal/app.Run()` → `internal/server.Run()`, with hardcoded users, in-memory sessions/audit, JSON-file repos for domain data, and one SSR HTML+JS screen. The biggest readiness gaps are: (1) `packaging` and `logistics` are wired to missing JSON files so those modules fail on first access, (2) auth/audit are non-persistent, (3) boot/config is hardcoded, (4) the UI only covers happy paths, and (5) CI/docs/observability are basically absent.

### Affected Areas
- `internal/app/app.go` — hardcoded bootstrap, fixed port, hardcoded users, and repo wiring to files that do not exist.
- `internal/store/jsonfile.go` — missing or malformed files return errors; this breaks `packaging` and `logistics` because `data/packaging.json` and `data/logistics.json` are absent.
- `internal/auth/auth.go`, `internal/server/server.go` — bearer-token sessions are memory-only; no expiry/logout/cookie flow.
- `internal/audit/audit.go` — audit trail is memory-only and disappears on restart.
- `internal/web/ui.go` — single inline page; missing delete flows, validation feedback, loading/error states.
- `data/` — no `packaging.json` or `logistics.json`; those modules are effectively dead until created.
- `openspec/config.yaml` / repo root — no CI, Dockerfile, README, or deployment/observability scaffolding.

### Approaches
1. **Viability unblocking pass** — create missing seed files, externalize config/port/users, persist audit/sessions, and add basic UI error/delete handling.
   - Pros: fixes the most visible broken paths fast; low risk; directly improves “functional.”
   - Cons: still leaves the app demo-grade; FoxPro remains a stub.
   - Effort: Medium

2. **Production hardening pass** — do the above plus real deployment/observability, stronger auth flow (cookie/session expiry), and a proper FoxPro integration contract.
   - Pros: closer to a shippable ERP; reduces restart/data-loss risk.
   - Cons: broader blast radius; needs more design and test work.
   - Effort: High

### Recommendation
Start with the viability unblocking pass. The app is not fully functional today because packaging/logistics cannot load, and the stateful subsystems disappear on restart. Fixing those first gives the highest impact per effort and creates a stable base for the remaining UI/ops work.

### Risks
- Missing-file handling in `JSONFileRepository` blocks whole modules instead of self-healing from empty data.
- In-memory sessions/audit make restarts destructive and undermine ERP credibility.
- UI permissions are duplicated in the frontend, so role drift can happen if backend permissions change.

### Ready for Proposal
Yes — propose a first change focused on unblocking the broken modules and persistence/config gaps; keep FoxPro/ops hardening as a follow-on change.
