#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
SOURCE_HOME="${AGENCY_SOURCE_HOME:-${HOME}/.agency}"
DISPOSABLE_HOME="${AGENCY_DISPOSABLE_HOME:-}"
GATEWAY_PORT="${AGENCY_DISPOSABLE_GATEWAY_PORT:-18200}"
WEB_PORT="${AGENCY_DISPOSABLE_WEB_PORT:-18280}"
KEEP_HOME="${AGENCY_DISPOSABLE_KEEP_HOME:-0}"

usage() {
  cat <<'EOF'
Usage: ./scripts/e2e-live-danger-disposable.sh [playwright test filters...]

Creates an isolated temporary Agency home, rewrites gateway/web host ports,
starts the disposable stack, and runs the guarded live-danger suite against it.

Environment:
  AGENCY_SOURCE_HOME             Source Agency home to clone (default: ~/.agency)
  AGENCY_DISPOSABLE_HOME         Target disposable home (default: mktemp)
  AGENCY_DISPOSABLE_GATEWAY_PORT Gateway host port for disposable stack (default: 18200)
  AGENCY_DISPOSABLE_WEB_PORT     Web host port for disposable stack (default: 18280)
  AGENCY_DISPOSABLE_KEEP_HOME=1  Keep disposable home after the run
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

if [ ! -d "$SOURCE_HOME" ]; then
  echo "Source Agency home does not exist: $SOURCE_HOME"
  exit 1
fi

if [ -z "$DISPOSABLE_HOME" ]; then
  DISPOSABLE_HOME="$(mktemp -d "${TMPDIR:-/tmp}/agency-danger-home.XXXXXX")"
else
  mkdir -p "$DISPOSABLE_HOME"
fi

cleanup() {
  if [ "${KEEP_HOME}" = "1" ]; then
    echo "Keeping disposable Agency home at $DISPOSABLE_HOME"
    return
  fi
  rm -rf "$DISPOSABLE_HOME"
}
trap cleanup EXIT

mkdir -p "$DISPOSABLE_HOME"
cp -R "$SOURCE_HOME"/. "$DISPOSABLE_HOME"/
rm -f "$DISPOSABLE_HOME/gateway.pid" "$DISPOSABLE_HOME/gateway.log"
rm -rf "$DISPOSABLE_HOME/run"

export AGENCY_HOME="$DISPOSABLE_HOME"
export AGENCY_WEB_PORT="$WEB_PORT"
export AGENCY_WEB_BASE_URL="https://127.0.0.1:${WEB_PORT}"
export AGENCY_GATEWAY_HEALTH_URL="http://127.0.0.1:${GATEWAY_PORT}/api/v1/health"
export AGENCY_DISPOSABLE_GATEWAY_PORT="$GATEWAY_PORT"
export AGENCY_E2E_ALLOW_DANGER=1
export AGENCY_E2E_DANGER_CONFIRM=destroy-all

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
echo "==> Disposable gateway:     $AGENCY_GATEWAY_HEALTH_URL"
echo "==> Disposable web:         $AGENCY_WEB_BASE_URL"

agency serve restart >/dev/null 2>&1 || true

"$ROOT_DIR/scripts/e2e-live-web.sh" \
  --allow-danger \
  --danger-confirm destroy-all \
  --force-infra-up \
  --config playwright.live.danger.config.ts \
  "$@"
