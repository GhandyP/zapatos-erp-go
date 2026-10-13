# Demo Run-Through

**Purpose:** a short, repeatable walkthrough of what the prototype actually does, starting from a known clean state. Use it for a portfolio demo or as the script for a factory conversation. Nothing here is validated with factory users yet.

## Reset to a clean state

```bash
PORT=3133 demo/reset.sh
```

The script writes a bootstrap config for the chosen port, copies the fictional dataset from `demo/data/` into a scratch root (`$HOME/.cache/zapatos-erp-demo` by default), builds the binary inside that root when needed, starts the app, and requires a real `200` from `/health` before reporting success.

It validates the root before deleting anything: top-level, dangerous, symlinked and repository paths are refused (including the repository's own `data/`), a symlinked `config/` or `data/` is refused, the binary must live inside the root, and nothing is removed while the demo port is still in use — either stop the instance or pass `FORCE=1` to stop it deliberately. Records carry a `(demo)` suffix where the domain has a name field; the demo invoice keeps a plain ID because invoices have no name, and the audit dataset starts empty.

## Walkthrough

Open `http://127.0.0.1:3133/app`.

| # | Action | Expected result |
|---|---|---|
| 1 | Sign in as `admin` / `admin123`. | The Spanish UI renders every module; the login panel documents the local accounts. |
| 2 | In **Fabricación**, press **Consultar saldo** with `rm-100`. | `Saldo disponible: 0 m2 (rm-100)`. |
| 3 | In **Abrir stock de material**, submit the prefilled form (100 `m2`). | No error; **Consultar saldo** now reports `100`. |
| 4 | In **Crear línea de materiales**, submit the prefilled form. | `bom-loafer-v1` appears in **Listas de materiales** with the Spanish column labels. |
| 5 | In **Crear orden de trabajo**, submit `wo-demo-001` with planned quantity `1`. | The order appears in **Órdenes de trabajo** as `draft`. |
| 6 | Release `wo-demo-001`, then complete it with good quantity `1`. | The order reads `completed`, and the material balance drops to `99` (planned `1` × BOM `1`). |
| 7 | Try to complete `wo-demo-001` again. | The section shows a visible error: `only released work orders can be completed`. Nothing changes. |
| 8 | Sign out, sign in as `production` / `production123`. | No **Abrir stock de material** or **Crear línea de materiales** form appears; the materials and work-order lists are readable. |
| 9 | Replace the prefilled order ID with `wo-prod-001`, planned quantity `2`, then release it and complete it with good quantity `2`. | The order reads `completed` and the material balance drops to `97`. |
| 10 | Sign back in as `admin` and open **Auditoría**. | The successful mutations above are listed with actor, action, and entity; **Exportar CSV** downloads the same events. Read-only actions such as the balance query, and rejected actions such as the repeated completion, write no audit event. |

Optional extras that also work: adding a raw material with its dimensions, adjusting finished-goods stock, and creating or issuing an invoice as `ventas`.

## What the walkthrough proves

- The manufacturing slice is transactional end to end: opening stock, BOM, order lifecycle, material consumption, and finished-stock increment.
- Errors are visible in the UI instead of failing silently, and rejected actions change nothing.
- Role gating is real on the server; the UI only hides what a role cannot do.
- Data survives a restart and can be restored from a backup (see [`backup-restore-runbook.md`](backup-restore-runbook.md)).

A Playwright browser script asserting these steps over both roles ran green (31 checks: balances `0 → 100 → 99 → 97`, order statuses, duplicate and unknown-order errors, the `goodQty` bound, and the absence of admin-only forms for production) against an instance reset from `demo/data`. That script is not committed to the repository yet.

## Known limitations to state out loud

- The manufacturing rules are provisional assumptions, not validated factory requirements: one material, one BOM line per variant, integer quantities, consumption based on the planned quantity, and no lots, scrap, or costing.
- Server error messages are English while the UI is Spanish; the app-wide convention.
- Security work is deferred; see [`security-backlog.md`](security-backlog.md). Do not expose the app to untrusted networks.
- Audit history keeps the newest 500 events.
