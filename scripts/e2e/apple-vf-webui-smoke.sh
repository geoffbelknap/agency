#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
WEB_DIR="$ROOT_DIR/web"
WEB_BASE_URL="${AGENCY_WEB_BASE_URL:-http://127.0.0.1:8280}"
CONFIG="${AGENCY_PLAYWRIGHT_CONFIG:-playwright.live.risky.config.ts}"
SPEC="${AGENCY_PLAYWRIGHT_SPEC:-tests/e2e-live-risky/apple-vf-webui-smoke.spec.ts}"

usage() {
  cat <<'EOF'
Usage: ./scripts/e2e/apple-vf-webui-smoke.sh [playwright args...]

Runs the live Apple VF Web UI operator smoke against an initialized Agency
stack configured with backend apple-vf-microvm.

Any extra arguments are passed through to Playwright.
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

cd "$WEB_DIR"
exec env \
  AGENCY_E2E_APPLE_VF_WEBUI=1 \
  AGENCY_WEB_BASE_URL="$WEB_BASE_URL" \
  npx playwright test -c "$CONFIG" "$SPEC" "$@"
