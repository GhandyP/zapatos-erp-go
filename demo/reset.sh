#!/usr/bin/env bash
# Reset the fictional demo dataset so a run-through always starts from the same state.
#
# Usage:   demo/reset.sh [root-dir]
# Default: $DEMO_ROOT or $HOME/.cache/zapatos-erp-demo
#
# Environment:
#   PORT   demo port, default 3001
#   START  start the app after resetting, default 1
#   BIN    application binary, default <root>/zapatos-erp (built if missing)
#   FORCE  1 to stop an instance already listening on PORT, default 0
#
# The script only touches the root directory it is given. It never removes the
# repository data directory.
set -euo pipefail

REPO_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
ROOT=${1:-${DEMO_ROOT:-$HOME/.cache/zapatos-erp-demo}}
PORT=${PORT:-3001}
START=${START:-1}
FORCE=${FORCE:-0}

listener_pid() {
  ss -ltnp 2>/dev/null | awk -v p=":$PORT" '$0 ~ p {print $NF}' | sed -n 's/.*pid=\([0-9]*\).*/\1/p' | head -1
}

PID=$(listener_pid || true)
if [ -n "$PID" ]; then
  if [ "$FORCE" = 1 ]; then
    echo "stopping instance on port $PORT (pid $PID)"
    kill "$PID" 2>/dev/null || true
    for _ in $(seq 1 40); do kill -0 "$PID" 2>/dev/null || break; sleep 0.25; done
  else
    echo "ERROR: something is already listening on port $PORT (pid $PID)." >&2
    echo "Stop it first, or re-run with FORCE=1 to stop this process." >&2
    exit 1
  fi
fi

mkdir -p "$ROOT/config" "$ROOT/data"
printf '{"bindAddr":":%s"}' "$PORT" > "$ROOT/config/bootstrap.json"

# Clear previous demo state: legacy JSON, the database and its sidecars, and the
# migration backups the first run creates.
rm -f "$ROOT"/data/*.json "$ROOT"/data/zapatos.sqlite "$ROOT"/data/zapatos.sqlite-wal "$ROOT"/data/zapatos.sqlite-shm "$ROOT"/data/migration.lock
rm -rf "$ROOT/data/migration-backups"
cp "$REPO_DIR"/demo/data/*.json "$ROOT/data/"
echo "demo data copied to $ROOT/data"

if [ "$START" != 1 ]; then
  echo "START=$START: data ready, app not started"
  exit 0
fi

BIN=${BIN:-$ROOT/zapatos-erp}
if [ ! -x "$BIN" ]; then
  echo "building $BIN"
  (cd "$REPO_DIR" && CGO_ENABLED=0 go build -o "$BIN" ./cmd/zapatos-erp)
fi

(cd "$ROOT" && setsid nohup "$BIN" > "$ROOT/server.log" 2>&1 < /dev/null &)
for _ in $(seq 1 60); do
  if curl -sf "http://127.0.0.1:$PORT/health" >/dev/null 2>&1; then break; fi
  sleep 0.5
done
curl -s "http://127.0.0.1:$PORT/health"; echo
echo "demo instance running: http://127.0.0.1:$PORT/app (root: $ROOT)"
echo "stop it with: kill \$(ss -ltnp | awk '/:$PORT/ {print \$NF}' | sed -n 's/.*pid=\\([0-9]*\\).*/\\1/p' | head -1)"
