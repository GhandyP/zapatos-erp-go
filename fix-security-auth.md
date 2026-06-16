# Security & Auth Fixes — Report

## Build Status
- `go build ./...` — PASS (zero errors)
- `go vet ./...` — PASS (zero errors)
- `go mod tidy` — PASS (added `golang.org/x/crypto v0.52.0`)

---

## Fix 1: Remove X-Role header bypass
**File:** `internal/server/server.go`

| Change | Detail |
|--------|--------|
| `currentSession()` | Removed X-Role header fallback. Function now only accepts Bearer tokens from Authorization header. |
| `roleFor()` | **Removed entirely** — it was only needed for the X-Role bypass path. |
| `actorName()` | Simplified signature: `actorName(session, role)` → `actorName(session)`. The `role` parameter was redundant with `session.Role`. |
| 14 handler functions | Changed `role := roleFor(r, session)` → `role := session.Role` (lines 78, 115, 134, 171, 212, 249, 268, 305, 324, 361, 396, 442, 461, 470 of original). |
| 13 audit calls | Changed `actorName(session, role)` → `actorName(session)` (lines 107, 129, 163, 204, 241, 263, 297, 319, 353, 375, 420, 434, 456 of original). |

## Fix 2: bcrypt password hashing
**File:** `internal/auth/auth.go`

| Change | Detail |
|--------|--------|
| Import | Added `"golang.org/x/crypto/bcrypt"` |
| `UserAccount.Password` → `PasswordHash` | Struct field renamed. |
| `NewUserAccount()` | New constructor function that hashes plaintext with `bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)` and panics on hash failure. |
| `Login()` | Changed plaintext comparison to `bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) == nil`. |
| Unchanged | `GetBearerToken()`, `GetRoleFromHeader()`, `randomHex()` kept as-is. |

## Fix 3: Updated credentials in app.go
**File:** `internal/app/app.go`

| Change | Detail |
|--------|--------|
| 6 user accounts | Changed from literal structs `{Username:..., Password:..., Role:...}` to `auth.NewUserAccount(username, password, role)` constructor calls. |

## Fix 4: Unexport permissions map
**File:** `internal/core/permissions.go`

| Change | Detail |
|--------|--------|
| `PermissionsByRole` → `permissionsByRole` | Map unexported. Internal references in `Can()` and `RoleAllowed()` updated. |
| `GetPermissions(role string) []string` | New function that returns a defensive copy of the permissions slice, or nil if role not found. |

No external references to `PermissionsByRole` existed outside the `core` package (only `core.Can()` and `core.RoleAllowed()` were used externally).

## Fix 5: crypto/rand for billing token generation
**File:** `internal/modules/billing/service.go`

| Change | Detail |
|--------|--------|
| Import | Added `"crypto/rand"`. Kept `"time"` (still used in `createInvoiceID`). |
| `randomToken()` | Replaced `fmt.Sprintf("%x", time.Now().UnixNano())[:n]` (predictable timestamp) with `rand.Read(b)` from `crypto/rand`. Panics on read failure. |

---

## Files Changed
1. `internal/auth/auth.go` — bcrypt hashing, PasswordHash field, NewUserAccount constructor
2. `internal/server/server.go` — removed X-Role bypass, removed roleFor, simplified actorName
3. `internal/app/app.go` — NewUserAccount constructor calls
4. `internal/core/permissions.go` — unexported permissions map, GetPermissions accessor
5. `internal/modules/billing/service.go` — crypto/rand for randomToken

## Dependency Added
- `golang.org/x/crypto v0.52.0` (via `go mod tidy`)
