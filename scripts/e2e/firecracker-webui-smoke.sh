#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
WEB_DIR="$ROOT_DIR/web"
WEB_BASE_URL="${AGENCY_WEB_BASE_URL:-http://127.0.0.1:5173}"
CONFIG="${AGENCY_PLAYWRIGHT_CONFIG:-playwright.live.risky.config.ts}"
SPEC="${AGENCY_PLAYWRIGHT_SPEC:-tests/e2e-live-risky/firecracker-webui-smoke.spec.ts}"
MODE="all"

usage() {
  cat <<'EOF'
Usage: ./scripts/e2e/firecracker-webui-smoke.sh [mode] [playwright args...]

Modes:
  all       Run every Firecracker Web UI smoke test
  manage    Create, inspect, DM, and delete a Firecracker agent
  recover   Recover a degraded Firecracker runtime through the Web UI
  cleanup   Stop/delete and verify per-agent runtime artifacts are removed

Any extra arguments are passed through to Playwright.
EOF
}

if [ "$#" -gt 0 ]; then
  case "$1" in
    all|manage|recover|cleanup)
      MODE="$1"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
  esac
fi

PLAYWRIGHT_ARGS=()
case "$MODE" in
  all)
    ;;
  manage)
    PLAYWRIGHT_ARGS+=("-g" "Firecracker agent can be managed")
    ;;
  recover)
    PLAYWRIGHT_ARGS+=("-g" "Firecracker degraded runtime can be recovered")
    ;;
  cleanup)
    PLAYWRIGHT_ARGS+=("-g" "Firecracker stop and delete clean up")
    ;;
  *)
    echo "Unknown mode: $MODE"
    usage
    exit 1
    ;;
esac
PLAYWRIGHT_ARGS+=("$@")

cd "$WEB_DIR"
exec env \
  AGENCY_E2E_FIRECRACKER_WEBUI=1 \
  AGENCY_WEB_BASE_URL="$WEB_BASE_URL" \
  npx playwright test -c "$CONFIG" "$SPEC" "${PLAYWRIGHT_ARGS[@]}"
