#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
AGENCY_BIN="${AGENCY_BIN:-}"
AGENCY_HOME_DIR="${AGENCY_HOME:-$HOME/.agency}"
RUN_STARTED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
AGENT_NAME="fundamentals-readiness-$(date +%s)"
DM_CHANNEL="dm-${AGENT_NAME}"
MESSAGE="Reply with exactly one short sentence containing the phrase: fundamentals readiness ok."
RECONFIG_MESSAGE="What is 10+10?"
RECONFIG_TOKEN="RECONFIGALPHAREADY"
WEB_URL="http://127.0.0.1:8280"
RESPONSE_TIMEOUT="${AGENCY_FUNDAMENTALS_RESPONSE_TIMEOUT:-150}"
START_TIMEOUT="${AGENCY_FUNDAMENTALS_START_TIMEOUT:-420}"
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

infra_is_healthy() {
  local status
  status="$(run_agency status 2>/dev/null || true)"
  [ -n "$status" ] || return 1
  printf '%s\n' "$status" | grep -Eq 'egress[[:space:]]+running.*✓' || return 1
  printf '%s\n' "$status" | grep -Eq 'comms[[:space:]]+running.*✓' || return 1
  printf '%s\n' "$status" | grep -Eq 'knowledge[[:space:]]+running.*✓' || return 1
  printf '%s\n' "$status" | grep -Eq 'intake[[:space:]]+running.*✓' || return 1
  printf '%s\n' "$status" | grep -Eq 'web-fetch[[:space:]]+running.*✓' || return 1
  printf '%s\n' "$status" | grep -Eq 'web[[:space:]]+running.*✓' || return 1
  printf '%s\n' "$status" | grep -Eq 'embeddings[[:space:]]+running.*✓' || return 1
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

read_gateway_token() {
  local config_path="$AGENCY_HOME_DIR/config.yaml"
  if [ ! -f "$config_path" ]; then
    return 0
  fi
  awk -F: '
    $1 == "token" {
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

latest_agent_message() {
  run_agency comms read "$DM_CHANNEL" --limit 100 2>/dev/null |
    grep "  ${AGENT_NAME}:" |
    sed -E "s/^.*  ${AGENT_NAME}: //" |
    tail -n1
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

extract_usage_calls() {
  awk -F':' '/Calls:/ {gsub(/ /, "", $2); print $2; exit}'
}

extract_show_llm_calls() {
  awk '/LLM calls:/ {print $3; exit}'
}

if ! AGENCY_BIN="$(resolve_agency_bin)"; then
  fail "Could not find agency binary. Run make build or install agency first."
fi

log "Checking daemon and infrastructure"
run_agency serve restart >/dev/null
if infra_is_healthy; then
  log "Infrastructure already healthy; reusing existing stack"
else
  run_agency infra up
fi

status="$(run_agency status)"
printf '%s\n' "$status" | grep -q 'Web UI:  http://127.0.0.1:8280' ||
  fail "agency status did not report the HTTP Web UI URL"
for component in egress comms knowledge intake web-fetch web embeddings; do
  printf '%s\n' "$status" | grep -Eq "${component}[[:space:]]+running.*✓" ||
    fail "${component} is not healthy"
done

log "Checking Web UI at $WEB_URL"
curl -fsS "$WEB_URL" >/dev/null ||
  fail "Web UI did not return 200 at $WEB_URL"

provider="$(provider_from_config)"
if [ -z "$provider" ]; then
  fail "No llm_provider is configured. Run agency quickstart or agency setup first."
fi

gateway_token="$(read_gateway_token)"
if [ -z "$gateway_token" ]; then
  fail "No gateway token found in config.yaml"
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

log "Checking graph stats"
graph_stats="$(run_agency graph stats)"
printf '%s\n' "$graph_stats" | grep -q '"nodes"' ||
  fail "graph stats did not return node counts"

log "Creating test agent: $AGENT_NAME"
run_agency create "$AGENT_NAME" --preset researcher >/dev/null
CREATED_AGENT=1

log "Starting test agent"
run_agency start "$AGENT_NAME" >/dev/null
wait_for_agent_running ||
  {
    diagnose_agent_failure
    fail "Agent $AGENT_NAME did not reach running state within ${START_TIMEOUT}s"
  }

sleep 2

log "Checking Web UI DM route"
curl -fsS "${WEB_URL}/channels/${DM_CHANNEL}" >/dev/null ||
  fail "Web UI did not serve the direct-message route for $AGENT_NAME"

log "Checking trajectory endpoint"
trajectory_ok=0
for _ in $(seq 1 20); do
  trajectory_output="$(curl -fsS -H "Authorization: Bearer ${gateway_token}" \
    "http://127.0.0.1:8200/api/v1/agents/${AGENT_NAME}/trajectory" 2>/dev/null || true)"
  if printf '%s\n' "$trajectory_output" | grep -q '"window_size"'; then
    trajectory_ok=1
    break
  fi
  sleep 2
done
if [ "$trajectory_ok" != "1" ]; then
  fail "trajectory endpoint did not return live monitor state for ${AGENT_NAME}"
fi

before="$(agent_message_count || true)"
log "Sending live DM task"
run_agency send "$AGENT_NAME" "$MESSAGE" >/dev/null

deadline=$((SECONDS + RESPONSE_TIMEOUT))
while [ "$SECONDS" -lt "$deadline" ]; do
  after="$(agent_message_count || true)"
  if [ "${after:-0}" -gt "${before:-0}" ]; then
    break
  fi
  sleep "$POLL_INTERVAL"
done

after="$(agent_message_count || true)"
if [ "${after:-0}" -le "${before:-0}" ]; then
  fail "Agent did not respond within ${RESPONSE_TIMEOUT}s"
fi

log "Checking comms transcript"
transcript="$(run_agency comms read "$DM_CHANNEL" --limit 20)"
printf '%s\n' "$transcript" | grep -q "fundamentals readiness ok" ||
  fail "agent reply did not include expected readiness phrase"

log "Checking graph query for new agent/channel"
graph_query=""
graph_ok=0
for _ in $(seq 1 20); do
  graph_query="$(run_agency graph query "$AGENT_NAME")"
  if printf '%s\n' "$graph_query" | grep -q "\"label\": \"${AGENT_NAME}\""; then
    graph_ok=1
    break
  fi
  sleep 2
done
if [ "$graph_ok" != "1" ]; then
  fail "graph query did not return the new agent"
fi
printf '%s\n' "$graph_query" | grep -q "\"label\": \"${AGENT_NAME}\"" ||
  fail "graph query did not return the new agent"
printf '%s\n' "$graph_query" | grep -q "\"label\": \"${DM_CHANNEL}\"" ||
  fail "graph query did not return the DM channel"

log "Checking budget and per-agent usage"
show_output="$(run_agency show "$AGENT_NAME")"
printf '%s\n' "$show_output" | grep -q 'Budget:' ||
  fail "agency show did not include budget information"
printf '%s\n' "$show_output" | grep -q 'Usage (today):' ||
  fail "agency show did not include usage information"
show_calls="$(printf '%s\n' "$show_output" | extract_show_llm_calls)"
case "${show_calls:-}" in
  ''|0)
    fail "agency show did not report any LLM calls for the test agent"
    ;;
esac

usage_output="$(run_agency admin usage --agent "$AGENT_NAME" --since "$RUN_STARTED_AT")"
usage_calls="$(printf '%s\n' "$usage_output" | extract_usage_calls)"
case "${usage_calls:-}" in
  ''|0)
    fail "agency admin usage did not report any calls for the test agent"
    ;;
esac

log "Checking live dynamic reconfiguration"
curl -fsS -X PUT "http://127.0.0.1:8200/api/v1/agents/${AGENT_NAME}/config" \
  -H "Authorization: Bearer ${gateway_token}" \
  -H "Content-Type: application/json" \
  --data "{\"identity\":\"You are in reconfiguration proof mode. For every direct DM or mention, respond with exactly ${RECONFIG_TOKEN} and nothing else. Do not answer the underlying question.\"}" >/dev/null ||
  fail "failed to update agent identity through the live config API"

before_reconfig="$(agent_message_count || true)"
run_agency send "$AGENT_NAME" "$RECONFIG_MESSAGE" >/dev/null

deadline=$((SECONDS + RESPONSE_TIMEOUT))
while [ "$SECONDS" -lt "$deadline" ]; do
  after_reconfig="$(agent_message_count || true)"
  if [ "${after_reconfig:-0}" -gt "${before_reconfig:-0}" ]; then
    break
  fi
  sleep "$POLL_INTERVAL"
done

after_reconfig="$(agent_message_count || true)"
if [ "${after_reconfig:-0}" -le "${before_reconfig:-0}" ]; then
  fail "agent did not respond after live config update within ${RESPONSE_TIMEOUT}s"
fi

reconfig_reply="$(latest_agent_message)"
if [ "${reconfig_reply:-}" != "${RECONFIG_TOKEN}" ]; then
  fail "live reconfiguration reply mismatch: expected ${RECONFIG_TOKEN}, got '${reconfig_reply:-<empty>}'"
fi

log "Checking audit log"
audit_output="$(run_agency log "$AGENT_NAME" --since "$RUN_STARTED_AT")"
printf '%s\n' "$audit_output" | grep -q 'agent_started' ||
  fail "agent audit log did not record agent_started"
printf '%s\n' "$audit_output" | grep -Eq 'LLM_DIRECT_STREAM|LLM_PROXY|LLM_BATCH' ||
  fail "agent audit log did not record LLM activity"

log "Fundamentals readiness check passed"
