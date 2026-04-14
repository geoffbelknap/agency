#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
WEB_DIR="$ROOT_DIR/web"
AGENCY_BIN="${AGENCY_BIN:-}"
PLAYWRIGHT_CONFIG="${AGENCY_PLAYWRIGHT_CONFIG:-playwright.live.config.ts}"
SKIP_BUILD="${AGENCY_E2E_SKIP_BUILD:-0}"
SKIP_INFRA="${AGENCY_E2E_SKIP_INFRA:-0}"
FORCE_RESTART="${AGENCY_E2E_FORCE_RESTART:-0}"
FORCE_INFRA_UP="${AGENCY_E2E_FORCE_INFRA_UP:-0}"
ALLOW_DANGER="${AGENCY_E2E_ALLOW_DANGER:-0}"
DANGER_CONFIRM="${AGENCY_E2E_DANGER_CONFIRM:-}"

usage() {
  cat <<'EOF'
Usage: ./scripts/e2e-live-web.sh [options] [playwright test filters...]

Options:
  --skip-build         Reuse the current local Agency binary and images
  --skip-infra         Skip infra up and health orchestration
  --force-restart      Force a gateway restart even if health checks pass
  --force-infra-up     Force infra up even if shared services already look healthy
  --allow-danger       Allow live-danger execution
  --danger-confirm <token>
                       Confirmation token for live-danger runs (expected: destroy-all)
  --config <path>      Playwright config file relative to web/
  -h, --help           Show this help

Any remaining arguments are passed through to:
  npx playwright test -c <config> ...
EOF
}

resolve_agency_bin() {
  if [ -n "${AGENCY_BIN:-}" ] && [ -x "${AGENCY_BIN}" ]; then
    printf '%s\n' "${AGENCY_BIN}"
    return 0
  fi
  if [ -x "$ROOT_DIR/agency" ]; then
    printf '%s\n' "$ROOT_DIR/agency"
    return 0
  fi
  if command -v agency >/dev/null 2>&1; then
    command -v agency
    return 0
  fi
  if [ -x "$HOME/.agency/bin/agency" ]; then
    printf '%s\n' "$HOME/.agency/bin/agency"
    return 0
  fi
  return 1
}

PLAYWRIGHT_ARGS=()
while [ "$#" -gt 0 ]; do
  case "$1" in
    --skip-build)
      SKIP_BUILD=1
      shift
      ;;
    --skip-infra)
      SKIP_INFRA=1
      shift
      ;;
    --force-restart)
      FORCE_RESTART=1
      shift
      ;;
    --force-infra-up)
      FORCE_INFRA_UP=1
      shift
      ;;
    --allow-danger)
      ALLOW_DANGER=1
      shift
      ;;
    --danger-confirm)
      if [ "$#" -lt 2 ]; then
        echo "--danger-confirm requires a confirmation token"
        exit 1
      fi
      DANGER_CONFIRM="$2"
      shift 2
      ;;
    --config)
      if [ "$#" -lt 2 ]; then
        echo "--config requires a path"
        exit 1
      fi
      PLAYWRIGHT_CONFIG="$2"
      shift 2
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

if [ "$PLAYWRIGHT_CONFIG" = "playwright.live.danger.config.ts" ]; then
  if [ "$ALLOW_DANGER" != "1" ]; then
    echo "Refusing to run live-danger without --allow-danger (or AGENCY_E2E_ALLOW_DANGER=1)."
    exit 1
  fi
  if [ "$DANGER_CONFIRM" != "destroy-all" ]; then
    echo "Refusing to run live-danger without --danger-confirm destroy-all."
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
  local instance="${AGENCY_INFRA_INSTANCE:-}"

  if [ -n "$instance" ]; then
    instance="$(printf '%s' "$instance" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9-]+/-/g; s/^-+//; s/-+$//')"
  fi

  if [ -n "$instance" ]; then
    local components="egress comms knowledge web"
    local lines=""
    local all_healthy=0

    until [ "$attempt" -ge "$timeout" ]; do
      lines="$(docker ps --format '{{.Names}} {{.Status}}' 2>/dev/null || true)"
      all_healthy=1
      for component in $components; do
        local expected="agency-infra-${component}-${instance}"
        if ! printf '%s\n' "$lines" | grep -Eq "^${expected} .*healthy"; then
          all_healthy=0
          break
        fi
      done
      if [ "$all_healthy" -eq 1 ]; then
        return 0
      fi
      attempt=$((attempt + 1))
      sleep 1
    done

    echo "Timed out waiting for disposable infrastructure to become healthy"
    for component in $components; do
      local expected="agency-infra-${component}-${instance}"
      printf '%s\n' "$lines" | grep -E "^${expected} " || printf '%s missing\n' "$expected"
    done
    return 1
  fi

  until status_output="$("$AGENCY_BIN" -q infra status 2>/dev/null)"; do
    attempt=$((attempt + 1))
    if [ "$attempt" -ge "$timeout" ]; then
      echo "Timed out waiting for infrastructure status"
      return 1
    fi
    sleep 1
  done

  attempt=0
  until printf '%s\n' "$status_output" | grep -q "egress.*✓" \
    && printf '%s\n' "$status_output" | grep -q "comms.*✓" \
    && printf '%s\n' "$status_output" | grep -q "knowledge.*✓" \
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
  printf '%s' "${AGENCY_WEB_BASE_URL:-http://127.0.0.1:8280}/health"
}

if [ "$SKIP_BUILD" != "1" ]; then
  echo "==> Building local Agency binary and images"
  make -C "$ROOT_DIR" build images-all
  AGENCY_BIN="$ROOT_DIR/agency"
fi

if ! AGENCY_BIN="$(resolve_agency_bin)"; then
  echo "agency binary not found. Set AGENCY_BIN or build the local repo binary first."
  exit 1
fi

GATEWAY_HEALTH_URL="$(gateway_health_url)"
WEB_HEALTH_URL="$(web_health_url)"

if [ "$FORCE_RESTART" = "1" ]; then
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

if [ "$SKIP_INFRA" != "1" ]; then
  if [ "$FORCE_INFRA_UP" = "1" ]; then
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
if [ "$PLAYWRIGHT_CONFIG" = "playwright.live.danger.config.ts" ]; then
  if [ "${#PLAYWRIGHT_ARGS[@]}" -gt 0 ]; then
    AGENCY_E2E_ALLOW_DANGER=1 AGENCY_E2E_DANGER_CONFIRM=destroy-all \
      npx playwright test -c "$PLAYWRIGHT_CONFIG" "${PLAYWRIGHT_ARGS[@]}"
  else
    AGENCY_E2E_ALLOW_DANGER=1 AGENCY_E2E_DANGER_CONFIRM=destroy-all \
      npx playwright test -c "$PLAYWRIGHT_CONFIG"
  fi
elif [ "${#PLAYWRIGHT_ARGS[@]}" -gt 0 ]; then
  npx playwright test -c "$PLAYWRIGHT_CONFIG" "${PLAYWRIGHT_ARGS[@]}"
else
  npx playwright test -c "$PLAYWRIGHT_CONFIG"
fi
