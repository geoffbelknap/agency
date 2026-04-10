#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
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

credential_names_for_provider() {
  case "$1" in
    anthropic)
      printf '%s\n' anthropic-api-key ANTHROPIC_API_KEY
      ;;
    openai)
      printf '%s\n' openai-api-key OPENAI_API_KEY
      ;;
    gemini)
      printf '%s\n' gemini-api-key GEMINI_API_KEY
      ;;
    *)
      printf '%s\n' "${1}-api-key" "$(printf '%s_API_KEY' "$1" | tr '[:lower:]-' '[:upper:]_')"
      ;;
  esac
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
  printf '\nDiagnostics for %s:\n' "$AGENT_NAME" >&2
  run_agency show "$AGENT_NAME" >&2 || true
  run_agency log "$AGENT_NAME" >&2 || true
  docker ps -a --filter "name=agency-${AGENT_NAME}" --format '{{.Names}} {{.Status}}' >&2 || true
  docker logs --tail 80 "agency-${AGENT_NAME}-enforcer" >&2 || true
  docker logs --tail 80 "agency-${AGENT_NAME}-workspace" >&2 || true
}

log "Checking daemon and infrastructure"
run_agency serve restart >/dev/null
log "Starting infrastructure; this can take several minutes when images are stale"
run_agency infra up

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

creds="$(run_agency creds list)"
cred_ok=0
while IFS= read -r name; do
  if printf '%s\n' "$creds" | awk '{print $1}' | grep -qx "$name"; then
    cred_ok=1
    break
  fi
done < <(credential_names_for_provider "$provider")
if [ "$cred_ok" != "1" ]; then
  fail "No credential found for provider '$provider'. Run agency setup --provider $provider."
fi

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
    log "Alpha readiness check passed"
    exit 0
  fi
  sleep "$POLL_INTERVAL"
done

fail "Agent did not respond within ${RESPONSE_TIMEOUT}s"
