# Proposal: First viability slice for Zapatos ERP

## Intent

Make the app reliably boot and support a real operator flow: load all active modules safely, remove hardcoded bootstrap assumptions, keep auth/audit/session behavior stable across restarts, and add baseline server hardening before any broader product work.

## Scope

### In Scope
- Add bootstrap config for bind address, data paths, and seed accounts.
- Add missing seed JSON for `packaging` and `logistics`, with safe fallback loading for malformed/missing files.
- Replace in-memory auth/audit/session state with file-backed persistence and add minimal HTTP hardening.

### Out of Scope
- CI, Docker, observability, and deployment automation.
- Full UI redesign or new business workflows.
- Migrating away from JSON file storage.

## Capabilities

### New Capabilities
- `bootstrap-config`: load app defaults from config instead of hardcoding address, seeds, and data paths.
- `module-seed-loading`: start all active modules even when seed files are empty, missing, or malformed.
- `persistent-session-audit`: keep sessions and audit events durable enough for restart-safe use.
- `server-baseline-hardening`: add timeouts, panic recovery, security headers, and request size limits.

### Modified Capabilities
- None.

## Approach

Introduce a small bootstrap layer in `internal/app` for config-driven wiring, keep JSON repositories but add the missing module seed files, and move auth/session/audit to file-backed stores with atomic writes. Wrap the HTTP handler with conservative middleware and explicit server timeouts.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `internal/app/app.go` | Modified | Bootstrap config and dependency wiring |
| `internal/auth/auth.go` | Modified | Durable session storage |
| `internal/audit/audit.go` | Modified | Durable audit storage |
| `internal/server/server.go` | Modified | Timeouts, middleware, safer request handling |
| `internal/store/jsonfile.go` | Modified | Startup fallback and atomic persistence |
| `data/packaging.json`, `data/logistics.json` | Added | First-run seed data |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| File-backed state corruption or locking issues | Medium | Atomic writes, small focused tests, keep JSON format simple |
| Hardening changes breaking current UI/API flows | Medium | Conservative defaults and handler-level tests |

## Rollback Plan

Revert the bootstrap layer, restore in-memory auth/audit/session stores, remove the middleware wrapper and timeout changes, and delete the new seed/config files. The app should return to the current hardcoded behavior.

## Dependencies

- None.

## Success Criteria

- [ ] App starts with missing data files and still exposes Packaging/Logistics.
- [ ] Login and audit events survive a process restart.
- [ ] `go test ./...` and `go build ./...` pass.
