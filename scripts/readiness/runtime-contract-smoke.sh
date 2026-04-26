#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BIN="${BIN:-/tmp/agency-runtime-smoke}"
AGENT_NAME="${AGENT_NAME:-}"
RUN_TESTS=1
RUN_DOCTOR=1
CONFIG_PATH="${CONFIG_PATH:-$HOME/.agency/config.yaml}"
TOKEN="${TOKEN:-}"

usage() {
  cat <<'EOF'
Usage: ./scripts/readiness/runtime-contract-smoke.sh [--agent NAME] [--skip-tests] [--skip-doctor]

Smoke checks:
  1. go test ./...
  2. go build ./cmd/gateway
  3. gateway health probe on localhost:8200 (if available)
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

gateway_token() {
  if [[ -n "$TOKEN" ]]; then
    printf '%s\n' "$TOKEN"
    return 0
  fi
  if [[ -f "$CONFIG_PATH" ]]; then
    awk '/^token:[[:space:]]*/ {print $2; exit}' "$CONFIG_PATH"
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --agent)
      [[ $# -ge 2 ]] || fail "--agent requires a value"
      AGENT_NAME="$2"
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

AUTH_TOKEN="$(gateway_token)"
AUTH_ARGS=()
if [[ -n "$AUTH_TOKEN" ]]; then
  AUTH_ARGS=(-H "Authorization: Bearer ${AUTH_TOKEN}")
fi

if [[ "$RUN_TESTS" -eq 1 ]]; then
  log "Running go test ./..."
  go test ./...
fi

log "Building gateway binary"
go build -o "$BIN" ./cmd/gateway

if curl -fsS "${AUTH_ARGS[@]}" http://127.0.0.1:8200/api/v1/health >/tmp/runtime-smoke-health.json 2>/dev/null; then
  log "Gateway health endpoint is reachable"
else
  log "Gateway health endpoint is not reachable; skipping HTTP smoke checks"
  AGENT_NAME=""
fi

if [[ -n "$AGENT_NAME" ]]; then
  manifest_url="http://127.0.0.1:8200/api/v1/agents/${AGENT_NAME}/runtime/manifest"
  status_url="http://127.0.0.1:8200/api/v1/agents/${AGENT_NAME}/runtime/status"
  validate_url="http://127.0.0.1:8200/api/v1/agents/${AGENT_NAME}/runtime/validate"

  log "Checking runtime manifest endpoint for agent ${AGENT_NAME}"
  if curl -fsS "${AUTH_ARGS[@]}" "$manifest_url" >/tmp/runtime-smoke-manifest.json 2>/dev/null; then
    log "Checking runtime status endpoint for agent ${AGENT_NAME}"
    curl -fsS "${AUTH_ARGS[@]}" "$status_url" >/tmp/runtime-smoke-status.json

    log "Checking runtime validate endpoint for agent ${AGENT_NAME}"
    validate_body="$(curl -sS "${AUTH_ARGS[@]}" -X POST -o /tmp/runtime-smoke-validate.json -w '%{http_code}' "$validate_url")"
    if [[ "$validate_body" != "200" && "$validate_body" != "400" ]]; then
      fail "unexpected runtime validate status for ${AGENT_NAME}: ${validate_body}"
    fi
  else
    log "Runtime manifest for ${AGENT_NAME} is not present yet; skipping runtime endpoint smoke"
  fi
fi

if [[ "$RUN_DOCTOR" -eq 1 ]]; then
  log "Running admin doctor"
  "$BIN" -q admin doctor
fi

log "Runtime contract smoke checks completed"
