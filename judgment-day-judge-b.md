# Judgment Day — Judge B (Adversarial Review)

**Reviewer:** Judge B — adversarial code reviewer
**Target:** zapatos-erp-go — Go ERP for a shoe factory
**Date:** 2026-05-27

---

## CRITICAL

### C-01 — X-Role header bypasses authentication entirely
- **Severity:** CRITICAL
- **File:** internal/server/server.go (lines ~380–390, `currentSession`)
- **Description:** If the Bearer token is invalid or absent, `currentSession()` falls back to reading the `X-Role` header. If the header contains a known role (e.g., `administrador`), the request is authenticated with full privileges — no password, no token, no session. Any HTTP client can impersonate any role by setting `X-Role: administrador`. The test suite uses this exact mechanism (`req.Header.Set("X-Role", "administrador")`), confirming it works and is the only auth path tested.
- **Suggested fix:** Remove the `X-Role` fallback from production code; gate it behind a build tag or env flag for tests only.

### C-02 — Hardcoded plaintext credentials
- **Severity:** CRITICAL
- **File:** internal/app/app.go (lines ~18–23)
- **Description:** All user accounts have trivially guessable passwords hardcoded in source (`admin/admin123`, `warehouse/warehouse123`, etc.). These are committed to version control. Combined with C-01, the auth layer provides zero security.
- **Suggested fix:** Load credentials from environment variables or a secrets manager; hash passwords at rest with bcrypt.

### C-03 — No password hashing — plaintext comparison
- **Severity:** CRITICAL
- **File:** internal/auth/auth.go (line ~37, `Login`)
- **Description:** Passwords are compared with `==` in plaintext. Even if credentials were loaded from config, a database dump or memory inspection reveals all passwords. Industry standard is bcrypt/argon2 with constant-time comparison.
- **Suggested fix:** Hash passwords on account creation; compare with `bcrypt.CompareHashAndPassword` in Login.

### C-04 — Session token fallback is deterministic on crypto/rand failure
- **Severity:** CRITICAL
- **File:** internal/auth/auth.go (lines ~55–61, `randomHex`)
- **Description:** If `crypto/rand.Read` fails, the fallback returns the hex encoding of the literal string `"fallback"` (the nested `ReplaceAll`/`TrimSpace` calls are no-ops on a string with no spaces). Every session created under this failure mode gets the same token `"66616c6c6261636b"`. An attacker who triggers or waits for a `rand.Read` failure can hijack any session.
- **Suggested fix:** If `rand.Read` fails, return an error or terminate; never fall back to a deterministic value.

### C-05 — JSONFileRepository updates in-memory state before successful persist
- **Severity:** CRITICAL
- **File:** internal/store/jsonfile.go (lines ~75–82, `Save`)
- **Description:** `Save()` writes to the in-memory map first, then calls `persistLocked()`. If the file write fails (disk full, permission error), the in-memory state has the new value but the file has the old value. All subsequent reads return the uncommitted data. On server restart, the old data loads — silently losing the "saved" record with no error surfaced to future readers.
- **Suggested fix:** Attempt persist first (write to temp file), then update in-memory state only on success.

---

## WARNING (real)

### W-01 — No request body size limit — memory exhaustion via large JSON
- **Severity:** WARNING (real)
- **File:** internal/server/server.go (lines ~425–429, `decodeJSON`)
- **Description:** `decodeJSON` reads `r.Body` without `http.MaxBytesReader`. An attacker can POST a multi-gigabyte JSON body to exhaust server memory. Every POST endpoint (raw-materials, finished-goods, packaging, logistics, invoices) is affected.
- **Suggested fix:** Wrap body with `http.MaxBytesReader(w, r.Body, 1<<20)` before decoding.

### W-02 — FoxPro Adapter has no concurrency protection — data race
- **Severity:** WARNING (real)
- **File:** internal/integrations/foxpro/adapter.go (entire struct)
- **Description:** `Adapter` holds mutable state (`syncedInvoiceID` map, `lastSyncAt`, `lastImported`, `lastUpdated`, `lastFailed`) with no mutex. Concurrent `/api/foxpro/sync` requests will race on map writes and field updates. With Go's race detector enabled, this will panic.
- **Suggested fix:** Add `sync.Mutex` to Adapter; lock in `Sync()` and `GetStatus()`.

### W-03 — AdjustStock is a classic TOCTOU race — stock corruption
- **Severity:** WARNING (real)
- **File:** internal/modules/finishedgoods/service.go (lines ~88–103, `AdjustStock`)
- **Description:** `AdjustStock` reads the current stock via `Get()`, computes the new value, then writes via `Save()`. Two concurrent adjustments (e.g., -5 and -3 on stock=10) can both read 10, compute 5 and 7 independently, and the last writer wins — final stock is 7 instead of the correct 2. This is a textbook read-modify-write race.
- **Suggested fix:** Either use a compare-and-swap pattern in the repository, or serialize adjustments per entity ID with a per-key lock.

### W-04 — Session tokens never expire — memory leak and stale auth
- **Severity:** WARNING (real)
- **File:** internal/auth/auth.go (`SessionStore`)
- **Description:** `SessionStore.sessions` grows forever. There is no TTL, no cleanup goroutine, no max-session cap. A long-running server accumulates unbounded session entries. Stolen tokens remain valid indefinitely.
- **Suggested fix:** Add a `createdAt` timestamp to each session; run a periodic cleanup or check expiry on `Get()`.

### W-05 — JSON file persistence is not atomic — corruption on crash
- **Severity:** WARNING (real)
- **File:** internal/store/jsonfile.go (lines ~66–74, `persistLocked`)
- **Description:** `persistLocked()` writes directly to the target file with `os.WriteFile`. If the process is killed mid-write (SIGKILL, OOM, power loss), the file will be partially written and corrupt. On next startup, `json.Unmarshal` will fail, and the seed data replaces all production data — a total data loss.
- **Suggested fix:** Write to a temporary file in the same directory, then `os.Rename` atomically over the target.

### W-06 — `ensureLoaded()` silently swallows all load errors
- **Severity:** WARNING (real)
- **File:** internal/store/jsonfile.go (lines ~30–48, `ensureLoaded`)
- **Description:** If `os.ReadFile` or `json.Unmarshal` fails, the error is never stored in `r.err` (which remains nil). Seed data is silently loaded instead. The caller has no way to know the production data was unreadable. Combined with W-05, a corrupt file silently resets to empty seed data.
- **Suggested fix:** Store the error in `r.err` during the `sync.Once` callback so all subsequent calls surface it.

### W-07 — `billing.randomToken` uses timestamp, not crypto — predictable IDs and collision risk
- **Severity:** WARNING (real)
- **File:** internal/modules/billing/service.go (lines ~110–112, `randomToken`)
- **Description:** `randomToken` is `fmt.Sprintf("%x", time.Now().UnixNano())[:n]`. Two invoices created in the same nanosecond get the same token, producing duplicate IDs. The value is also predictable — an attacker can enumerate invoice IDs.
- **Suggested fix:** Use `crypto/rand` for the random portion of invoice IDs.

### W-08 — Zero-valued dimensions in existing JSON data will block updates
- **Severity:** WARNING (real)
- **File:** data/raw-materials.json, data/finished-goods.json
- **Description:** Existing records (`rm-100`, `rm-audit-test`, `rm-filter-test`, `fg-filter-test`, `fg-oxford-negro-40`, etc.) have no `dimensions` field. They deserialize with `LengthCM=0, WidthCM=0, HeightCM=0`. The `Upsert` methods now require `dimensions.Validate()` which rejects zero-length dimensions. Any attempt to re-save these records (even a no-op update) will fail with a validation error. These records are effectively frozen.
- **Suggested fix:** Run a data migration to add valid dimensions to all existing records, or make dimensions optional for pre-existing records.

### W-09 — No test coverage for billing module — critical business logic untested
- **Severity:** WARNING (real)
- **File:** internal/modules/billing/ (no service_test.go)
- **Description:** The billing module handles invoice creation, status transitions (draft→issued), and validation. There are zero tests. The `Issue()` method has logic that only allows draft→issued transitions, which is untested. The `normalize()` function silently resets invalid statuses to draft — also untested.
- **Suggested fix:** Add table-driven tests for Upsert, Issue, Get, and all status transitions including invalid ones.

### W-10 — No test coverage for actual Bearer token authentication
- **Severity:** WARNING (real)
- **File:** internal/server/server_test.go
- **Description:** Every test uses `X-Role` header bypass. Zero tests exercise the login → token → authenticated request flow. The Bearer token path, session lookup, and unauthorized rejection are completely untested.
- **Suggested fix:** Add tests that POST to `/api/login`, capture the token, and use it in subsequent requests; test expired/invalid tokens.

### W-11 — Duplicate dead code in foxpro route handler
- **Severity:** WARNING (real)
- **File:** internal/server/server.go (lines ~300–320, `/api/foxpro` handler)
- **Description:** The `/api/foxpro` handler contains two dead branches:
  1. `if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/sync")` — always false because Go's ServeMux routes `/api/foxpro/sync` to the separate registered handler; the path at this handler is always exactly `/api/foxpro`.
  2. `if r.Method == http.MethodPost && r.URL.Path == "/api/foxpro/sync"` — same reason; dead code.
  
  These branches are unreachable, creating false confidence that sync is handled here. The actual sync handler is registered separately at `/api/foxpro/sync`.
- **Suggested fix:** Remove the dead branches from the `/api/foxpro` handler; keep only the GET logic.

### W-12 — finished-goods.json contains invalid test data entry
- **Severity:** WARNING (real)
- **File:** data/finished-goods.json
- **Description:** The entry `{"id": "!!!", "style": "!!!", "size": "!!!", "color": "!!!", "stock": 1}` is clearly test/dummy data. Its slugified ID would be empty if regenerated, but since the ID is pre-set to `"!!!"`, it persists as-is. This entry pollutes the product catalog and could confuse users or downstream integrations.
- **Suggested fix:** Remove the `!!!` entry from the seed data.

### W-13 — `filterAuditEvents` silently ignores malformed event timestamps
- **Severity:** WARNING (real)
- **File:** internal/server/server.go (lines ~360–370, `filterAuditEvents`)
- **Description:** `eventTime, _ := time.Parse(time.RFC3339Nano, event.At)` discards the parse error. If an event has a malformed timestamp, `eventTime` is the zero time. The `from`/`to` filter logic then treats it as year 0001, which is before any realistic `from` filter, so the event is silently excluded. Users may not realize events are missing.
- **Suggested fix:** Log parse errors or include events with unparseable timestamps with a warning.

---

## WARNING (theoretical)

### W-T-01 — CSV injection in audit export
- **Severity:** WARNING (theoretical)
- **File:** internal/server/server.go (audit export handler)
- **Description:** The CSV export writes `event.Actor`, `event.Action`, `event.Entity` directly. If these fields contained values starting with `=`, `+`, `-`, or `@` (e.g., a username `=CMD("calc")`), spreadsheet applications could interpret them as formulas. Currently all actors come from hardcoded user accounts, so this is not exploitable today. But if user accounts become configurable or actor names come from external input, this becomes real.
- **Suggested fix:** Prefix potentially formula-interpreted values with a single quote or validate against formula-starting characters.

### W-T-02 — Dimensions.Validate allows WeightKG=0 for physical items
- **Severity:** WARNING (theoretical)
- **File:** internal/domain/dimensions.go
- **Description:** A shoe, raw material, or packaging box with weight 0 kg is physically impossible. The validation only rejects negative weights. This could lead to data quality issues where users forget to enter weight. However, this may be intentional if weight is optional for certain item types.
- **Suggested fix:** Consider making `WeightKG > 0` required, or add an explicit `omitempty`-style policy.

---

## SUGGESTION

### S-01 — Dead Go function `escape` in web/ui.go
- **Severity:** SUGGESTION
- **File:** internal/web/ui.go (line ~end, `escape` function)
- **Description:** The Go function `escape(s string) string` is never called. The HTML/JS template uses a JavaScript `escapeHtml` function instead. This is dead code.
- **Suggested fix:** Remove the unused `escape` function.

### S-02 — No CORS headers on API responses
- **Severity:** SUGGESTION
- **File:** internal/server/server.go
- **Description:** The API doesn't set CORS headers. If the frontend is ever served from a different origin (e.g., a separate dev server during frontend development), all API calls will be blocked by the browser. Currently fine since UI and API are same-origin.
- **Suggested fix:** Add a CORS middleware or explicit headers, at least for development.

### S-03 — `/api/health` accepts any HTTP method
- **Severity:** SUGGESTION
- **File:** internal/server/server.go (health handler)
- **Description:** The health endpoint doesn't check `r.Method`. POST, DELETE, PUT all return 200. While not a security issue, it violates REST conventions and could confuse monitoring tools.
- **Suggested fix:** Restrict to GET only.

### S-04 — No `Content-Type` validation on POST endpoints
- **Severity:** SUGGESTION
- **File:** internal/server/server.go (all POST handlers)
- **Description:** POST endpoints accept requests with any Content-Type. While `json.NewDecoder` will fail on non-JSON bodies, validating `Content-Type: application/json` upfront gives better error messages and follows HTTP semantics.
- **Suggested fix:** Check `r.Header.Get("Content-Type")` before decoding.

### S-05 — `notFound` used for method-not-allowed
- **Severity:** SUGGESTION
- **File:** internal/server/server.go (multiple handlers)
- **Description:** Several handlers return 404 when the method is wrong (e.g., `if r.Method != http.MethodDelete { notFound(w); return }`). HTTP 405 Method Not Allowed is the correct status. This makes API debugging harder for clients.
- **Suggested fix:** Return 405 with an `Allow` header for method mismatches.

### S-06 — `server.Run` prints to stdout via goroutine but has no graceful shutdown
- **Severity:** SUGGESTION
- **File:** internal/server/server.go (lines ~30–36, `Run`)
- **Description:** `Run` launches a goroutine that prints a message and calls `ListenAndServe`. There's no `context.Context` or `Shutdown()` support. The server can't be gracefully stopped — on SIGTERM, in-flight requests are killed. The goroutine for the print message is also unnecessary.
- **Suggested fix:** Accept a context, use `server.Shutdown(ctx)` on signal, remove the print goroutine.

### S-07 — `buildModules` doesn't check for missing data directory
- **Severity:** SUGGESTION
- **File:** internal/app/app.go (lines ~35–50, `buildModules`)
- **Description:** `buildModules` joins the working directory with `"data"` but never verifies the directory exists. If the binary is run from a different directory, all repositories will silently load seed data (empty) instead of real data. No error or warning is surfaced.
- **Suggested fix:** Check `os.Stat(dataDir)` and return a clear error if the directory is missing.

---

## Summary

| Severity | Count |
|----------|-------|
| CRITICAL | 5 |
| WARNING (real) | 13 |
| WARNING (theoretical) | 2 |
| SUGGESTION | 7 |

**Top 3 most impactful issues:**
1. **C-01** — X-Role header bypasses all authentication. Any HTTP client can be admin.
2. **C-05 + W-05 + W-06** — JSON persistence has three compounding bugs: non-atomic writes, in-memory-first mutation, and silent error swallowing. Together they can cause total data loss.
3. **W-03** — AdjustStock TOCTOU race will corrupt inventory under concurrent use.

---

Skill Resolution: paths-injected — loaded `/home/ghandy/.agents/skills/golang-testing/SKILL.md` before review.
