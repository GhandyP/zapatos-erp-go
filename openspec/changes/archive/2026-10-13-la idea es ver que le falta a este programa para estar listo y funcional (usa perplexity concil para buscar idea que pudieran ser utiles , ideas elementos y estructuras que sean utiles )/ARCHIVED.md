# Archived: 2026-10-13

**Disposition:** archived without implementation as an active change.

**Why:** this change was drafted when the repository genuinely lacked the pieces it names — `data/packaging.json` and `data/logistics.json` were absent, bootstrap values were hardcoded, sessions and audit lived in memory, and CI, Docker, README and observability did not exist. All of that landed afterwards through the storage and readiness workstream:

- bootstrap configuration in `internal/app/config.go`
- seed files under `data/`, with packaging and logistics loading from them
- SQLite-backed durable sessions and audit (`internal/auth/sqlite.go`, `internal/audit/sqlite_store.go`)
- CI workflows, `Dockerfile`, `README.md` and `internal/observability/`
- the tracked workstream with decisions, checks and evidence in `odd/tasks/product-readiness-2-3-4.md`

The request that triggered this change was dropped, and keeping it active would report implemented work as missing. It is preserved here unchanged for history.

**Superseded by:** `docs/product-roadmap.md` for direction and remaining proposed work, and `odd/tasks/product-readiness-2-3-4.md` for the delivered work units and their verification.
