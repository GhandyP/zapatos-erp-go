#!/usr/bin/env bash
# Reset the fictional demo dataset so a run-through always starts from the same state.
#
# Usage:   demo/reset.sh [root-dir]
# Default: $DEMO_ROOT or $HOME/.cache/zapatos-erp-demo
#
# Environment:
#   PORT             demo port, default 3001
#   START            start the app after resetting, default 1 (any other value prepares data only)
#   BIN              application binary; must live inside the demo root, default <root>/zapatos-erp
#   FORCE            1 stops the process listening on PORT, default 0
#   SKIP_PORT_CHECK  1 skips listener detection when `ss` is unavailable, default 0
#
# The script validates and confines the root before deleting anything: it refuses
# top-level, dangerous, symlinked or repository paths, refuses to write through a
# symlinked config/data directory, and refuses to remove anything when the demo
# port is still in use.
set -euo pipefail

REPO_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
REPO_REAL=$(cd "$REPO_DIR" && pwd -P)
PORT=${PORT:-3001}
START=${START:-1}
FORCE=${FORCE:-0}
SKIP_PORT_CHECK=${SKIP_PORT_CHECK:-0}

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

case "$PORT" in
  ''|*[!0-9]*) fail "PORT must be a number, got '$PORT'" ;;
esac

# --- root validation: never operate outside the validated root ---------------
RAW_ROOT=${1:-${DEMO_ROOT:-$HOME/.cache/zapatos-erp-demo}}
if [ -z "$RAW_ROOT" ]; then
  fail "the demo root is empty"
fi
if [ -L "$RAW_ROOT" ]; then
  fail "the demo root $RAW_ROOT is a symlink; pass the real directory instead"
fi
if [ -e "$RAW_ROOT" ] && [ ! -d "$RAW_ROOT" ]; then
  fail "$RAW_ROOT exists and is not a directory"
fi
mkdir -p "$RAW_ROOT" || fail "cannot create the demo root $RAW_ROOT"
ROOT=$(cd "$RAW_ROOT" && pwd -P)
case "$ROOT" in
  /|/home|/tmp|/var|/usr|/etc|/data|/app|"$HOME") fail "refusing to use $ROOT as a demo root" ;;
esac
if [ "$ROOT" = "$REPO_REAL" ]; then
  fail "refusing to reset the repository root; pick a scratch directory"
fi
case "$ROOT" in
  "$REPO_REAL"/*) fail "refusing a demo root inside the repository ($ROOT)" ;;
esac
case "${ROOT#/}" in
  */*) : ;;
  *) fail "refusing a top-level directory ($ROOT) as a demo root" ;;
esac

DATA_DIR="$ROOT/data"
CONFIG_DIR="$ROOT/config"
for dir in "$DATA_DIR" "$CONFIG_DIR"; do
  if [ -L "$dir" ]; then
    fail "$dir is a symlink; refusing to write through it"
  fi
  if [ -e "$dir" ] && [ ! -d "$dir" ]; then
    fail "$dir exists and is not a directory"
  fi
done
mkdir -p "$DATA_DIR" "$CONFIG_DIR"
DATA_REAL=$(cd "$DATA_DIR" && pwd -P)
case "$DATA_REAL" in
  "$ROOT"/*) : ;;
  *) fail "$DATA_DIR resolves outside the demo root" ;;
esac
echo "demo root: $ROOT"

# --- port guard: exact match, and never reset while the port is busy ---------
listener_pids() {
  ss -ltnpH "sport = :$PORT" 2>/dev/null | sed -n 's/.*pid=\([0-9]*\).*/\1/p'
}

if ! command -v ss >/dev/null 2>&1; then
  if [ "$SKIP_PORT_CHECK" != 1 ]; then
    fail "ss is unavailable, so the demo port cannot be checked; stop any instance on port $PORT and re-run with SKIP_PORT_CHECK=1"
  fi
else
  PIDS=$(listener_pids || true)
  if [ -n "$PIDS" ]; then
    if [ "$FORCE" = 1 ]; then
      echo "stopping instance(s) on port $PORT: $(echo "$PIDS" | tr '\n' ' ')"
      for pid in $PIDS; do kill "$pid" 2>/dev/null || true; done
      for _ in $(seq 1 40); do
        if [ -z "$(listener_pids || true)" ]; then break; fi
        sleep 0.25
      done
    else
      fail "port $PORT is in use (pid(s): $(echo "$PIDS" | tr '\n' ' ')); stop it first or re-run with FORCE=1"
    fi
  fi
  if [ -n "$(listener_pids || true)" ]; then
    fail "port $PORT is still in use; refusing to reset the demo data"
  fi
fi

# --- reset the validated root only ------------------------------------------
printf '{"bindAddr":":%s"}' "$PORT" > "$CONFIG_DIR/bootstrap.json"
rm -f "$DATA_DIR"/*.json "$DATA_DIR"/zapatos.sqlite "$DATA_DIR"/zapatos.sqlite-wal "$DATA_DIR"/zapatos.sqlite-shm "$DATA_DIR"/migration.lock
rm -rf "$DATA_DIR/migration-backups"
cp "$REPO_DIR"/demo/data/*.json "$DATA_DIR/"
echo "demo data copied to $DATA_DIR"

if [ "$START" != 1 ]; then
  echo "START=$START: data ready, app not started"
  exit 0
fi

# --- binary stays inside the root -------------------------------------------
BIN=${BIN:-$ROOT/zapatos-erp}
BIN_DIR=$(cd "$(dirname "$BIN")" && pwd -P)
case "$BIN_DIR" in
  "$ROOT"|"$ROOT"/*) : ;;
  *) fail "BIN must live inside the demo root ($ROOT); got $BIN" ;;
esac
if [ ! -x "$BIN" ]; then
  echo "building $BIN"
  (cd "$REPO_DIR" && CGO_ENABLED=0 go build -o "$BIN" ./cmd/zapatos-erp)
fi

# --- start, then require a real health answer -------------------------------
(cd "$ROOT" && setsid nohup "$BIN" > "$ROOT/server.log" 2>&1 < /dev/null &)
HEALTHY=0
for _ in $(seq 1 60); do
  if curl -sf "http://127.0.0.1:$PORT/health" >/dev/null 2>&1; then HEALTHY=1; break; fi
  sleep 0.5
done
if [ "$HEALTHY" != 1 ]; then
  echo "ERROR: the demo instance never answered GET /health on port $PORT" >&2
  tail -20 "$ROOT/server.log" >&2 || true
  exit 1
fi
curl -sf "http://127.0.0.1:$PORT/health"
echo
echo "demo instance running: http://127.0.0.1:$PORT/app (root: $ROOT)"
echo "stop it with: kill \$(ss -ltnp \"sport = :$PORT\" | sed -n 's/.*pid=\\([0-9]*\\).*/\\1/p' | head -1)"
