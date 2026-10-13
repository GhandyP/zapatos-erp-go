# Backup and Restore Runbook (SQLite)

**Status:** current operating procedure for the storage described in the [README](../README.md). It covers the SQLite deployment only; the future PostgreSQL path is a separate plan in [`postgresql-migration-plan.md`](postgresql-migration-plan.md).

This runbook was exercised end to end on the isolated test instance described under [Rehearsal evidence](#rehearsal-evidence); the commands below are the ones that were run.

## What is protected

| Path under the data directory | Contents | Notes |
| --- | --- | --- |
| `zapatos.sqlite` | All business records, audit events, and sessions. | Owner-only (`0600`). SQLite runs in WAL mode. |
| `zapatos.sqlite-wal`, `zapatos.sqlite-shm` | Write-ahead log and shared-memory index. | Removed by a clean shutdown; present while the app runs or after an unclean stop. |
| `migration.lock` | Host-local lock that serializes the one-time JSON import. | Owner-only (`0600`); safe to copy, never restore it from another host while that host runs. |
| `migration-backups/legacy-json-v1/` | Exact pre-import bytes of the legacy business JSON plus `manifest.json`. | Owner-only directory (`0700`) and files (`0600`). |
| Legacy `*.json` files | Stale copies left in place after the import. | Ignored once the database carries the completed `legacy-json-v1` marker. Never treat them as live data. |

Nothing in this directory is encrypted by the application, and there is no built-in scheduling: backups are an operator responsibility.

## Backup

1. **Stop the application cleanly** (SIGTERM to the process, not SIGKILL) and confirm it exited. A clean close checkpoints and removes the WAL sidecars, so `zapatos.sqlite` alone is consistent.
2. **If the process cannot be stopped cleanly**, archive the whole directory including `zapatos.sqlite-wal` and `zapatos.sqlite-shm`, or run `VACUUM INTO '<target>'` from a SQLite build against the live database. Copying only `zapatos.sqlite` from a running instance can lose committed transactions.
3. **Archive the whole data directory**, preserving permissions, and move the copy off the host:

   ```bash
   tar czf "data-$(date +%Y%m%d-%H%M%S).tar.gz" -C <root> data
   ```

4. **Keep the archive and the `migration-backups` directory together.** The legacy bytes and manifest are the only copy of the pre-import JSON once the original files are overwritten or deleted.

## Restore

1. **Stop every process that uses the directory.** Several instances may share it during normal operation under the same UID (WAL plus the connection busy timeout), but a restore is different: a running process keeps its handle on the replaced file and would keep writing to the unlinked inode while ignoring the restored data.
2. **Move the current directory aside rather than deleting it** (`mv data data.broken-<timestamp>`) so a failed restore can be investigated.
3. **Extract the archive** so the directory structure and permissions come back:

   ```bash
   tar xzf data-<timestamp>.tar.gz -C <root>
   ```

4. **Verify permissions** before starting: the data directory and `migration-backups` are `0700`, and the database, lock, and backup files are `0600`. Startup forces owner-only modes every time — it recreates and chmods the data directory and the `migration-backups` parent to `0700`, and re-applies `0600` to the database, its WAL/SHM sidecars, and the migration lock (`internal/app/sqlite_runtime.go:57-131`). It does not touch the legacy `*.json` files, so check those separately when they are part of the archive. Unsupported platforms fail closed before any directory is created.
5. **Start the application and verify**:

   - `GET /health` returns `{"app":"Zapatos ERP","ok":true}`.
   - Signing in works; a restored database carries its own session rows, so tokens issued before the backup remain valid until they are cleared.
   - Business counts match the backup: work orders with their statuses, material balance, finished-goods stock, and the audit list.
   - If the restored database carries the completed `legacy-json-v1` marker, startup ignores the legacy JSON in the same directory. There is no fallback to those files if the database is unreadable.

6. **A restored database is a point-in-time copy.** Everything written after the archived copy is lost. Reconcile with any paper or downstream record before reopening the instance to users.

## Rehearsal evidence

Exercised on the isolated instance at `/home/ghandy/.cache/erp-e2e` (bind `:3111`, demo data). The rehearsal script lives outside the repository for now, like the browser check script; the commands it ran are the ones in this runbook, and the table below records what each step produced.

| Step | Observed result |
| --- | --- |
| Start and record state | Work orders `wo-demo-001 completed (1/1)`, `wo-demo-002 released (1/0)`, `wo-prod-001 completed (2/2)`; material balance `97`; 11 audit events; finished stock per variant. |
| Clean shutdown | Process gone, **0** WAL/SHM sidecars left, `zapatos.sqlite` `0600`, `migration.lock` `0600`, `migration-backups` `0700`. |
| Backup | `tar czf backups/data-20261013-103321.tar.gz -C root data` → 8.0K (4.7K compressed). |
| Data loss | `rm -rf root/data` → directory gone. |
| Restore | `tar xzf <archive> -C root` → all files back with their original modes. |
| Restart | Work orders, material balance, finished stock **identical** to the pre-backup snapshot; audit count 11 → 11. |

## Limits and non-goals

- No off-host copy, rotation, encryption, or retention policy is automated here.
- No point-in-time recovery: the granularity is the archived copy.
- Audit history is capped at the newest 500 events by the writer, so old events may already be gone before a backup runs.
- Restoring an older copy re-introduces its sessions and rolls back every later change.
- This runbook does not cover the PostgreSQL target, which needs `pg_dump`/PITR and the rehearsed rollback described in [`postgresql-migration-plan.md`](postgresql-migration-plan.md).
