#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
WEB_DIR="$ROOT_DIR/web"
WEB_BASE_URL="${AGENCY_WEB_BASE_URL:-http://127.0.0.1:8280}"
CONFIG="${AGENCY_PLAYWRIGHT_CONFIG:-playwright.live.risky.config.ts}"
SPEC="${AGENCY_PLAYWRIGHT_SPEC:-tests/e2e-live-risky/apple-container-operator-flow.spec.ts}"

usage() {
  cat <<'EOF'
Usage: ./scripts/e2e/apple-container-webui-smoke.sh [playwright args...]

Runs the macOS Apple Container operator flow against an already-running live
Agency stack configured with backend apple-container.

Expected setup:
  ./scripts/readiness/apple-container-smoke.sh --keep-running

Any arguments are passed through to Playwright.
EOF
}

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
  usage
  exit 0
fi

cd "$WEB_DIR"
exec env \
  AGENCY_E2E_APPLE_CONTAINER_WEBUI=1 \
  AGENCY_WEB_BASE_URL="$WEB_BASE_URL" \
  npx playwright test -c "$CONFIG" "$SPEC" "$@"
