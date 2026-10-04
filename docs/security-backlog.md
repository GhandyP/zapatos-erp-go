# Security Backlog

> **DEFERRED / NOT IMPLEMENTED. Do not expose or deploy this application to untrusted networks until this backlog has been addressed and the resulting deployment has been reviewed.** Baseline HTTP protections listed below do not make the app production-safe. This document records future work; it is not a security sign-off or an implementation plan for the current roadmap task.

## Current boundary and evidence

| Surface | Current repository behavior |
|---|---|
| UI and login | `/` and `/app` serve the inline UI. The login page discloses the seeded account credentials. `POST /api/login` returns a bearer token and user payload. Do not copy the disclosed values into other documentation. |
| Auth status | `GET /api/me` requires a bearer token; `GET /api/boot` returns role names without authentication. `/health` and `/metrics` are also unauthenticated probes. |
| Transport and bind | The app serves plain HTTP. The documented default bind is `:3001` (wildcard interface); the documented default data directory is `data/`. No app TLS configuration is documented. |
| Browser and server sessions | The UI stores the bearer token in browser `localStorage`. Its logout action clears only that browser value; there is no server-side logout/revocation route. Session snapshots are raw server-side data; file creation requests mode `0644`, which the process umask may narrow (default path `data/sessions.json`). |
| Authorization | The server has a role-permission map and checks permissions for resource operations. Some handlers do not distinguish a missing/invalid session from an authenticated user lacking permission, so protected requests can return `403` instead of a consistent `401`. The UI hides the auditor's raw-material create form, but still renders write forms in other auditor-visible sections. |
| Configuration | `README.md` documents fallback defaults when bootstrap config is absent, while `openspec/specs/operations-security-baseline/spec.md` requires fail-fast handling for invalid/missing required runtime inputs or secrets. Resolve this contract; do not assume fallback defaults are safe for a network deployment. |
| Files and logs | JSON repositories and audit snapshots are created with requested mode `0644`, which the process umask may narrow; session snapshot permissions also need restriction. Request logs include method, path, status, duration, byte count, request ID, and trace correlation. 5xx response bodies are redacted. |
| Build/release | Current workflows run tests, vet, and builds; the release workflow publishes a binary and checksum. Dependency vulnerability gating, an SBOM, and artifact signing/provenance are not evidenced by those workflows. |

Relevant implementation/doc evidence includes `internal/web/ui.go`, `internal/server/server.go`, `internal/core/permissions.go`, `internal/store/jsonfile.go`, `internal/audit/audit.go`, `README.md`, and `.github/workflows/`. The `bcrypt` password-hash baseline is in `internal/auth/auth.go`; it does not address seeded credentials, session lifecycle, transport, or storage protections.

## Deferred backlog and acceptance criteria

All items below remain **deferred**. The exposure warning stays in force until the applicable items are complete and reviewed.

### 1. Remove reusable seed credentials and login disclosure

**Current concern:** first-run seed accounts are disclosed in the login UI and the README describes them as intended for local/controlled use.

**Accept when:**
- The login page and public responses disclose no usernames/passwords, password hints, or reusable default credentials.
- Network-capable deployments cannot start with known/shared seed passwords. First-run setup requires an operator-controlled, unique credential bootstrap and forces replacement before serving protected routes.
- Development-only seed behavior is explicit, isolated, and cannot silently become the deployment default; any currently disclosed credentials are invalidated/rotated.
- Tests verify the rendered UI, responses, logs, and startup path do not reveal or accept reusable defaults outside the explicitly isolated development mode.

### 2. Define the HTTP, bind, and TLS boundary

**Current concern:** the default `:3001` listener is not loopback-only, the app uses HTTP, and the login/token flow can cross that boundary without transport protection.

**Accept when:**
- A supported deployment topology is explicit: app-terminated TLS or a documented trusted TLS-terminating proxy, including which proxy headers are trusted and how the external HTTPS boundary is enforced.
- Without that secured topology, the app binds only to loopback or refuses an unsafe network bind; operators cannot mistake `:3001` for local-only.
- Credentials and bearer tokens are never accepted/sent over an untrusted plaintext connection. Tests or deployment smoke checks prove the selected boundary and fail closed for unsupported settings.
- The documented Docker port publication and proxy configuration match the chosen boundary; HTTPS-related headers are applied only when appropriate.

### 3. Replace browser token persistence and add server-side lifecycle controls

**Current concern:** the bearer token is stored in `localStorage`; browser logout only deletes that local value. Persistent server snapshots contain raw session data, and file creation requests mode `0644`, which the process umask may narrow.

**Accept when:**
- The browser no longer stores bearer credentials in script-readable `localStorage`; use a reviewed session design with appropriate cookie protections or an explicitly reviewed alternative.
- Sessions have a defined idle/absolute expiry and are rejected after expiry. A server-side logout/revocation operation invalidates the session, including across app restarts.
- Server storage does not persist reusable raw bearer tokens; snapshot permissions are restricted as described in item 7.
- Tests cover login, invalid/expired sessions, logout, revocation, restart behavior, and absence of session secrets in browser-readable storage, logs, and responses other than the one-time login response.

### 4. Make auth status, RBAC, and read-only UI consistent

**Current concern:** `GET /api/me` has a `401` path, but several protected handlers treat a missing session as an empty role and can answer `403`. The UI's auditor write controls are inconsistent across sections.

**Accept when:**
- Every protected endpoint consistently returns `401` for missing/invalid/expired authentication and `403` only for an authenticated identity lacking permission.
- Authorization is deny-by-default and enforced by the server for every read and mutation; hiding a button/form is never treated as authorization.
- The UI derives read/write affordances from the authenticated user's capabilities and hides/disables all mutation controls for read-only capabilities, including auditor-visible modules.
- A role/route matrix test exercises unauthenticated, read-only, and permitted users against every protected route, including negative mutation attempts.

### 5. Validate configuration and fail closed for deployment secrets

**Current concern:** the documented optional-config fallback conflicts with the existing operations-security OpenSpec requirement for invalid or missing required runtime inputs/secrets to stop startup.

**Accept when:**
- Configuration has a strict schema and rejects malformed, invalid, conflicting, or unsupported values with a startup error before listening.
- Deployment mode has no silent fallback to seed credentials or insecure secret material; secret sources and rotation are explicit and are not committed in bootstrap files.
- Any local-development defaults are explicitly gated and bound to a safe local-only mode; tests prove absent/malformed config cannot accidentally select deployment credentials or a wildcard plaintext listener.
- README, bootstrap spec, and runtime behavior agree on which values are required and which are safe local defaults.

### 6. Define and test sensitive-log redaction

**Current concern:** current request logs contain request metadata and 5xx bodies are generic, but there is no documented, tested end-to-end redaction contract for secrets across future logging paths.

**Accept when:**
- Authorization headers, cookies, passwords, bearer tokens, session identifiers, secret config, and sensitive request/response bodies never enter logs or client-facing errors.
- Login and failed-auth paths are specifically covered; path/query values are reviewed so secrets cannot be placed in logged URLs.
- Tests inject recognizable sentinel secrets and assert they are absent from captured logs, traces, metrics labels, and 5xx responses while correlation fields remain useful.

### 7. Restrict data, session, audit, and backup file access

**Current concern:** JSON/audit/session snapshots are raw files; file creation requests mode `0644`, which the process umask may narrow. That argument alone does not establish the permissions of pre-existing files. Default data lives under `data/`.

**Accept when:**
- The configured data directory and newly created business, audit, and session files use restrictive owner/group permissions appropriate to the documented runtime account; existing files are checked or corrected safely at startup.
- Temporary snapshots, exported files, and backup/restore artifacts receive equivalent protection; tests assert modes on supported platforms and report unsafe permissions rather than silently claiming success.
- Container volume ownership/permissions match the app's non-root runtime user and the documented deployment topology.
- Scope here is confidentiality and filesystem access control only. Record durability, corruption handling, transactional writes, backup consistency, and concurrency are owned by **Phase 2 — Audit and storage data integrity** in [`product-roadmap.md`](product-roadmap.md); do not duplicate that work here.

### 8. Add dependency and release supply-chain controls

**Current concern:** CI/release workflows validate tests, vet, and builds and publish a checksum, but do not evidence a dependency vulnerability gate or SBOM/signing policy.

**Accept when:**
- Go dependencies are checked automatically on change and release; findings have a documented severity threshold, remediation path, and time-bounded exception process.
- Release outputs include a machine-readable SBOM tied to the exact revision/artifact. If release policy requires it, artifacts/images are signed and provenance is generated and verifiable before distribution.
- Toolchain, container base images, and CI actions are reviewed/pinned under the agreed supply-chain policy; release checksums remain available but are not treated as signatures.

### 9. Security test and review plan

**Accept when:**
- Automated coverage includes the route/auth matrix, role denials, expiry/revocation/logout, seed credential non-disclosure, malformed config/fail-closed behavior, HTTP/TLS boundary, log/error redaction, and filesystem permissions.
- A release/deployment checklist confirms the reviewed bind/proxy/TLS setup, secrets/bootstrap handling, data-directory ownership, backup access controls, dependency findings, and approved artifact provenance.
- Test results and any explicit exceptions are recorded for the exact release artifact; an unresolved critical/high finding or failed boundary check blocks untrusted-network deployment.

## Existing baseline controls — present, but not a completion claim

These controls are evidenced in the current repository and should be retained. They do **not** resolve the deferred items above:

- Passwords are hashed with bcrypt (`internal/auth/auth.go`).
- Server-side role permission checks exist (`internal/core/permissions.go`, `internal/server/server.go`).
- The HTTP server configures read/header/write/idle timeouts and a 1 MiB request-body limit; panic recovery and baseline headers are present (`internal/server/server.go`).
- 5xx responses use a generic error body; request logs carry correlation/metadata fields (`internal/server/server.go`, `internal/observability/observability.go`).
- `/health` and `/metrics` are intentionally unauthenticated probes; `GET /api/boot` exposes role names only. These routes still need to be considered in a deployment's network boundary.

## OpenSpec references and non-goals

Reconcile this backlog with the existing requirements; the specs are not evidence that every requirement is implemented:

- [`operations-security-baseline`](../openspec/specs/operations-security-baseline/spec.md) — runtime configuration and sensitive-data handling.
- [`server-baseline-hardening`](../openspec/specs/server-baseline-hardening/spec.md) — HTTP limits, recovery, and headers.
- [`persistent-session-audit`](../openspec/specs/persistent-session-audit/spec.md) — restart-persistent sessions and audit events.
- [`bootstrap-config`](../openspec/specs/bootstrap-config/spec.md) — current fallback/default configuration contract to reconcile with fail-closed deployment requirements.

This document does not claim a security audit, compliance certification, or production readiness; it does not implement fixes. Product data-integrity and storage architecture are covered in [`product-roadmap.md`](product-roadmap.md), not duplicated here. No external research is used.
