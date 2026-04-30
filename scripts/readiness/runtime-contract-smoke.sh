#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BIN="${BIN:-/tmp/agency-runtime-smoke}"
AGENT_NAME="${AGENT_NAME:-}"
RUN_TESTS=1
RUN_DOCTOR=1
CONFIG_PATH="${CONFIG_PATH:-$HOME/.agency/config.yaml}"
TOKEN="${TOKEN:-}"
AGENCY_HOME_DIR=""
START_GATEWAY=0
GATEWAY_ADDR=""
GATEWAY_URL="${AGENCY_GATEWAY_URL:-http://127.0.0.1:8200}"
GATEWAY_PID=""

usage() {
  cat <<'EOF'
Usage: ./scripts/readiness/runtime-contract-smoke.sh [--agent NAME] [--home PATH] [--config PATH] [--bin PATH] [--token TOKEN] [--start-gateway] [--gateway-addr ADDR] [--skip-tests] [--skip-doctor]

Smoke checks:
  1. go test ./...
  2. go build ./cmd/gateway
  3. gateway health probe, optionally starting a temporary gateway
  4. agent runtime manifest/status/validate endpoints (if --agent is provided)
  5. agency admin doctor

The script does not create or restart agents. If an agent has not been started
through the new runtime supervisor path yet, runtime endpoint checks will be
reported as skipped rather than mutating the current environment.
EOF
}

log() {
  printf '==> %s\n' "$1"
}

fail() {
  printf 'ERROR: %s\n' "$1" >&2
  exit 1
}

cleanup() {
  if [[ -n "$GATEWAY_PID" ]]; then
    kill "$GATEWAY_PID" >/dev/null 2>&1 || true
    wait "$GATEWAY_PID" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT INT TERM HUP

gateway_token() {
  if [[ -n "$TOKEN" ]]; then
    printf '%s\n' "$TOKEN"
    return 0
  fi
  if [[ -f "$CONFIG_PATH" ]]; then
    awk '/^token:[[:space:]]*/ {print $2; exit}' "$CONFIG_PATH"
  fi
}

refresh_auth_args() {
  AUTH_TOKEN="$(gateway_token)"
  AUTH_ARGS=()
  if [[ -n "$AUTH_TOKEN" ]]; then
    AUTH_ARGS=(-H "Authorization: Bearer ${AUTH_TOKEN}")
  fi
}

free_gateway_addr() {
  python3 - <<'PY'
import socket

with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
    sock.bind(("127.0.0.1", 0))
    print(f"127.0.0.1:{sock.getsockname()[1]}")
PY
}

wait_gateway() {
  local url="$1"
  local deadline=$((SECONDS + 20))
  until curl -fsS "${AUTH_ARGS[@]}" "$url/api/v1/health" >/tmp/runtime-smoke-health.json 2>/dev/null; do
    if (( SECONDS >= deadline )); then
      return 1
    fi
    sleep 0.2
  done
}

start_gateway() {
  [[ -n "$AGENCY_HOME_DIR" ]] || fail "--start-gateway requires --home"
  if [[ -z "$GATEWAY_ADDR" ]]; then
    GATEWAY_ADDR="$(free_gateway_addr)"
  fi
  GATEWAY_URL="http://$GATEWAY_ADDR"
  log "Starting temporary gateway on $GATEWAY_ADDR"
  "$BIN" -H "$AGENCY_HOME_DIR" serve --http "$GATEWAY_ADDR" >/tmp/runtime-smoke-gateway.log 2>&1 &
  GATEWAY_PID="$!"
  if ! wait_gateway "$GATEWAY_URL"; then
    cat /tmp/runtime-smoke-gateway.log >&2 || true
    fail "temporary gateway did not become healthy"
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --agent)
      [[ $# -ge 2 ]] || fail "--agent requires a value"
      AGENT_NAME="$2"
      shift 2
      ;;
    --config)
      [[ $# -ge 2 ]] || fail "--config requires a path"
      CONFIG_PATH="$2"
      shift 2
      ;;
    --home)
      [[ $# -ge 2 ]] || fail "--home requires a path"
      AGENCY_HOME_DIR="$2"
      CONFIG_PATH="$AGENCY_HOME_DIR/config.yaml"
      shift 2
      ;;
    --bin)
      [[ $# -ge 2 ]] || fail "--bin requires a path"
      BIN="$2"
      shift 2
      ;;
    --token)
      [[ $# -ge 2 ]] || fail "--token requires a value"
      TOKEN="$2"
      shift 2
      ;;
    --start-gateway)
      START_GATEWAY=1
      shift
      ;;
    --gateway-addr)
      [[ $# -ge 2 ]] || fail "--gateway-addr requires a value"
      GATEWAY_ADDR="$2"
      GATEWAY_URL="http://$GATEWAY_ADDR"
      shift 2
      ;;
    --skip-tests)
      RUN_TESTS=0
      shift
      ;;
    --skip-doctor)
      RUN_DOCTOR=0
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      fail "unknown argument: $1"
      ;;
  esac
done

cd "$ROOT"

refresh_auth_args

if [[ "$RUN_TESTS" -eq 1 ]]; then
  log "Running go test ./..."
  go test ./...
fi

log "Building gateway binary"
go build -o "$BIN" ./cmd/gateway

if [[ "$START_GATEWAY" -eq 1 ]]; then
  start_gateway
  refresh_auth_args
elif curl -fsS "${AUTH_ARGS[@]}" "$GATEWAY_URL/api/v1/health" >/tmp/runtime-smoke-health.json 2>/dev/null; then
  log "Gateway health endpoint is reachable"
else
  log "Gateway health endpoint is not reachable; skipping HTTP smoke checks"
  AGENT_NAME=""
fi

if [[ -n "$AGENT_NAME" ]]; then
  manifest_url="$GATEWAY_URL/api/v1/agents/${AGENT_NAME}/runtime/manifest"
  status_url="$GATEWAY_URL/api/v1/agents/${AGENT_NAME}/runtime/status"
  validate_url="$GATEWAY_URL/api/v1/agents/${AGENT_NAME}/runtime/validate"

  log "Checking runtime manifest endpoint for agent ${AGENT_NAME}"
  if curl -fsS "${AUTH_ARGS[@]}" "$manifest_url" >/tmp/runtime-smoke-manifest.json 2>/dev/null; then
    log "Checking runtime status endpoint for agent ${AGENT_NAME}"
    curl -fsS "${AUTH_ARGS[@]}" "$status_url" >/tmp/runtime-smoke-status.json

    log "Checking runtime validate endpoint for agent ${AGENT_NAME}"
    validate_code="$(curl -sS "${AUTH_ARGS[@]}" -X POST -o /tmp/runtime-smoke-validate.json -w '%{http_code}' "$validate_url")"
    if [[ "$validate_code" == "200" ]]; then
      log "Runtime validate succeeded for ${AGENT_NAME}"
    elif [[ "$validate_code" == "400" ]]; then
      log "Runtime validate returned a fail-closed reason for ${AGENT_NAME}"
      cat /tmp/runtime-smoke-validate.json
      printf '\n'
    else
      fail "unexpected runtime validate status for ${AGENT_NAME}: ${validate_code}"
    fi
  else
    if [[ "$START_GATEWAY" -eq 1 ]]; then
      fail "runtime manifest for ${AGENT_NAME} is not present on temporary gateway"
    fi
    log "Runtime manifest for ${AGENT_NAME} is not present yet; skipping runtime endpoint smoke"
  fi
fi

if [[ "$RUN_DOCTOR" -eq 1 ]]; then
  log "Running admin doctor"
  if [[ -z "$AGENCY_HOME_DIR" && "$CONFIG_PATH" == */config.yaml ]]; then
    AGENCY_HOME_DIR="$(dirname "$CONFIG_PATH")"
  fi
  export AGENCY_GATEWAY_URL="$GATEWAY_URL"
  if [[ -n "$AGENCY_HOME_DIR" ]]; then
    "$BIN" -H "$AGENCY_HOME_DIR" -q admin doctor
  else
    "$BIN" -q admin doctor
  fi
fi

log "Runtime contract smoke checks completed"
