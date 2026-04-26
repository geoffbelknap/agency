#!/usr/bin/env bash
set -euo pipefail

# Legacy script name kept for compatibility. This is the readiness check for
# the current core Agency path.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
AGENCY_BIN="${AGENCY_BIN:-}"
AGENCY_HOME_DIR="${AGENCY_HOME:-$HOME/.agency}"
AGENT_NAME="alpha-readiness-$(date +%s)"
DM_CHANNEL="dm-${AGENT_NAME}"
MESSAGE="Alpha readiness check: reply with exactly one short sentence confirming the live path works."
WEB_URL="http://127.0.0.1:8280"
RESPONSE_TIMEOUT="${AGENCY_ALPHA_RESPONSE_TIMEOUT:-120}"
START_TIMEOUT="${AGENCY_ALPHA_START_TIMEOUT:-420}"
POLL_INTERVAL=2
CREATED_AGENT=0

runtime_cli() {
  local config_path="${AGENCY_HOME_DIR}/config.yaml"
  local backend=""
  if [ -f "$config_path" ] && command -v ruby >/dev/null 2>&1; then
    backend="$(ruby -e 'require "yaml"; path = ARGV[0]; data = YAML.load_file(path) || {}; hub = data["hub"].is_a?(Hash) ? data["hub"] : {}; value = hub["deployment_backend"].to_s.strip; puts(value)' "$config_path" 2>/dev/null || true)"
  fi
  case "$backend" in
    podman)
      printf '%s\n' podman
      ;;
    *)
      printf '%s\n' docker
      ;;
  esac
}

log() {
  printf '==> %s\n' "$*"
}

fail() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

resolve_agency_bin() {
  if [ -n "$AGENCY_BIN" ]; then
    printf '%s\n' "$AGENCY_BIN"
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
  return 1
}

run_agency() {
  "$AGENCY_BIN" -q "$@"
}

wait_for_status() {
  local deadline=$((SECONDS + 20))
  while [ "$SECONDS" -lt "$deadline" ]; do
    if run_agency status >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  return 1
}

infra_is_healthy() {
  local status
  status="$(run_agency status 2>/dev/null || true)"
  [ -n "$status" ] || return 1
  printf '%s\n' "$status" | grep -Eq 'egress[[:space:]]+running.*✓' || return 1
  printf '%s\n' "$status" | grep -Eq 'comms[[:space:]]+running.*✓' || return 1
  printf '%s\n' "$status" | grep -Eq 'knowledge[[:space:]]+running.*✓' || return 1
  printf '%s\n' "$status" | grep -Eq 'web[[:space:]]+running.*✓' || return 1
}

cleanup() {
  set +e
  if [ "$CREATED_AGENT" = "1" ]; then
    run_agency delete "$AGENT_NAME" >/dev/null 2>&1
  fi
  if run_agency comms list 2>/dev/null | grep -q "$DM_CHANNEL"; then
    run_agency comms archive "$DM_CHANNEL" >/dev/null 2>&1
  fi
}
trap cleanup EXIT INT TERM HUP

if ! AGENCY_BIN="$(resolve_agency_bin)"; then
  fail "Could not find agency binary. Run make build or install agency first."
fi

provider_from_config() {
  local config_path="$AGENCY_HOME_DIR/config.yaml"
  if [ ! -f "$config_path" ]; then
    return 0
  fi
  awk -F: '
    $1 == "llm_provider" {
      gsub(/[ "]/, "", $2)
      print $2
      exit
    }
  ' "$config_path"
}

config_value() {
  local key="$1"
  local config_path="$AGENCY_HOME_DIR/config.yaml"
  if [ ! -f "$config_path" ]; then
    return 0
  fi
  awk -F: -v key="$key" '
    $1 == key {
      gsub(/[ "]/, "", $2)
      print $2
      exit
    }
  ' "$config_path"
}

provider_credential_configured() {
  local provider="$1"
  local gateway_addr
  local token
  gateway_addr="$(config_value gateway_addr)"
  token="$(config_value token)"
  [ -n "$gateway_addr" ] || gateway_addr="127.0.0.1:8200"
  python3 - "$gateway_addr" "$token" "$provider" <<'PY'
import json
import sys
import urllib.request

gateway_addr, token, provider = sys.argv[1:4]
req = urllib.request.Request(f"http://{gateway_addr}/api/v1/infra/providers")
if token:
    req.add_header("Authorization", f"Bearer {token}")
try:
    with urllib.request.urlopen(req, timeout=5) as resp:
        providers = json.load(resp)
except Exception as exc:
    raise SystemExit(f"provider metadata unavailable: {exc}")
for item in providers:
    if item.get("name") == provider:
        if item.get("credential_configured"):
            raise SystemExit(0)
        name = item.get("credential_name") or "provider credential"
        raise SystemExit(f"credential {name!r} is not configured")
raise SystemExit(f"provider {provider!r} is not available")
PY
}

agent_message_count() {
  run_agency comms read "$DM_CHANNEL" --limit 100 2>/dev/null |
    grep -c "  ${AGENT_NAME}:"
}

wait_for_agent_running() {
  local deadline=$((SECONDS + START_TIMEOUT))
  local detail
  while [ "$SECONDS" -lt "$deadline" ]; do
    detail="$(run_agency show "$AGENT_NAME" 2>/dev/null || true)"
    if printf '%s\n' "$detail" | grep -q '"status": "running"'; then
      return 0
    fi
    sleep 2
  done
  return 1
}

diagnose_agent_failure() {
  local cli
  cli="$(runtime_cli)"
  printf '\nDiagnostics for %s:\n' "$AGENT_NAME" >&2
  run_agency show "$AGENT_NAME" >&2 || true
  run_agency log "$AGENT_NAME" >&2 || true
  if command -v "$cli" >/dev/null 2>&1; then
    "$cli" ps -a --filter "name=agency-${AGENT_NAME}" --format '{{.Names}} {{.Status}}' >&2 || true
    "$cli" logs --tail 80 "agency-${AGENT_NAME}-enforcer" >&2 || true
    "$cli" logs --tail 80 "agency-${AGENT_NAME}-workspace" >&2 || true
  fi
}

log "Running legacy alpha-readiness-check for the current core Agency path"
log "Checking daemon and infrastructure"
run_agency serve restart >/dev/null
wait_for_status || fail "gateway did not become reachable after daemon restart"
if infra_is_healthy; then
  log "Infrastructure already healthy; reusing existing stack"
else
  log "Starting infrastructure; this can take several minutes when images are stale"
  run_agency infra up
fi

status="$(run_agency status)"
printf '%s\n' "$status" | grep -q 'Web UI:  http://127.0.0.1:8280' ||
  fail "agency status did not report the HTTP Web UI URL"
printf '%s\n' "$status" | grep -Eq 'egress[[:space:]]+running.*✓' ||
  fail "egress is not healthy"
printf '%s\n' "$status" | grep -Eq 'comms[[:space:]]+running.*✓' ||
  fail "comms is not healthy"
printf '%s\n' "$status" | grep -Eq 'knowledge[[:space:]]+running.*✓' ||
  fail "knowledge is not healthy"
printf '%s\n' "$status" | grep -Eq 'web[[:space:]]+running.*✓' ||
  fail "web is not healthy"

log "Checking Web UI at $WEB_URL"
curl -fsS "$WEB_URL" >/dev/null ||
  fail "Web UI did not return 200 at $WEB_URL"

provider="$(provider_from_config)"
if [ -z "$provider" ]; then
  fail "No llm_provider is configured. Run agency quickstart or agency setup first."
fi

log "Checking provider: $provider"
run_agency hub show "$provider" >/dev/null ||
  fail "Hub provider '$provider' is not installed. Run agency hub install $provider."

provider_credential_configured "$provider" ||
  fail "No credential found for provider '$provider'. Run agency setup --provider $provider."

log "Creating test agent: $AGENT_NAME"
run_agency create "$AGENT_NAME" --preset henry >/dev/null
CREATED_AGENT=1

log "Starting test agent"
run_agency start "$AGENT_NAME" >/dev/null
wait_for_agent_running ||
  {
    diagnose_agent_failure
    fail "Agent $AGENT_NAME did not reach running state within ${START_TIMEOUT}s"
  }

# The body runtime can emit ready just after start returns. Avoid racing the
# first message before it subscribes to comms.
sleep 2

log "Checking Web UI chat route"
curl -fsS "${WEB_URL}/channels/${DM_CHANNEL}" >/dev/null ||
  fail "Web UI did not serve the direct-message route for $AGENT_NAME"

before="$(agent_message_count || true)"
log "Sending live DM task"
run_agency send "$AGENT_NAME" "$MESSAGE" >/dev/null

deadline=$((SECONDS + RESPONSE_TIMEOUT))
while [ "$SECONDS" -lt "$deadline" ]; do
  after="$(agent_message_count || true)"
  if [ "${after:-0}" -gt "${before:-0}" ]; then
    log "Agent responded"
    run_agency comms read "$DM_CHANNEL" --limit 10 | tail -n 8
    log "Cleaning up test agent"
    cleanup
    CREATED_AGENT=0
    log "Legacy alpha readiness check passed for the core Agency path"
    exit 0
  fi
  sleep "$POLL_INTERVAL"
done

fail "Agent did not respond within ${RESPONSE_TIMEOUT}s"
