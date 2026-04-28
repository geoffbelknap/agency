#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
WEB_DIR="$ROOT_DIR/web"
WEB_BASE_URL="${AGENCY_WEB_BASE_URL:-http://127.0.0.1:5173}"
CONFIG="${AGENCY_PLAYWRIGHT_CONFIG:-playwright.live.risky.config.ts}"
SPEC="${AGENCY_PLAYWRIGHT_SPEC:-tests/e2e-live-risky/firecracker-webui-smoke.spec.ts}"

cd "$WEB_DIR"
exec env \
  AGENCY_E2E_FIRECRACKER_WEBUI=1 \
  AGENCY_WEB_BASE_URL="$WEB_BASE_URL" \
  npx playwright test -c "$CONFIG" "$SPEC" "$@"
