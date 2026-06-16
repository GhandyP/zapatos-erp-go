# Fix Report — Concurrency & Data Integrity

**Date:** 2026-05-27
**Tool:** el Gentleman (surgical fix agent)

---

## Fix 1: JSONFileRepository — atomic writes + error surfacing

**File:** `internal/store/jsonfile.go`

| # | Lines | Change |
|---|-------|--------|
| 1a | 63–68 | `persistLocked()`: write to `r.path + ".tmp"` first via `os.WriteFile`, then `os.Rename(tmpPath, r.path)`. Prevents data corruption on crash during write. |
| 1b | 35, 44 | `ensureLoaded()`: on `os.ReadFile` failure → `r.err = fmt.Errorf("read file %s: %w", r.path, err)`. On `json.Unmarshal` failure → `r.err = fmt.Errorf("unmarshal %s: %w", r.path, err)`. Errors now surfaced to callers via `ensureLoaded()` return value instead of silently swallowed. |

---

## Fix 2: FoxPro Adapter — add mutex

**File:** `internal/integrations/foxpro/adapter.go`

| # | Lines | Change |
|---|-------|--------|
| 2a | 8–11 | Added `"sync"` import. |
| 2a | 33 | Added `mu sync.RWMutex` field to `Adapter` struct. |
| 2b | 46–58 | Refactored `buildStatus` from method to package-level function: `func buildStatus(svc BillingService, syncedInvoiceID map[string]struct{}, lastSyncAt *string, imported, updated, failed int)`. Parameter name `svc` avoids shadowing the `billing` package import. |
| 2b | 86–87 | `Sync()`: added `a.mu.Lock()` / `defer a.mu.Unlock()` at entry. Calls `buildStatus(...)` with explicit adapter fields. |
| 2c | 82–85 | `GetStatus()`: added `a.mu.RLock()` / `defer a.mu.RUnlock()` at entry. Calls `buildStatus(...)` with explicit adapter fields. |

The refactored `buildStatus` receives all data as parameters instead of reading adapter state, eliminating the deadlock risk that would arise with Go's non-reentrant mutexes.

---

## Fix 3: AdjustStock TOCTOU race

**File:** `internal/modules/finishedgoods/service.go`

| # | Lines | Change |
|---|-------|--------|
| 3 | 11 | Added `"sync"` import. |
| 3 | 34–35 | Added `mu sync.Mutex` field to `Service` struct. |
| 3 | 88–89 | `AdjustStock()`: added `s.mu.Lock()` / `defer s.mu.Unlock()` at entry. Serializes stock adjustments per service instance, preventing last-writer-wins on concurrent `Get`→compute→`Save` sequences. |

---

## Fix 4: Seed data — add dimensions, remove test artifact

**File:** `data/raw-materials.json`

| # | Change |
|---|--------|
| 4a | Added `dimensions` (`lengthCm: 100, widthCm: 50, heightCm: 2, weightKg: 3.5`) to all 3 raw material records (rm-100, rm-audit-test, rm-filter-test). |

**File:** `data/finished-goods.json`

| # | Change |
|---|--------|
| 4b | Added `dimensions` (`lengthCm: 30, widthCm: 10, heightCm: 12, weightKg: 0.8`) to all 4 finished good records (fg-loafer-negro-39, fg-filter-test, fg-oxford-negro-40, fg-oxford-cafe-41). |
| 4b | Removed entry with `"id": "!!!"` (test artifact). |

**File:** `data/invoices.json` — no changes needed.

---

## Build Verification

`go vet ./internal/store/... ./internal/integrations/foxpro/... ./internal/modules/finishedgoods/...` passes with no errors.

`go build ./...` fails only from a pre-existing missing dependency (`golang.org/x/crypto/bcrypt` in `internal/auth/auth.go`), unrelated to these changes.

---

**Skill Resolution:** `paths-injected` — Go Testing Patterns skill loaded from `/home/ghandy/.agents/skills/golang-testing/SKILL.md`.
