#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
WEB_DIR="$ROOT_DIR/web"
AGENCY_BIN="${AGENCY_BIN:-}"
PLAYWRIGHT_CONFIG="${AGENCY_PLAYWRIGHT_CONFIG:-playwright.live.config.ts}"

if [ -z "$AGENCY_BIN" ]; then
  if command -v agency >/dev/null 2>&1; then
    AGENCY_BIN="$(command -v agency)"
  elif [ -x "$HOME/.agency/bin/agency" ]; then
    AGENCY_BIN="$HOME/.agency/bin/agency"
  else
    echo "agency binary not found. Set AGENCY_BIN or run 'make install' first."
    exit 1
  fi
fi

if [ ! -d "$WEB_DIR/node_modules" ]; then
  echo "web/node_modules is missing."
  echo "Run: cd \"$WEB_DIR\" && npm install"
  exit 1
fi

if [ ! -x "$WEB_DIR/node_modules/.bin/playwright" ]; then
  echo "Playwright is not installed in web/node_modules."
  echo "Run: cd \"$WEB_DIR\" && npm install && npx playwright install chromium"
  exit 1
fi

wait_for_url() {
  local url="$1"
  local timeout="${2:-120}"
  local attempt=0

  until curl -kfsS "$url" >/dev/null 2>&1; do
    attempt=$((attempt + 1))
    if [ "$attempt" -ge "$timeout" ]; then
      echo "Timed out waiting for $url"
      return 1
    fi
    sleep 1
  done
}

url_is_healthy() {
  local url="$1"
  curl -kfsS "$url" >/dev/null 2>&1
}

wait_for_infra_healthy() {
  local timeout="${1:-120}"
  local attempt=0
  local status_output=""

  until status_output="$("$AGENCY_BIN" -q infra status 2>/dev/null)"; do
    attempt=$((attempt + 1))
    if [ "$attempt" -ge "$timeout" ]; then
      echo "Timed out waiting for infrastructure status"
      return 1
    fi
    sleep 1
  done

  attempt=0
  until printf '%s\n' "$status_output" | grep -q "web-fetch.*✓" \
    && printf '%s\n' "$status_output" | grep -q "knowledge.*✓" \
    && printf '%s\n' "$status_output" | grep -q "intake.*✓" \
    && printf '%s\n' "$status_output" | grep -q "web.*✓"; do
    attempt=$((attempt + 1))
    if [ "$attempt" -ge "$timeout" ]; then
      echo "Timed out waiting for infrastructure to become healthy"
      printf '%s\n' "$status_output"
      return 1
    fi
    sleep 1
    status_output="$("$AGENCY_BIN" -q infra status 2>/dev/null || true)"
  done
}

gateway_health_url() {
  printf '%s' "${AGENCY_GATEWAY_HEALTH_URL:-http://127.0.0.1:8200/api/v1/health}"
}

web_health_url() {
  printf '%s' "${AGENCY_WEB_BASE_URL:-https://127.0.0.1:8280}/health"
}

if [ "${AGENCY_E2E_SKIP_BUILD:-0}" != "1" ]; then
  echo "==> Building local Agency binary and images"
  make -C "$ROOT_DIR" all
fi

GATEWAY_HEALTH_URL="$(gateway_health_url)"
WEB_HEALTH_URL="$(web_health_url)"

if [ "${AGENCY_E2E_FORCE_RESTART:-0}" = "1" ]; then
  echo "==> Ensuring gateway is running (forced restart)"
  "$AGENCY_BIN" serve restart >/dev/null 2>&1 || true
  wait_for_url "$GATEWAY_HEALTH_URL" 60
elif url_is_healthy "$GATEWAY_HEALTH_URL"; then
  echo "==> Reusing healthy gateway at $GATEWAY_HEALTH_URL"
else
  echo "==> Ensuring gateway is running"
  "$AGENCY_BIN" serve restart >/dev/null 2>&1 || true
  wait_for_url "$GATEWAY_HEALTH_URL" 60
fi

if [ "${AGENCY_E2E_SKIP_INFRA:-0}" != "1" ]; then
  if [ "${AGENCY_E2E_FORCE_INFRA_UP:-0}" = "1" ]; then
    echo "==> Ensuring shared infrastructure is up (forced)"
    if ! "$AGENCY_BIN" -q infra up; then
      echo "agency infra up reported a startup failure; waiting for services to settle..."
    fi
    wait_for_infra_healthy 180
  elif wait_for_infra_healthy 1 >/dev/null 2>&1; then
    echo "==> Reusing healthy shared infrastructure"
  else
    echo "==> Ensuring shared infrastructure is up"
    if ! "$AGENCY_BIN" -q infra up; then
      echo "agency infra up reported a startup failure; waiting for services to settle..."
    fi
    wait_for_infra_healthy 180
  fi
fi

if url_is_healthy "$WEB_HEALTH_URL"; then
  echo "==> Reusing healthy agency-web at $WEB_HEALTH_URL"
else
  echo "==> Waiting for agency-web"
  wait_for_url "$WEB_HEALTH_URL" 120
fi

echo "==> Running live Playwright suite"
cd "$WEB_DIR"
npx playwright test -c "$PLAYWRIGHT_CONFIG" "$@"
