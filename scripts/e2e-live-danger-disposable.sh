#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
SOURCE_HOME="${AGENCY_SOURCE_HOME:-${HOME}/.agency}"
DISPOSABLE_HOME="${AGENCY_DISPOSABLE_HOME:-}"
GATEWAY_PORT="${AGENCY_DISPOSABLE_GATEWAY_PORT:-18200}"
WEB_PORT="${AGENCY_DISPOSABLE_WEB_PORT:-18280}"
PROXY_PORT="${AGENCY_DISPOSABLE_GATEWAY_PROXY_PORT:-18202}"
PROXY_KNOWLEDGE_PORT="${AGENCY_DISPOSABLE_GATEWAY_PROXY_KNOWLEDGE_PORT:-18204}"
PROXY_INTAKE_PORT="${AGENCY_DISPOSABLE_GATEWAY_PROXY_INTAKE_PORT:-18205}"
KNOWLEDGE_PORT="${AGENCY_DISPOSABLE_KNOWLEDGE_PORT:-18214}"
INTAKE_PORT="${AGENCY_DISPOSABLE_INTAKE_PORT:-18215}"
WEB_FETCH_PORT="${AGENCY_DISPOSABLE_WEB_FETCH_PORT:-18216}"
KEEP_HOME="${AGENCY_DISPOSABLE_KEEP_HOME:-0}"
SKIP_BUILD="${AGENCY_E2E_SKIP_BUILD:-0}"

usage() {
  cat <<'EOF'
Usage: ./scripts/e2e-live-danger-disposable.sh [options] [playwright test filters...]

Creates an isolated temporary Agency home, rewrites gateway/web host ports,
starts the disposable stack, and runs the guarded live-danger suite against it.

Options:
  --keep-home         Preserve the disposable Agency home after the run
  --skip-build        Reuse the current local Agency binary and images
  -h, --help          Show this help

Environment:
  AGENCY_SOURCE_HOME             Source Agency home to clone (default: ~/.agency)
  AGENCY_DISPOSABLE_HOME         Target disposable home (default: mktemp)
  AGENCY_DISPOSABLE_GATEWAY_PORT Gateway host port for disposable stack (default: 18200)
  AGENCY_DISPOSABLE_WEB_PORT     Web host port for disposable stack (default: 18280)
  AGENCY_DISPOSABLE_GATEWAY_PROXY_PORT           Gateway-proxy host port for :8202 (default: 18202)
  AGENCY_DISPOSABLE_GATEWAY_PROXY_KNOWLEDGE_PORT Gateway-proxy host port for :8204 (default: 18204)
  AGENCY_DISPOSABLE_GATEWAY_PROXY_INTAKE_PORT    Gateway-proxy host port for :8205 (default: 18205)
  AGENCY_DISPOSABLE_KNOWLEDGE_PORT               Knowledge host port (default: 18214)
  AGENCY_DISPOSABLE_INTAKE_PORT                  Intake host port (default: 18215)
  AGENCY_DISPOSABLE_WEB_FETCH_PORT               Web-fetch host port (default: 18216)
  AGENCY_DISPOSABLE_KEEP_HOME=1  Keep disposable home after the run
EOF
}

PLAYWRIGHT_ARGS=()
while [ "$#" -gt 0 ]; do
  case "$1" in
    --keep-home)
      KEEP_HOME=1
      shift
      ;;
    --skip-build)
      SKIP_BUILD=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --)
      shift
      while [ "$#" -gt 0 ]; do
        PLAYWRIGHT_ARGS+=("$1")
        shift
      done
      ;;
    *)
      PLAYWRIGHT_ARGS+=("$1")
      shift
      ;;
  esac
done

port_in_use() {
  python3 -c 'import socket,sys; s=socket.socket(); s.settimeout(0.2); code=s.connect_ex(("127.0.0.1", int(sys.argv[1]))); s.close(); raise SystemExit(0 if code == 0 else 1)' "$1"
}

pick_free_port() {
  python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()'
}

sanitize_instance() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9-]+/-/g; s/^-+//; s/-+$//'
}

if [ ! -d "$SOURCE_HOME" ]; then
  echo "Source Agency home does not exist: $SOURCE_HOME"
  exit 1
fi

if [ -z "$DISPOSABLE_HOME" ]; then
  DISPOSABLE_HOME="$(mktemp -d "${TMPDIR:-/tmp}/agency-danger-home.XXXXXX")"
else
  mkdir -p "$DISPOSABLE_HOME"
fi

if [ -z "${AGENCY_DISPOSABLE_GATEWAY_PORT:-}" ] && port_in_use "$GATEWAY_PORT"; then
  GATEWAY_PORT="$(pick_free_port)"
fi

if [ -z "${AGENCY_DISPOSABLE_WEB_PORT:-}" ] && port_in_use "$WEB_PORT"; then
  WEB_PORT="$(pick_free_port)"
fi

if [ -z "${AGENCY_DISPOSABLE_GATEWAY_PROXY_PORT:-}" ] && port_in_use "$PROXY_PORT"; then
  PROXY_PORT="$(pick_free_port)"
fi

if [ -z "${AGENCY_DISPOSABLE_GATEWAY_PROXY_KNOWLEDGE_PORT:-}" ] && port_in_use "$PROXY_KNOWLEDGE_PORT"; then
  PROXY_KNOWLEDGE_PORT="$(pick_free_port)"
fi

if [ -z "${AGENCY_DISPOSABLE_GATEWAY_PROXY_INTAKE_PORT:-}" ] && port_in_use "$PROXY_INTAKE_PORT"; then
  PROXY_INTAKE_PORT="$(pick_free_port)"
fi

if [ -z "${AGENCY_DISPOSABLE_KNOWLEDGE_PORT:-}" ] && port_in_use "$KNOWLEDGE_PORT"; then
  KNOWLEDGE_PORT="$(pick_free_port)"
fi

if [ -z "${AGENCY_DISPOSABLE_INTAKE_PORT:-}" ] && port_in_use "$INTAKE_PORT"; then
  INTAKE_PORT="$(pick_free_port)"
fi

if [ -z "${AGENCY_DISPOSABLE_WEB_FETCH_PORT:-}" ] && port_in_use "$WEB_FETCH_PORT"; then
  WEB_FETCH_PORT="$(pick_free_port)"
fi

mkdir -p "$DISPOSABLE_HOME"
cp -R "$SOURCE_HOME"/. "$DISPOSABLE_HOME"/
rm -f "$DISPOSABLE_HOME/gateway.pid" "$DISPOSABLE_HOME/gateway.log"
rm -rf "$DISPOSABLE_HOME/run"

export AGENCY_HOME="$DISPOSABLE_HOME"
export AGENCY_INFRA_INSTANCE="$(sanitize_instance "$(basename "$DISPOSABLE_HOME")")"
export AGENCY_BIN="${AGENCY_BIN:-$ROOT_DIR/agency}"
export AGENCY_GATEWAY_URL="http://127.0.0.1:${GATEWAY_PORT}"
export AGENCY_WEB_PORT="$WEB_PORT"
export AGENCY_GATEWAY_PROXY_PORT="$PROXY_PORT"
export AGENCY_GATEWAY_PROXY_KNOWLEDGE_PORT="$PROXY_KNOWLEDGE_PORT"
export AGENCY_GATEWAY_PROXY_INTAKE_PORT="$PROXY_INTAKE_PORT"
export AGENCY_KNOWLEDGE_PORT="$KNOWLEDGE_PORT"
export AGENCY_INTAKE_PORT="$INTAKE_PORT"
export AGENCY_WEB_FETCH_PORT="$WEB_FETCH_PORT"
export AGENCY_WEB_BASE_URL="http://127.0.0.1:${WEB_PORT}"
export AGENCY_GATEWAY_HEALTH_URL="http://127.0.0.1:${GATEWAY_PORT}/api/v1/health"
export AGENCY_DISPOSABLE_GATEWAY_PORT="$GATEWAY_PORT"

stop_pid() {
  local pid="$1"
  local waited=0

  if [ -z "$pid" ] || ! kill -0 "$pid" 2>/dev/null; then
    return 0
  fi

  kill -TERM "$pid" 2>/dev/null || true
  while kill -0 "$pid" 2>/dev/null && [ "$waited" -lt 10 ]; do
    waited=$((waited + 1))
    sleep 1
  done

  if kill -0 "$pid" 2>/dev/null; then
    kill -KILL "$pid" 2>/dev/null || true
  fi
}

cleanup() {
  local status="$?"
  trap - EXIT INT TERM HUP

  echo "==> Cleaning up disposable Agency runtime"
  AGENCY_HOME="$DISPOSABLE_HOME" AGENCY_INFRA_INSTANCE="$AGENCY_INFRA_INSTANCE" "$AGENCY_BIN" -q infra down >/dev/null 2>&1 || true
  AGENCY_HOME="$DISPOSABLE_HOME" "$AGENCY_BIN" serve stop >/dev/null 2>&1 || true

  if [ -f "$DISPOSABLE_HOME/gateway.pid" ]; then
    stop_pid "$(cat "$DISPOSABLE_HOME/gateway.pid" 2>/dev/null || true)"
    rm -f "$DISPOSABLE_HOME/gateway.pid"
  fi

  if [ "${KEEP_HOME}" = "1" ]; then
    echo "Keeping disposable Agency home at $DISPOSABLE_HOME"
  else
    rm -rf "$DISPOSABLE_HOME"
  fi

  exit "$status"
}
trap cleanup EXIT INT TERM HUP

python3 - <<'PY'
import os
from pathlib import Path
import yaml

home = Path(os.environ["AGENCY_HOME"])
config_path = home / "config.yaml"
data = {}
if config_path.exists():
    data = yaml.safe_load(config_path.read_text()) or {}
data["gateway_addr"] = f"127.0.0.1:{os.environ['AGENCY_DISPOSABLE_GATEWAY_PORT']}"
config_path.write_text(yaml.safe_dump(data, sort_keys=False))
PY

echo "==> Disposable Agency home: $DISPOSABLE_HOME"
echo "==> Disposable infra id:    $AGENCY_INFRA_INSTANCE"
echo "==> Disposable gateway:     $AGENCY_GATEWAY_HEALTH_URL"
echo "==> Disposable web:         $AGENCY_WEB_BASE_URL"

"$ROOT_DIR/scripts/e2e-live-web.sh" \
  --allow-danger \
  --danger-confirm destroy-all \
  --force-infra-up \
  $([ "$SKIP_BUILD" = "1" ] && printf '%s' '--skip-build') \
  --config playwright.live.danger.config.ts \
  "${PLAYWRIGHT_ARGS[@]}"
