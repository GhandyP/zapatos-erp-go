# Judgment Day — Judge A (Adversarial Review)

**Project:** zapatos-erp-go
**Scope:** Full project — all .go files under cmd/, internal/, data/ JSON files
**Date:** 2026-05-27

---

## FINDINGS

### F-01 — CRITICAL: Unauthenticated role bypass via X-Role header

**File:** internal/server/server.go (lines ~270–280, `currentSession` function)

```go
role := auth.GetRoleFromHeader(r.Header.Get("X-Role"))
if role == "" {
    return "", auth.SessionUser{}, false
}
return "", auth.SessionUser{Role: role}, true
```

When no `Authorization` bearer token is present, the server falls back to accepting an `X-Role` header. Any unauthenticated HTTP client can send `X-Role: administrador` and bypass the login flow entirely, gaining full admin access (wildcard `*` permissions). `GetRoleFromHeader` only validates the role name against known roles — it does not authenticate the caller.

**Suggested fix:** Remove the `X-Role` header fallback from production code; use it only in tests or behind a build tag.

---

### F-02 — CRITICAL: Hardcoded plaintext credentials

**File:** internal/app/app.go (lines 25–30)

All user accounts have plaintext passwords (`admin123`, `warehouse123`, etc.) embedded directly in the source code. Even for a demo/internal tool, this is a credential leak risk if the binary or source is shared.

**Suggested fix:** Load credentials from environment variables or a config file; never commit real passwords.

---

### F-03 — CRITICAL: No password hashing — plaintext comparison

**File:** internal/auth/auth.go (line 43)

```go
if user.Username == username && user.Password == password {
```

Passwords are compared as raw plaintext. No bcrypt, argon2, or any hash function is used. Combined with F-02, any access to the source code or memory dump reveals all credentials.

**Suggested fix:** Store password hashes (bcrypt) and compare with `bcrypt.CompareHashAndPassword`.

---

### F-04 — CRITICAL: Foxpro adapter race condition — concurrent map read/write + deadlock

**File:** internal/integrations/foxpro/adapter.go

The `Adapter` struct has no synchronization. `syncedInvoiceID` is a plain `map[string]struct{}` accessed by both `Sync()` (write) and `buildStatus()` (read). In Go, concurrent map read+write is a fatal runtime crash (`concurrent map read and map write`).

Additionally, `Sync()` calls `a.buildStatus()` internally. If a mutex were added naively, `Sync()` holding the lock and calling `buildStatus()` which also tries to acquire the lock would deadlock (Go mutexes are not reentrant).

**Suggested fix:** Add a `sync.RWMutex`; refactor `buildStatus()` to accept parameters instead of reading adapter state directly, or extract a lock-free inner helper called by both.

---

### F-05 — WARNING (real): Non-atomic JSON file persistence — data corruption on crash

**File:** internal/store/jsonfile.go, `persistLocked()` method

```go
return os.WriteFile(r.path, data, 0o644)
```

`os.WriteFile` truncates then writes. If the process crashes or loses power mid-write, the file is left empty or partially written, losing all data. The correct pattern is write-to-temp + `os.Rename` (atomic on same filesystem).

**Suggested fix:** Write to `path + ".tmp"` then `os.Rename` to `path`.

---

### F-06 — WARNING (real): No session expiration

**File:** internal/auth/auth.go

Sessions are created in `Login()` and stored in a map forever. There is no TTL, no cleanup goroutine, no maximum session count. Over time this is an unbounded memory leak, and stolen tokens never expire.

**Suggested fix:** Add a TTL to sessions and a periodic cleanup, or use JWT with expiry.

---

### F-07 — WARNING (real): Seed data missing `dimensions` — breaks record updates

**File:** data/raw-materials.json, data/finished-goods.json

All pre-existing records (`rm-100`, `rm-audit-test`, `rm-filter-test`, `fg-loafer-negro-39`, `fg-oxford-negro-40`, etc.) lack the `dimensions` field. On JSON unmarshal, `Dimensions` defaults to zero-value (`{0, 0, 0, 0}`). Any attempt to upsert these records (e.g., editing from the UI) triggers `Dimensions.Validate()` → error `"dimensions require positive lengthCm, widthCm and heightCm"`. Users cannot update existing records without first adding dimensions, but there is no migration path or warning.

**Suggested fix:** Either add valid dimensions to seed data, or make dimensions optional in validation for existing records.

---

### F-08 — WARNING (real): Audit store silently drops events at 500-event cap

**File:** internal/audit/audit.go

```go
if len(s.events) > s.maxEvents {
    s.events = append([]Event(nil), s.events[len(s.events)-s.maxEvents:]...)
}
```

The `maxEvents` is hardcoded to 500 and not configurable. Events beyond the cap are silently discarded with no notification, log, or persistence. For an ERP audit trail, this means compliance-relevant records vanish without trace.

**Suggested fix:** Make `maxEvents` configurable; log a warning when events are dropped; consider persistent storage.

---

### F-09 — WARNING (real): Duplicate/unreachable FoxPro sync handler code

**File:** internal/server/server.go

The `/api/foxpro/sync` endpoint is registered as a standalone handler AND handled inside the `/api/foxpro` handler via `strings.HasSuffix(r.URL.Path, "/sync")`. Go's `ServeMux` matches the longer pattern first, so the suffix-check code inside the `/api/foxpro` handler is dead code. Similarly, the condition `r.Method == http.MethodPost && r.URL.Path == "/api/foxpro/sync"` inside the same handler is unreachable.

**Suggested fix:** Remove the suffix-check and exact-path-check blocks from the `/api/foxpro` handler; keep only the standalone `/api/foxpro/sync` handler.

---

### F-10 — WARNING (real): Weak pseudo-random invoice ID tokens

**File:** internal/modules/billing/service.go

```go
func randomToken(n int) string {
    return fmt.Sprintf("%x", time.Now().UnixNano())[:n]
}
```

This truncates a time-based hex string to `n` characters. Two calls within the same nanosecond produce identical tokens. The resulting invoice IDs (e.g., `inv-cli-1-20260410T043055Z-a3f2b`) have a predictable, guessable component. The truncation to 5 hex chars gives only ~1M possible values.

**Suggested fix:** Use `crypto/rand` for token generation (same as session tokens in `auth.go`).

---

### F-11 — WARNING (real): Unconstrained request body size — DoS vector

**File:** internal/server/server.go, `decodeJSON()` function

```go
func decodeJSON(body io.Reader, target any) error {
    dec := json.NewDecoder(body)
    return dec.Decode(target)
}
```

No `http.MaxBytesReader` or equivalent is applied to `r.Body`. An attacker can send a multi-gigabyte JSON payload to any POST endpoint, exhausting server memory.

**Suggested fix:** Wrap `r.Body` with `http.MaxBytesReader(w, r.Body, maxBytes)` before decoding.

---

### F-12 — WARNING (real): No HTTP method restrictions on several endpoints

**File:** internal/server/server.go

The following endpoints accept any HTTP method without returning 405 Method Not Allowed:
- `/api/me` — accepts POST, PUT, DELETE, etc.
- `/api/boot` — same
- `/api/audit` — same
- `/api/audit/export` — same
- `/` and `/app` — same

Only some endpoints (e.g., `/api/login`) explicitly check `r.Method`.

**Suggested fix:** Add method checks (`if r.Method != http.MethodGet { notFound(w); return }`) to all read-only endpoints.

---

### F-13 — WARNING (real): `PermissionsByRole` is an exported mutable global

**File:** internal/core/permissions.go

```go
var PermissionsByRole = map[string][]string{ ... }
```

The map is exported and mutable. Any package can reassign or modify role permissions at runtime. There are no tests for `Can()` or `RoleAllowed()`. There is no synchronization for concurrent access.

**Suggested fix:** Make the map unexported or provide only accessor functions; add unit tests for `Can()`.

---

### F-14 — WARNING (real): Dead code — unused `escape` function

**File:** internal/web/ui.go

```go
func escape(s string) string {
    return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(s, "&", "&amp;"), "<", "&lt;"), ">", "&gt;")
}
```

This function is defined but never called. HTML escaping in the UI is done client-side via JavaScript `escapeHtml()`. Dead code adds confusion and maintenance burden.

**Suggested fix:** Remove the unused `escape` function.

---

### F-15 — WARNING (theoretical): Potential Collisions in Adjustment IDs

**File:** internal/modules/finishedgoods/service.go

```go
ID: fmt.Sprintf("adj_%d", time.Now().UnixNano()),
```

If two stock adjustments occur within the same nanosecond (possible under high load or on fast hardware), the IDs collide, potentially corrupting the adjustment history.

**Suggested fix:** Use a UUID or combine a counter with the timestamp.

---

### F-16 — SUGGESTION: No test coverage for billing, auth, audit, foxpro

**Files:**
- internal/modules/billing/service.go — zero tests
- internal/auth/auth.go — zero tests
- internal/audit/audit.go — zero tests
- internal/integrations/foxpro/adapter.go — zero tests

These modules handle critical business logic (invoice lifecycle, authentication, audit trail, external integration) but have no test files at all. The server test (`server_test.go`) only exercises create/list flows; it does not test error paths, method restrictions, permission enforcement, or the foxpro sync.

**Suggested fix:** Add test files for each module with at minimum: happy path, validation errors, not-found, and boundary cases.

---

### F-17 — SUGGESTION: `produccion` role has asymmetric logistics permissions

**File:** internal/core/permissions.go

`produccion` has `logistics:read` but not `logistics:write`, while `almacen` has both. This may be intentional but is undocumented. If production needs to update storage slot assignments, this is a functional gap. If intentional, it should be documented.

---

### F-18 — SUGGESTION: CSV export ignores writer errors

**File:** internal/server/server.go (audit export handler)

```go
_ = writer.Write([]string{...})
```

All `csv.Writer.Write` errors are silently discarded. If the HTTP response writer fails (client disconnect, broken pipe), the server logs nothing.

**Suggested fix:** Check and log `writer.Error()` after `writer.Flush()`.

---

### F-19 — SUGGESTION: Normalize allocates empty slice on every List() call

**File:** internal/modules/finishedgoods/service.go, internal/modules/billing/service.go

Both `normalize()` functions convert `nil` slices to empty slices on every read. In `List()`, this runs for every item. For large datasets, this is wasteful allocation. The JSON encoder already handles nil slices as `null` vs `[]` — the frontend should handle both.

**Suggested fix:** Only normalize on write (Upsert/Save), not on read. Or accept nil as valid.

---

### F-20 — SUGGESTION: Missing test cases in dimensions_test.go

**File:** internal/domain/dimensions_test.go

The test table does not cover:
- All dimensions zero
- Negative length/width/height (only negative weight and zero length tested)
- Extremely large values
- Weight exactly zero (valid but boundary)

**Suggested fix:** Add table entries for these edge cases.

---

## VERDICT: NOT CLEAN — 4 CRITICAL, 8 WARNING (real), 1 WARNING (theoretical), 7 SUGGESTION

### Summary by severity

| Severity | Count | Key themes |
|---|---|---|
| CRITICAL | 4 | Auth bypass, plaintext creds, race/deadlock |
| WARNING (real) | 8 | Data corruption, weak crypto, missing constraints |
| WARNING (theoretical) | 1 | ID collision |
| SUGGESTION | 7 | Test coverage, dead code, minor improvements |

### Top 3 must-fix before any deployment

1. **F-01**: Remove `X-Role` header fallback — trivial to exploit, full admin bypass
2. **F-04**: Add synchronization to foxpro adapter — will crash under concurrent load
3. **F-05**: Atomic file writes — any power loss or OOM kill corrupts all data

---

Skill Resolution: paths-injected — loaded `/home/ghandy/.agents/skills/golang-testing/SKILL.md` as instructed.
