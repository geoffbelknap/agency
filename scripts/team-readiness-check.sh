#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
AGENCY_BIN="${AGENCY_BIN:-}"
AGENCY_HOME_DIR="${AGENCY_HOME:-$HOME/.agency}"
WEB_URL="http://127.0.0.1:8280"
GATEWAY_URL="http://127.0.0.1:8200"
SUFFIX="$(date +%s)"
TEAM_COORD="team-coord-${SUFFIX}"
TEAM_COVER="team-cover-${SUFFIX}"
TEAM_A="team-claim-a-${SUFFIX}"
TEAM_B="team-claim-b-${SUFFIX}"
COORD_TEAM="coord-team-${SUFFIX}"
CLAIM_TEAM="claim-team-${SUFFIX}"
FAILOVER_MISSION="team-failover-${SUFFIX}"
CLAIM_MISSION="team-claim-${SUFFIX}"
POLL_INTERVAL=2
START_TIMEOUT="${AGENCY_TEAM_START_TIMEOUT:-420}"
FAILOVER_TIMEOUT="${AGENCY_TEAM_FAILOVER_TIMEOUT:-45}"
CREATED_AGENTS=()

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

api_json() {
  local method="$1"
  local path="$2"
  local content_type="${3:-application/json}"
  local data_file="${4:-}"

  if [ -n "$data_file" ]; then
    curl -fsS -X "$method" \
      -H "Authorization: Bearer ${GATEWAY_TOKEN}" \
      -H "Content-Type: ${content_type}" \
      --data-binary @"$data_file" \
      "${GATEWAY_URL}${path}"
  else
    curl -fsS -X "$method" \
      -H "Authorization: Bearer ${GATEWAY_TOKEN}" \
      "${GATEWAY_URL}${path}"
  fi
}

api_json_inline() {
  local method="$1"
  local path="$2"
  local payload="$3"
  curl -fsS -X "$method" \
    -H "Authorization: Bearer ${GATEWAY_TOKEN}" \
    -H "Content-Type: application/json" \
    --data "$payload" \
    "${GATEWAY_URL}${path}"
}

agent_mission_path() {
  printf '%s\n' "${AGENCY_HOME_DIR}/agents/$1/mission.yaml"
}

audit_log_path() {
  printf '%s\n' "${AGENCY_HOME_DIR}/audit/$1/gateway.jsonl"
}

wait_for_agent_running() {
  local agent_name="$1"
  local deadline=$((SECONDS + START_TIMEOUT))
  local detail
  while [ "$SECONDS" -lt "$deadline" ]; do
    detail="$(run_agency show "$agent_name" 2>/dev/null || true)"
    if printf '%s\n' "$detail" | grep -q '"status": "running"'; then
      return 0
    fi
    sleep 2
  done
  return 1
}

cleanup() {
  set +e
  for mission in "$FAILOVER_MISSION" "$CLAIM_MISSION"; do
    if curl -fsS -H "Authorization: Bearer ${GATEWAY_TOKEN:-}" "${GATEWAY_URL}/api/v1/missions/${mission}" >/dev/null 2>&1; then
      api_json_inline POST "/api/v1/missions/${mission}/complete" '{"summary":"team readiness cleanup"}' >/dev/null 2>&1 || true
      api_json DELETE "/api/v1/missions/${mission}" >/dev/null 2>&1 || true
    fi
  done
  rm -rf "${AGENCY_HOME_DIR}/teams/${COORD_TEAM}" "${AGENCY_HOME_DIR}/teams/${CLAIM_TEAM}" >/dev/null 2>&1 || true
  for agent in "${CREATED_AGENTS[@]:-}"; do
    run_agency delete "$agent" >/dev/null 2>&1 || true
    if run_agency comms list 2>/dev/null | grep -q "dm-${agent}"; then
      run_agency comms archive "dm-${agent}" >/dev/null 2>&1 || true
    fi
  done
}
trap cleanup EXIT INT TERM HUP

if ! AGENCY_BIN="$(resolve_agency_bin)"; then
  fail "Could not find agency binary. Run make build or install agency first."
fi

GATEWAY_TOKEN="$(read_gateway_token)"
if [ -z "$GATEWAY_TOKEN" ]; then
  fail "No gateway token found in config.yaml"
fi

log "Checking daemon, infrastructure, and Web UI"
run_agency serve restart >/dev/null
run_agency infra up
curl -fsS "$WEB_URL" >/dev/null ||
  fail "Web UI did not return 200 at $WEB_URL"

create_agent() {
  local name="$1"
  run_agency create "$name" --preset researcher >/dev/null
  CREATED_AGENTS+=("$name")
}

log "Creating disposable team agents"
for agent in "$TEAM_COORD" "$TEAM_COVER" "$TEAM_A" "$TEAM_B"; do
  create_agent "$agent"
done

log "Starting coordinator agent"
run_agency start "$TEAM_COORD" >/dev/null
wait_for_agent_running "$TEAM_COORD" || fail "Coordinator agent did not reach running state"

log "Starting coverage agent"
run_agency start "$TEAM_COVER" >/dev/null
wait_for_agent_running "$TEAM_COVER" || fail "Coverage agent did not reach running state"

log "Writing coordinator team config"
mkdir -p "${AGENCY_HOME_DIR}/teams/${COORD_TEAM}"
cat > "${AGENCY_HOME_DIR}/teams/${COORD_TEAM}/team.yaml" <<EOF
version: "0.1"
name: ${COORD_TEAM}
description: Team readiness coordinator failover proof
coordinator: ${TEAM_COORD}
coverage: ${TEAM_COVER}
members:
  - name: ${TEAM_COORD}
    type: agent
    agent_type: coordinator
  - name: ${TEAM_COVER}
    type: agent
EOF

FAILOVER_YAML="$(mktemp /tmp/team-failover.XXXXXX)"
cat > "$FAILOVER_YAML" <<EOF
name: ${FAILOVER_MISSION}
description: Team readiness failover proof
instructions: Coordinate this mission and ensure coverage handoff works.
EOF

log "Creating and assigning coordinator team mission"
api_json POST "/api/v1/missions" "application/yaml" "$FAILOVER_YAML" >/dev/null
api_json_inline POST "/api/v1/missions/${FAILOVER_MISSION}/assign" "{\"target\":\"${COORD_TEAM}\",\"type\":\"team\"}" >/dev/null

[ -f "$(agent_mission_path "$TEAM_COORD")" ] ||
  fail "Coordinator did not receive mission copy"
[ ! -f "$(agent_mission_path "$TEAM_COVER")" ] ||
  fail "Coverage agent should not receive mission copy before failover"

log "Halting coordinator to trigger failover"
api_json_inline POST "/api/v1/agents/${TEAM_COORD}/stop" '{"type":"supervised","reason":"team readiness failover probe","initiator":"operator"}' >/dev/null

deadline=$((SECONDS + FAILOVER_TIMEOUT))
while [ "$SECONDS" -lt "$deadline" ]; do
  if [ -f "$(agent_mission_path "$TEAM_COVER")" ] &&
     grep -q 'mission_coordinator_failover' "$(audit_log_path "$TEAM_COORD")" 2>/dev/null; then
    break
  fi
  sleep "$POLL_INTERVAL"
done

[ -f "$(agent_mission_path "$TEAM_COVER")" ] ||
  fail "Coverage agent did not receive mission copy after coordinator halt"
grep -q 'mission_coordinator_failover' "$(audit_log_path "$TEAM_COORD")" ||
  fail "Coordinator audit log did not record failover"

log "Writing no-coordinator team config"
mkdir -p "${AGENCY_HOME_DIR}/teams/${CLAIM_TEAM}"
cat > "${AGENCY_HOME_DIR}/teams/${CLAIM_TEAM}/team.yaml" <<EOF
version: "0.1"
name: ${CLAIM_TEAM}
description: Team readiness claim proof
members:
  - name: ${TEAM_A}
    type: agent
  - name: ${TEAM_B}
    type: agent
EOF

CLAIM_YAML="$(mktemp /tmp/team-claim.XXXXXX)"
cat > "$CLAIM_YAML" <<EOF
name: ${CLAIM_MISSION}
description: Team readiness deconfliction proof
instructions: Claim trigger events before acting.
EOF

log "Creating and assigning no-coordinator team mission"
api_json POST "/api/v1/missions" "application/yaml" "$CLAIM_YAML" >/dev/null
api_json_inline POST "/api/v1/missions/${CLAIM_MISSION}/assign" "{\"target\":\"${CLAIM_TEAM}\",\"type\":\"team\"}" >/dev/null

[ -f "$(agent_mission_path "$TEAM_A")" ] ||
  fail "First no-coordinator member did not receive mission copy"
[ -f "$(agent_mission_path "$TEAM_B")" ] ||
  fail "Second no-coordinator member did not receive mission copy"

log "Checking no-coordinator claim deconfliction"
claim_first="$(api_json_inline POST "/api/v1/missions/${CLAIM_MISSION}/claim" "{\"event_key\":\"INC-123\",\"agent_name\":\"${TEAM_A}\"}")"
printf '%s\n' "$claim_first" | grep -q '"claimed":true' ||
  fail "First claim did not succeed: $claim_first"
printf '%s\n' "$claim_first" | grep -q "\"holder\":\"${TEAM_A}\"" ||
  fail "First claim did not record the expected holder: $claim_first"

claim_second="$(api_json_inline POST "/api/v1/missions/${CLAIM_MISSION}/claim" "{\"event_key\":\"INC-123\",\"agent_name\":\"${TEAM_B}\"}")"
printf '%s\n' "$claim_second" | grep -q '"claimed":false' ||
  fail "Second claim unexpectedly succeeded: $claim_second"
printf '%s\n' "$claim_second" | grep -q "\"holder\":\"${TEAM_A}\"" ||
  fail "Second claim did not report the original holder: $claim_second"

log "Team readiness check passed"
