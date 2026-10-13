# PostgreSQL Migration Plan (Future Integration)

> **Status: plan only.** The application currently starts a SQLite runtime. This document neither implements nor schedules PostgreSQL, and no PostgreSQL connection should be attempted as part of this work. A production storage move remains subject to the storage/concurrency decision in [Product Roadmap](product-roadmap.md).

## 1. Status and scope

**Current.** The documented storage contract is SQLite on one host, on a local durable volume, with all processes sharing the data directory under the same UID. Cross-host and shared-network filesystems are unsupported. SQLite holds shared business records, audit events, and sessions; startup completes the JSON import before constructing the SQLite-backed adapters. (Evidence: `README.md:53-63`; `internal/app/app.go:31-41`; `internal/app/sqlite_runtime.go:48-117`.)

**Proposed.** Treat PostgreSQL as a later option if factory use requires multiple application hosts, reliable concurrent writes beyond the single-host SQLite operating model, or operational recovery/availability that the approved SQLite deployment cannot meet. First confirm those needs, data integrity expectations, and who will operate PostgreSQL. The roadmap requires a Phase 2 storage decision before a production migration; this plan is not that approval. (Evidence: `docs/product-roadmap.md:41-48,92`.)

**In scope:** the future integration shape, schema and cutover decisions, validation, and rollout gates. **Out of scope:** adding a driver, configuring credentials, connecting to any server, implementing migrations, or dual-writing. Security remediation remains a separate prerequisite in [Security Backlog](security-backlog.md); this plan is not a security sign-off.

## 2. Current seams and obstacles

| Area | Current | Proposed implication |
|---|---|---|
| Repository | `Repository[T]` exposes `List`, `FindByID`, `Save`, and `Delete`. `sqliteRepository` implements it over a collection name, a JSON payload, and the `(collection, id)` primary key. | Keep module services behind repositories, but add a PostgreSQL adapter and contract tests. This is a useful seam, not a drop-in driver swap. (`internal/store/repository.go:7-12`; `internal/store/sqlite.go:13-23,77-160`.) |
| Transaction scope | Mutations can be run through the backend-neutral `TransactionScope`; the SQLite scope constructs all module repositories and the audit writer over one `*sql.Tx`. | Preserve this atomic boundary for business changes plus audit writes. Provide a PostgreSQL scope using the same service contract. (`internal/server/transaction_scope.go:21-34`; `internal/server/sqlite_scope.go:20-60`.) |
| SQL and transaction APIs | Repository calls use SQLite SQL with `?` placeholders and a SQLite-named executor. `SQLiteUnitOfWork.Run` accepts `func(*sql.Tx)`, and repository methods have no context parameter. | Replace/adapt the SQLite executor and SQL dialect, use context-aware queries, and verify PostgreSQL transaction/error behavior. The high-level `TransactionScope` exists, but no engine-neutral low-level SQL executor/unit-of-work contract exists today. (`internal/store/sqlite.go:22-23,85-160`; `internal/store/unit_of_work.go:22-48`; `internal/server/transaction_scope.go:21-34`.) |
| Runtime and sessions/audit | `app.Run` selects `buildSQLiteRuntime`; it wires SQLite modules, sessions, audit, and transaction scope. The session and audit implementations directly query `store_records`. | Add an explicit backend/runtime factory and PostgreSQL session/audit adapters; do not imply the current auth or audit adapters are portable. (`internal/app/app.go:31-41`; `internal/app/sqlite_runtime.go:111-117`; `internal/auth/sqlite.go:27-68`; `internal/audit/sqlite_store.go:54-56,75-78`; `internal/audit/sqlite.go:28-50`.) |
| SQLite operations | `OpenSQLite` sets a 5-second busy timeout, one open connection, WAL mode, and the shared table. Startup secures the data directory and the backup parent (`0700`) and fixes owner-only (`0600`) modes for the database, lock, and sidecars; the import gate creates and verifies the backup directory and its files only while an import is pending, and skips that work once the marker is complete. | Retire—not translate—SQLite pragmas, WAL/sidecar handling, and filesystem database permissions for PostgreSQL. Define network/TLS, pool, credential, and database backup controls instead. (`README.md:58`; `internal/store/sqlite.go:30-67`; `internal/app/sqlite_runtime.go:57-110,133-237`; `internal/store/json_migration.go:223-227,292-305,329-374`; `internal/store/sqlite_import_gate.go:46-52`.) |
| Import coordination | A host-local lock file serializes the current JSON import. Unix-like targets use `flock`; other platforms fail closed. The completion marker is not a separate marker file: it is a row in SQLite's `store_migrations` table. | Use a database-side migration lock/ledger for a PostgreSQL migration job; a local file lock is not a multi-host coordinator. (`internal/store/migration_lock.go:10-43`; `internal/store/migration_lock_unix.go:1-62`; `internal/store/migration_lock_unsupported.go:1-13`; `internal/store/sqlite_import_gate.go:40-82`; `internal/store/json_import.go:12-16,109-123,157-169`.) |

**Concurrency gap to close.** Today the SQLite pool is limited to one connection per process. PostgreSQL pooling permits concurrent transactions, but the current manufacturing and stock workflows read then save without a database-level row-lock API. For example, material balance scans movements and work-order completion then inserts consumption; concurrent completions could both observe the same balance unless the new implementation serializes by material and enforces invariants in PostgreSQL. Add and test explicit locking/atomic update behavior before enabling concurrent PostgreSQL writers. (`internal/store/sqlite.go:47-52`; `internal/modules/finishedgoods/service.go:81-101`; `internal/modules/manufacturing/service.go:188-230,328-368`.)

## 3. Configuration and connection

**Current.** Bootstrap configuration contains `bindAddr`, `dataDir`, and `seedAccounts`; `app.Run` always builds SQLite. There is no database-backend setting in that bootstrap structure. The README describes `data/` as the default data directory; the Docker image declares `/data` as a volume and runs as its non-root `zapatos` user. (`internal/app/config.go:12-30`; `internal/app/app.go:31-41`; `README.md:20,37-45,53-58`; `Dockerfile:12-30`.)

**Proposed future PostgreSQL settings** (names/defaults are recommendations for a later implementation, not current environment variables):

| Setting | Proposed default / rule |
|---|---|
| `DATABASE_URL` | No default; required only when PostgreSQL mode is explicitly selected. Never log it. |
| `PG_TLS_MODE` | `verify-full`; reject plaintext/verification-disabled production connections. |
| `PG_TLS_ROOT_CERT` | Unset when system roots are appropriate; otherwise point to an operator-mounted CA file. Never bake the CA or private key into the image. |
| `PG_POOL_MAX_OPEN` / `PG_POOL_MAX_IDLE` | Initial staging values `10` / `5`; tune against the database connection budget and measured concurrency. |
| `PG_POOL_CONN_MAX_LIFETIME` / `PG_POOL_CONN_MAX_IDLE_TIME` | Initial values `30m` / `5m`; verify against infrastructure connection rotation. |
| `PG_CONNECT_TIMEOUT` / `PG_QUERY_TIMEOUT` | Initial values `5s` / `15s`; apply deadlines to connection, query, and transaction work and tune from staging measurements. |
| Migration execution | Production default is an explicit, single-run migration job; the application refuses an incompatible schema version and does not run DDL on every startup. No automatic-migration switch is proposed for production. |

Use the deployment secret manager or an equivalent protected injection mechanism for credentials, with separate least-privilege runtime and migration roles. Keep connection strings and TLS material out of bootstrap JSON, source control, command-line arguments, and logs; document rotation and revocation. The PostgreSQL driver and its TLS options must be selected and reviewed only after this plan is approved. Current transport is documented as plain HTTP with no app TLS configuration, so the separate TLS/security boundary must also be resolved. (`docs/security-backlog.md:3-4,11`.)

## 4. Schema and migrations

**Current.** Typed repositories store canonical collections in the shared `store_records(collection, id, payload)` table; `payload` is JSON, and the primary key is `(collection, id)`. The import gate also creates `store_migrations` for its completion marker. Canonical collection names are declared in `collections.go`. (`internal/store/sqlite.go:13-20,101-103,135-146`; `internal/store/json_import.go:12-16,109-123,157-169`; `internal/store/sqlite_import_probe.go:81-124`; `internal/store/collections.go:3-14`.)

**Proposed initial table map.** Preserve the current collection names as table names and string identifiers. Use typed columns for fields needed for constraints and query indexes; retain JSONB only where it avoids an unreviewed domain-model rewrite. Do not silently discard fields or change identifiers during conversion.

| Current collection → proposed table | Initial typed fields / constraints to carry forward |
|---|---|
| `raw_materials` → `raw_materials` | `id` primary key; integer `min_stock >= 0`; unit and dimensions. |
| `finished_goods` → `finished_goods` | `id` primary key; integer `stock >= 0`; style, size, color, dimensions, and adjustments. |
| `packaging` → `packaging` | `id` primary key; positive integer `max_units`; dimensions. |
| `logistics` → `logistics` | `id` primary key; positive integer `capacity_units`; dimensions. |
| `invoices` → `invoices` | `id` primary key; customer and status; non-negative total/tax. Confirm numeric precision before mapping current `float64` amounts to a database type. |
| `audit_events` → `audit_events` | `id` primary key; timestamp, actor, action, and entity columns. |
| `manufacturing_boms` → `manufacturing_boms` | `id` primary key; revision and positive integer `quantity_per_unit`; variant/material IDs and unit. |
| `manufacturing_work_orders` → `manufacturing_work_orders` | `id` primary key; BOM/revision, status, and integer planned/good quantities. |
| `material_movements` → `material_movements` | `id` primary key; material, kind, work-order ID, unit, timestamp, and positive integer quantity. |
| `sessions` → `sessions` | Do not copy live bearer tokens; see identifier and cutover notes below. Define a reviewed token-hash/session lifecycle before creating this table. |

These proposed columns carry forward current Go fields and service rules: raw materials (`internal/modules/rawmaterials/service.go:11-18,31-38`), finished goods (`internal/modules/finishedgoods/service.go:19-25,56-101`), packaging (`internal/modules/packaging/service.go:10-17,33-47`), logistics (`internal/modules/logistics/service.go:10-15,30-45`), invoices (`internal/modules/billing/service.go:23-31,57-68`), audit events (`internal/audit/audit.go:14-19`), and manufacturing (`internal/modules/manufacturing/service.go:24-62,86-94,143-181,188-230,313-368`). They are not claims that the current database already has these columns.

**Identifiers and invariants.** Most domain record IDs are caller- or service-assigned strings. Finished-goods IDs may be slugs; invoice IDs are generated strings. New audit IDs use `aud_` plus 32 hexadecimal characters, while legacy JSON audit imports receive `legacy-audit-%06d` IDs; preserve these existing IDs rather than regenerating them. Current SQLite session IDs are raw bearer tokens shaped as `sess_` plus 16 random bytes rendered as hex. (`internal/modules/finishedgoods/service.go:82-106,113-129`; `internal/modules/billing/service.go:49-62,103-116`; `internal/audit/sqlite.go:70-75`; `internal/app/sqlite_runtime.go:267-295,371-373`; `internal/auth/sqlite.go:36-47`; `internal/auth/auth.go:92-97`.)

The current service rules include integer quantities, positive BOM/opening/consumption quantities, non-negative minimum stock and finished stock, and rejection of material balances below zero. Add database constraints for equivalent persisted invariants, but retain service validation and reject invalid source rows before cutover. (`internal/modules/rawmaterials/service.go:11-18,31-38`; `internal/modules/finishedgoods/service.go:19-25,56-101`; `internal/modules/manufacturing/service.go:24-58,86-94,143-181,188-230,313-368`; see also [HTTP contract](http-contract.md#request-fields-and-validation).)

Normal SQLite audit writes retain at most the newest 500 events in the collection, ordered by event timestamp. The one-time JSON importer inserts prepared audit rows directly and does not apply that writer retention step, so 500 is not a guaranteed cap on every imported dataset. Confirm the cutover policy and preserve the current write-time cap unless the owner explicitly chooses otherwise. (`internal/audit/sqlite.go:14,28-48,85-145`; `internal/store/json_import.go:129-135`; `internal/app/sqlite_runtime.go:284-313`.)

**Indexes from current access patterns.** Primary keys on each table support current find/delete/upsert by ID and list ordering. Add `material_movements(material_id, kind)` for the balance calculation; consider a unique partial constraint for one opening movement per material to protect concurrent writes. Add `audit_events(at, id)` for chronological listing and retention. Current audit listing reads the collection and applies its case-insensitive substring filters in memory; at a 500-event cap, begin with a bounded scan rather than adding an unapproved search extension. If retention or volume grows, benchmark and then choose indexes/operators for the actual filter semantics. (`internal/store/sqlite.go:85-160`; `internal/modules/manufacturing/service.go:188-229`; `internal/audit/sqlite_store.go:65-116`; `internal/server/server.go:706-711,1063-1090`.)

Use ordered, versioned SQL migrations with a schema-version ledger containing version, checksum, and applied time. The external migration job should serialize through a PostgreSQL advisory lock, verify checksums, fail on unknown/partial state, and run migrations idempotently. Keep schema migrations distinct from the one-time data-cutover marker. Rehearse expand/backfill/constraint-tightening steps if future releases need zero/low downtime; none exist today.

## 5. Data cutover

**Current reference behavior.** The JSON-to-SQLite gate takes its host lock, probes SQLite read-only, strictly prepares and validates JSON, publishes a no-overwrite, checksum-verified backup of exact source bytes, then opens SQLite and imports. Import inserts and verifies collection/global counts, then commits the `complete` marker atomically with the rows. A completed marker skips preparation, so application startup ignores stale JSON; it does not delete or physically make those files unreadable. (`internal/store/sqlite_import_gate.go:40-82`; `internal/store/sqlite_import_probe.go:21-29,56-68,76-123`; `internal/store/json_migration.go:30-118,224-313`; `internal/store/json_import.go:49-168`; `README.md:56-58`.)

**Proposed SQLite-to-PostgreSQL analogue:**

1. **Freeze and inspect.** Stop writes to SQLite. Run a non-mutating source probe that checks SQLite schema, marker, row counts, JSON payload validity, and every collection/ID; reject an unexpected schema or unmarked/partial source. Record a deterministic source manifest. SQLite currently uses WAL, so use a database-consistent backup procedure (quiesce/checkpoint or a SQLite backup API); do not assume copying only the main database file captures committed state. (`internal/store/sqlite.go:50-53`; probe behavior: `internal/store/sqlite_import_probe.go:21-29`.)
2. **Validate and plan.** Strictly decode all records, reject duplicate IDs/unknown or invalid values, check quantity and stock invariants, resolve any representation conversion, and produce per-table counts and deterministic content digests. Do not mutate either system during this phase.
3. **Back up exactly.** Publish a protected, no-overwrite, checksummed copy of the consistent SQLite source plus migration manifest and restore instructions before writing PostgreSQL. Verify an existing backup byte-for-byte rather than overwriting it. Assign an operator and retention location before the cutover.
4. **Import once.** Use a single migration job protected by a PostgreSQL advisory lock and a unique cutover ID. For a bounded dataset, insert all rows and the complete marker in one transaction. If measured size requires batching, persist source digest and per-table/chunk progress; reject changed input on resume, compare existing rows instead of blindly upserting, and mark complete only after all chunks and checks finish.
5. **Verify and activate.** Compare source and target row counts by collection/table, unique IDs, constraints, per-table digests, and material/audit invariants. Invalidate all session tokens so users must sign in again; the current JSON import likewise backs up but does not import legacy `sessions.json`. Switch the application backend only after explicit go/no-go approval. Once active, PostgreSQL is authoritative and stale SQLite/JSON data is never an automatic fallback. (`internal/app/sqlite_runtime.go:238-321`; `internal/auth/sqlite.go:36-68`; `README.md:56-57`.)

Keep the original SQLite copy frozen and the PostgreSQL cutover marker/manifest together for the approved rollback window. Never run both backends as writers. Define the window's end before cutover; after PostgreSQL accepts writes, returning to SQLite requires a tested, write-frozen reverse export and reconciliation. Restoring the old frozen copy after PostgreSQL writes would lose those writes and is not an acceptable silent rollback.

## 6. Backups and rollback

**Proposed.** Assign backup ownership explicitly: a managed PostgreSQL service may own base backups and WAL/PITR, or the operator may run the corresponding tested process. In either case, retain encrypted logical backups (for example, `pg_dump`) for portability, database/WAL recovery for point-in-time restore, and the application schema version and cutover manifest. Define retention, access control, off-host storage, RPO/RTO, and restore authority.

A successful backup job is not a release gate by itself. Require a restore rehearsal into an isolated PostgreSQL instance, data-count/digest checks, app startup, login with fresh tokens, and read/write smoke tests before staging approval and every production cutover.

**Rollback without dual writes:** stop ingress and application writes first. Before PostgreSQL has accepted writes, a verified frozen SQLite backup may be reactivated. After PostgreSQL writes, preserve PostgreSQL, freeze it, and either run the rehearsed reverse-export/reconciliation path into SQLite or remain stopped while the data is reconciled; never silently start the old SQLite copy and lose acknowledged changes. No fallback to stale JSON is allowed. SQLite/JSON startup currently has no fallback after its import marker is complete (`README.md:57`); the PostgreSQL application must follow the same fail-closed principle.

## 7. Testing

**Proposed test plan.** Keep ordinary `go test ./...` offline: PostgreSQL integration tests must require an explicit build tag or opt-in environment variable, and only their dedicated CI job may start a pinned PostgreSQL container. The current runtime path selects SQLite; passing SQLite tests cannot establish PostgreSQL compatibility (`internal/app/app.go:31-41`).

- Run the same repository contract cases against SQLite and a real PostgreSQL container: CRUD, upsert, not-found, list ordering, duplicate IDs, JSON/field round-trips, and constraints.
- Test unit-of-work commit/rollback, business mutation plus audit atomicity, context cancellation, and transaction errors.
- Test concurrent stock adjustments, opening stock, work-order completions, duplicate requests, and session creation; assert no lost updates, duplicate movements, or negative balances.
- Test schema migration from empty/current/prior supported versions, checksums, lock contention, idempotent reruns, interrupted imports, changed-source refusal, progress resume, and exact row-count/digest verification.
- Exercise connection failures, TLS certificate/hostname rejection, pool exhaustion/timeouts, database restart, backup restore, and point-in-time recovery in the opt-in environment.

Synthetic tests cannot establish factory workflow fit, real production data quality, peak concurrency, acceptable outage windows, or operator readiness. Require factory-user reconciliation of representative data and a witnessed recovery drill before the pilot exit gate.

## 8. Rollout

| Phase | Work and exit gate |
|---|---|
| Development | Implement the backend/runtime seam, PostgreSQL repositories, transactions, migrations, constraints, and opt-in container tests. Exit only when SQLite behavior remains unchanged and PostgreSQL contract/concurrency tests pass. |
| Staging | Restore an authorized, protected copy of production data into PostgreSQL. Run the full probe/plan/import/verification, query and concurrency load, backup/restore rehearsal, and rollback drill. Record counts, digests, latency/error baselines, and elapsed cutover time. |
| Factory pilot | Use a small, named user group with an approved maintenance window and support owner. PostgreSQL is the sole writer. Reconcile stock, work orders, invoices, and audit events with factory users; collect issues and verify the operator runbook. |
| Cutover window | Freeze writes; take/verify the final SQLite backup; run one migration job; compare manifests/counts; deploy the PostgreSQL runtime; verify schema version, database readiness, login with fresh sessions, and representative reads/writes; monitor before reopening normal traffic. Keep a named rollback owner and window end. |

**Go/no-go:** no source validation errors; all per-table counts, keys, digests, and agreed business reconciliations match; restore and rollback rehearsals pass; TLS and pool settings are verified; the database-aware readiness check passes; errors and latency meet staging-approved limits; factory and operations owners sign off. Current `/health` returns a static application response and `/metrics` reports HTTP request metrics, so neither currently proves database readiness or PostgreSQL health. Add a database readiness signal and bounded pool/transaction/error metrics before relying on them for cutover. (`internal/server/server.go:153-164`; `docs/http-contract.md:15-20`.)

**Rollback triggers:** target/source mismatch, failed constraint or audit write, database readiness/connection failure, sustained error/latency or pool-wait breach, failed restore, or factory-critical workflow regression. Stop writes, preserve both databases and logs, and follow the rehearsed reverse-export rule in §6; do not enable dual writes as an emergency shortcut.

## 9. Open questions, assumptions, and non-goals

**Open questions to resolve before implementation:**

- Is multi-host access actually required, or can a supported single-host SQLite deployment meet the need?
- Who owns PostgreSQL operations, backups/PITR, encryption, monitoring, upgrades, restore authority, and on-call response?
- What operator skill and training are available for migrations, connection/pool incidents, and recovery?
- What RPO/RTO, maintenance window, rollback-window duration, data retention, and audit retention does the factory require?
- What are the authoritative invoice precision/currency rules, material units, and acceptable legacy-data conversions? Which tables/fields need full normalization after user review?

**Assumptions.** The current collection names, string IDs, and service-level invariants are migration inputs, not proof of a final factory data model. Factory validation may change that model. Security remediation is a separate prerequisite tracked in [Security Backlog](security-backlog.md); this document does not replace or complete it.

**Non-goals:** connecting to PostgreSQL now; selecting a vendor/hosting platform; scheduling production cutover; implementing schema/data migrations; changing product workflows or security controls; and dual writes or automatic fallback between databases.
