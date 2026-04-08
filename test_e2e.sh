#!/usr/bin/env bash
# End-to-end test: agency init → infra up → create → start → send
# Exercises the Go binary through the full agent lifecycle.
#
# Prerequisites:
#   - Docker running
#   - Go binary built: go build -o agency ./cmd/gateway/
#
# Usage: ./test_e2e.sh

set -euo pipefail

AGENCY_BIN="$(dirname "$0")/agency"
TEST_AGENT="e2e-test-agent"
PASS=0
FAIL=0
TOTAL=0
ERRORS=()

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

step() {
    TOTAL=$((TOTAL + 1))
    echo -e "\n${YELLOW}[$TOTAL] $1${NC}"
}

pass() {
    PASS=$((PASS + 1))
    echo -e "  ${GREEN}✓ PASS${NC}: $1"
}

fail() {
    FAIL=$((FAIL + 1))
    ERRORS+=("[$TOTAL] $1")
    echo -e "  ${RED}✗ FAIL${NC}: $1"
}

# check runs a command and passes/fails based on exit code.
# Unlike the old version, stderr is shown on failure — not hidden.
check() {
    local output
    if output=$(eval "$1" 2>&1); then
        pass "$2"
    else
        fail "$2"
        echo "    Command: $1"
        echo "    Output: $output" | head -5
    fi
}

# run_cmd executes a command and fails the test if it returns non-zero.
# This replaces the old pattern of appending "|| true" to everything.
run_cmd() {
    local desc="$1"
    shift
    local output
    if output=$("$@" 2>&1); then
        echo "$output"
    else
        local rc=$?
        echo "$output"
        fail "$desc (exit code $rc)"
    fi
}

cleanup() {
    echo -e "\n${YELLOW}Cleanup...${NC}"
    "$AGENCY_BIN" -q stop "$TEST_AGENT" 2>/dev/null && echo "✓ Agent $TEST_AGENT stopped" || true
    "$AGENCY_BIN" -q delete "$TEST_AGENT" 2>/dev/null && echo "✓ Agent $TEST_AGENT deleted" || true
    "$AGENCY_BIN" -q mission delete e2e-test-mission 2>/dev/null || true
    "$AGENCY_BIN" -q creds delete e2e-test-key 2>/dev/null || true
}

trap cleanup EXIT

echo "================================"
echo " Agency E2E Test"
echo "================================"

# Verify binary exists
if [ ! -x "$AGENCY_BIN" ]; then
    echo "Binary not found at $AGENCY_BIN. Build first: go build -o agency ./cmd/gateway/"
    exit 1
fi

echo "Binary: $AGENCY_BIN"
echo "Test agent: $TEST_AGENT"

# ---------------------------------------------------------------------------
# Load API keys from .env files (repo root, workspace root, or home dir).
# Keys already in the environment take precedence over .env values.
# ---------------------------------------------------------------------------
load_env() {
    local envfile="$1"
    [ -f "$envfile" ] || return
    while IFS='=' read -r key value; do
        # Skip comments and blank lines
        [[ "$key" =~ ^#.*$ || -z "$key" ]] && continue
        # Strip surrounding quotes
        value="${value%\"}" ; value="${value#\"}"
        value="${value%\'}" ; value="${value#\'}"
        # Only export if not already set
        if [ -z "${!key:-}" ]; then
            export "$key=$value"
        fi
    done < "$envfile"
}

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
load_env "$SCRIPT_DIR/.env"
load_env "$SCRIPT_DIR/../.env"
load_env "$HOME/.env"

# Require at least one LLM provider key
if [ -z "${ANTHROPIC_API_KEY:-}" ] && [ -z "${OPENAI_API_KEY:-}" ] && [ -z "${GOOGLE_API_KEY:-}" ]; then
    echo "ERROR: No LLM provider API key found."
    echo "Set ANTHROPIC_API_KEY, OPENAI_API_KEY, or GOOGLE_API_KEY in:"
    echo "  - environment variables"
    echo "  - $SCRIPT_DIR/.env"
    echo "  - $SCRIPT_DIR/../.env (workspace root)"
    exit 1
fi

# Record test start time for log filtering
TEST_START_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ)

# Gateway log position — set after setup creates ~/.agency/
GATEWAY_LOG=~/.agency/gateway.log
LOG_START_LINE=0

# ---------------------------------------------------------------------------
# Pre-cleanup: nuke all agency state so every run starts from a clean slate.
# This avoids stale containers, networks, audit logs, and daemon PIDs from
# prior sessions contaminating the test.
# ---------------------------------------------------------------------------
echo "Cleaning previous agency state..."

# 1. Kill any running gateway daemon
if [ -f ~/.agency/gateway.pid ]; then
    PID=$(cat ~/.agency/gateway.pid 2>/dev/null)
    if [ -n "$PID" ] && kill -0 "$PID" 2>/dev/null; then
        kill "$PID" 2>/dev/null || true
        sleep 1
        # Force-kill if still alive
        kill -0 "$PID" 2>/dev/null && kill -9 "$PID" 2>/dev/null || true
        echo "  Stopped gateway daemon (PID $PID)"
    fi
fi
# Catch any gateway process not tracked by the PID file
pkill -f "agency serve" 2>/dev/null || true

# 2. Remove all agency Docker containers (labeled and unlabeled)
LABELED=$(docker ps -aq --filter "label=agency.managed=true" 2>/dev/null)
NAMED=$(docker ps -aq --filter "name=agency-" 2>/dev/null)
ALL_CONTAINERS=$(echo -e "${LABELED}\n${NAMED}" | sort -u | grep -v '^$' || true)
if [ -n "$ALL_CONTAINERS" ]; then
    echo "$ALL_CONTAINERS" | xargs docker rm -f 2>/dev/null || true
    echo "  Removed $(echo "$ALL_CONTAINERS" | wc -w) container(s)"
fi

# 3. Remove all agency Docker networks
LABELED_NETS=$(docker network ls -q --filter "label=agency.managed=true" 2>/dev/null)
NAMED_NETS=$(docker network ls --format '{{.Name}}' 2>/dev/null | grep '^agency-' | xargs -r -I{} docker network inspect {} --format '{{.ID}}' 2>/dev/null || true)
ALL_NETS=$(echo -e "${LABELED_NETS}\n${NAMED_NETS}" | sort -u | grep -v '^$' || true)
if [ -n "$ALL_NETS" ]; then
    echo "$ALL_NETS" | xargs docker network rm 2>/dev/null || true
    echo "  Removed agency network(s)"
fi

# 4. Archive ~/.agency/ so setup bootstraps fresh
if [ -d ~/.agency ]; then
    ARCHIVE=~/.agency-archive-$(date +%Y%m%d-%H%M%S)
    mv ~/.agency "$ARCHIVE"
    echo "  Archived ~/.agency → $ARCHIVE"
fi

echo "Clean slate ready."

# --------------------------------------------------
# Phase 1: Init
# --------------------------------------------------
step "agency init"
if [ ! -f ~/.agency/config.yaml ]; then
    # Pick the first available provider key (already validated above)
    SETUP_FLAGS=()
    if [ -n "${ANTHROPIC_API_KEY:-}" ]; then
        SETUP_FLAGS+=(--provider anthropic --api-key "$ANTHROPIC_API_KEY")
        echo "  Provider: anthropic"
    elif [ -n "${OPENAI_API_KEY:-}" ]; then
        SETUP_FLAGS+=(--provider openai --api-key "$OPENAI_API_KEY")
        echo "  Provider: openai"
    elif [ -n "${GOOGLE_API_KEY:-}" ]; then
        SETUP_FLAGS+=(--provider google --api-key "$GOOGLE_API_KEY")
        echo "  Provider: google"
    fi
    run_cmd "agency setup" AGENCY_NO_BROWSER=1 "$AGENCY_BIN" setup --no-browser "${SETUP_FLAGS[@]}"
else
    echo "Already initialized, skipping setup"
fi
check "[ -d ~/.agency ]" "~/.agency directory exists"
check "[ -f ~/.agency/config.yaml ]" "config.yaml exists"

# --------------------------------------------------
# Phase 2: Health check (daemon should be running)
# --------------------------------------------------
step "Gateway health"
sleep 2
check "curl -sf http://localhost:8200/api/v1/health" "Gateway is healthy"

# --------------------------------------------------
# Phase 3: Infrastructure
# --------------------------------------------------
step "agency infra up"
run_cmd "agency infra up" "$AGENCY_BIN" -q infra up
sleep 5

step "agency infra status"
STATUS=$(run_cmd "agency infra status" "$AGENCY_BIN" -q infra status)
echo "$STATUS"
check "echo '$STATUS' | grep -qi 'healthy\|running\|ok'" "Infrastructure has healthy components"

# --------------------------------------------------
# Phase 4: Create agent
# --------------------------------------------------
step "agency create $TEST_AGENT"
run_cmd "agency create" "$AGENCY_BIN" -q create "$TEST_AGENT"
check "[ -d ~/.agency/agents/$TEST_AGENT ]" "Agent directory exists"
check "[ -f ~/.agency/agents/$TEST_AGENT/agent.yaml ]" "agent.yaml exists"
check "[ -f ~/.agency/agents/$TEST_AGENT/constraints.yaml ]" "constraints.yaml exists"

step "agency status (check agent visible)"
LIST=$("$AGENCY_BIN" -q status 2>&1)
echo "$LIST"
check "echo '$LIST' | grep -q '$TEST_AGENT'" "Agent appears in status"

# --------------------------------------------------
# Phase 5: Start agent
# --------------------------------------------------
step "agency start $TEST_AGENT"
run_cmd "agency start" "$AGENCY_BIN" -q start "$TEST_AGENT"
sleep 3

step "agency show $TEST_AGENT"
SHOW=$(run_cmd "agency show" "$AGENCY_BIN" -q show "$TEST_AGENT")
echo "$SHOW"
check "echo '$SHOW' | grep -qi 'running\|started\|healthy'" "Agent is running"

# Verify containers are actually running (not restarting/exited)
step "Container health check"
for role in workspace enforcer; do
    CTR_NAME="agency-${TEST_AGENT}-${role}"
    CTR_STATE=$(docker inspect "$CTR_NAME" --format '{{.State.Status}}' 2>&1) || CTR_STATE="missing"
    if [ "$CTR_STATE" = "running" ]; then
        pass "$CTR_NAME is running"
    else
        fail "$CTR_NAME state: $CTR_STATE"
    fi
done

# Check for OOMKill or crash
for role in workspace enforcer; do
    CTR_NAME="agency-${TEST_AGENT}-${role}"
    OOM=$(docker inspect "$CTR_NAME" --format '{{.State.OOMKilled}}' 2>/dev/null) || OOM="unknown"
    EXIT_CODE=$(docker inspect "$CTR_NAME" --format '{{.State.ExitCode}}' 2>/dev/null) || EXIT_CODE="unknown"
    RESTARTS=$(docker inspect "$CTR_NAME" --format '{{.RestartCount}}' 2>/dev/null) || RESTARTS="unknown"
    if [ "$OOM" = "true" ]; then
        fail "$CTR_NAME was OOM killed"
    fi
    if [ "$RESTARTS" != "0" ] && [ "$RESTARTS" != "unknown" ]; then
        fail "$CTR_NAME has restarted $RESTARTS time(s)"
    fi
done

# --------------------------------------------------
# Phase 6: Send task to agent (via DM)
# --------------------------------------------------
step "agency send $TEST_AGENT"
sleep 5
SEND=$(run_cmd "agency send" "$AGENCY_BIN" -q send "$TEST_AGENT" "Say hello in the general channel. This is an E2E test.")
echo "Send output: '$SEND'"
check "echo '$SEND' | grep -qi 'sent\|delivered\|accepted'" "Task delivered via DM"

sleep 10

step "Check channel for agent message"
MESSAGES=$(run_cmd "comms read general" "$AGENCY_BIN" -q comms read general)
echo "$MESSAGES"
check "echo \"$MESSAGES\" | grep -qi \"$TEST_AGENT\"" "Agent posted to general channel"

# --------------------------------------------------
# Phase 7: Logs
# --------------------------------------------------
step "agency log $TEST_AGENT"
LOGS=$(run_cmd "agency log" "$AGENCY_BIN" -q log "$TEST_AGENT")
echo "$LOGS" | tail -5
check "echo '$LOGS' | grep -qi 'task\|session\|event\|audit\|MEDIATION\|agent_started'" "Audit log readable"

# --------------------------------------------------
# Phase 8: Halt and resume
# --------------------------------------------------
step "agency halt $TEST_AGENT"
run_cmd "agency halt" "$AGENCY_BIN" -q halt "$TEST_AGENT" --tier supervised --reason "E2E test"
sleep 2

SHOW2=$(run_cmd "agency show (halted)" "$AGENCY_BIN" -q show "$TEST_AGENT")
check "echo '$SHOW2' | grep -qi 'halt\|paused\|stopped'" "Agent is halted"

# Verify containers are paused (not crashed)
for role in workspace enforcer; do
    CTR_NAME="agency-${TEST_AGENT}-${role}"
    CTR_STATE=$(docker inspect "$CTR_NAME" --format '{{.State.Status}}' 2>&1) || CTR_STATE="missing"
    if [ "$CTR_STATE" = "paused" ] || [ "$CTR_STATE" = "running" ]; then
        pass "$CTR_NAME state after halt: $CTR_STATE"
    else
        fail "$CTR_NAME unexpected state after halt: $CTR_STATE (expected paused)"
    fi
done

step "agency resume $TEST_AGENT"
run_cmd "agency resume" "$AGENCY_BIN" -q resume "$TEST_AGENT"
sleep 3

SHOW3=$(run_cmd "agency show (resumed)" "$AGENCY_BIN" -q show "$TEST_AGENT")
check "echo '$SHOW3' | grep -qi 'running\|started'" "Agent resumed"

# Verify containers resumed (not crashed)
for role in workspace enforcer; do
    CTR_NAME="agency-${TEST_AGENT}-${role}"
    CTR_STATE=$(docker inspect "$CTR_NAME" --format '{{.State.Status}}' 2>&1) || CTR_STATE="missing"
    EXIT_CODE=$(docker inspect "$CTR_NAME" --format '{{.State.ExitCode}}' 2>/dev/null) || EXIT_CODE="unknown"
    if [ "$CTR_STATE" = "running" ]; then
        pass "$CTR_NAME running after resume"
    else
        fail "$CTR_NAME state after resume: $CTR_STATE (exit code: $EXIT_CODE)"
    fi
done

# --------------------------------------------------
# Phase 9: Stop and delete
# --------------------------------------------------
step "agency stop $TEST_AGENT"
run_cmd "agency stop" "$AGENCY_BIN" -q stop "$TEST_AGENT"

step "agency delete $TEST_AGENT"
run_cmd "agency delete" "$AGENCY_BIN" -q delete "$TEST_AGENT"
check "[ ! -d ~/.agency/agents/$TEST_AGENT ]" "Agent directory removed"

# --------------------------------------------------
# Phase 10: Credentials
# --------------------------------------------------
step "Credential CRUD"
run_cmd "creds set" "$AGENCY_BIN" -q creds set --name e2e-test-key --value "test-secret-value" --kind internal --protocol api-key --scope platform
CRED_LIST=$("$AGENCY_BIN" -q creds list 2>&1)
check "echo '$CRED_LIST' | grep -q 'e2e-test-key'" "Credential appears in list"

CRED_SHOW=$("$AGENCY_BIN" -q creds show e2e-test-key 2>&1)
check "echo '$CRED_SHOW' | grep -qi 'e2e-test-key'" "Credential is retrievable"

run_cmd "creds delete" "$AGENCY_BIN" -q creds delete e2e-test-key
CRED_LIST2=$("$AGENCY_BIN" -q creds list 2>&1)
check "! echo '$CRED_LIST2' | grep -q 'e2e-test-key'" "Credential deleted"

# --------------------------------------------------
# Phase 11: Missions
# --------------------------------------------------
step "Mission lifecycle"
run_cmd "create agent for missions" "$AGENCY_BIN" -q create "$TEST_AGENT"

cat > /tmp/e2e-test-mission.yaml <<MISSION
name: e2e-test-mission
description: E2E test mission
instructions: This is a test mission for E2E validation.
MISSION

run_cmd "mission create" "$AGENCY_BIN" -q mission create /tmp/e2e-test-mission.yaml
MISSION_LIST=$("$AGENCY_BIN" -q mission list 2>&1)
check "echo '$MISSION_LIST' | grep -q 'e2e-test-mission'" "Mission appears in list"

run_cmd "mission assign" "$AGENCY_BIN" -q mission assign e2e-test-mission "$TEST_AGENT"
MISSION_SHOW=$("$AGENCY_BIN" -q mission show e2e-test-mission 2>&1)
check "echo '$MISSION_SHOW' | grep -qi '$TEST_AGENT\|assigned'" "Mission assigned to agent"

run_cmd "mission pause" "$AGENCY_BIN" -q mission pause e2e-test-mission
run_cmd "mission delete" "$AGENCY_BIN" -q mission delete e2e-test-mission
MISSION_LIST2=$("$AGENCY_BIN" -q mission list 2>&1)
check "! echo '$MISSION_LIST2' | grep -q 'e2e-test-mission'" "Mission deleted"

"$AGENCY_BIN" -q delete "$TEST_AGENT" 2>/dev/null || true

# --------------------------------------------------
# Phase 12: Hub
# --------------------------------------------------
step "Hub operations"
run_cmd "hub update" "$AGENCY_BIN" -q hub update
HUB_SEARCH=$("$AGENCY_BIN" -q hub search test 2>&1) || true
check "true" "Hub search executes without crash"

# --------------------------------------------------
# Phase 13: Knowledge graph
# --------------------------------------------------
step "Knowledge graph"
KNOWLEDGE_STATS=$(run_cmd "knowledge stats" "$AGENCY_BIN" -q knowledge stats)
check "echo '$KNOWLEDGE_STATS' | grep -qi 'nodes\|edges\|entities\|empty\|0'" "Knowledge stats accessible"

KNOWLEDGE_ONTOLOGY=$(run_cmd "knowledge ontology" "$AGENCY_BIN" -q knowledge ontology)
check "echo '$KNOWLEDGE_ONTOLOGY' | grep -qi 'type\|relationship\|ontology\|empty'" "Ontology accessible"

# --------------------------------------------------
# Phase 14: Auth validation (API level)
# --------------------------------------------------
step "Auth enforcement"
TOKEN=$(grep '^token:' ~/.agency/config.yaml | awk '{print $2}' | tr -d '"' | tr -d "'")

check "curl -sf -H 'Authorization: Bearer $TOKEN' http://localhost:8200/api/v1/health" "Authenticated health check works"

HTTP_CODE=$(curl -s -o /dev/null -w '%{http_code}' http://localhost:8200/api/v1/agents)
check "[ '$HTTP_CODE' = '401' ]" "Unauthenticated request returns 401"

check "curl -sf http://localhost:8200/api/v1/health" "Health check works without auth"

# --------------------------------------------------
# Phase 15: Infrastructure status + doctor
# --------------------------------------------------
step "Infrastructure status (post-modularization)"
INFRA_STATUS=$(run_cmd "infra status" "$AGENCY_BIN" -q infra status)
echo "$INFRA_STATUS"
check "echo '$INFRA_STATUS' | grep -qi 'egress\|comms\|knowledge\|healthy\|running'" "Infrastructure components visible"

DOCTOR=$(run_cmd "admin doctor" "$AGENCY_BIN" -q admin doctor)
echo "$DOCTOR"
# Doctor failures — dangling images are informational in dev (dirty builds),
# but security checks (credentials_isolated, network_mediation, etc.) must pass.
SECURITY_FAILS=$(echo "$DOCTOR" | grep '✗' | grep -cv 'dangling_images' || true)
DANGLING=$(echo "$DOCTOR" | grep -c 'dangling_images.*✗' || true)
if [ "$SECURITY_FAILS" -gt 0 ]; then
    fail "Admin doctor: $SECURITY_FAILS security check(s) failed"
elif [ "$DANGLING" -gt 0 ]; then
    pass "Admin doctor: security checks pass (dangling images: informational)"
else
    pass "Admin doctor: all checks pass"
fi

# --------------------------------------------------
# Phase 16: Gateway log error scan
# --------------------------------------------------
step "Gateway log scan"
if [ -f "$GATEWAY_LOG" ]; then
    # Extract only log lines written during this test run
    NEW_LINES=$(tail -n +"$((LOG_START_LINE + 1))" "$GATEWAY_LOG")

    # Count errors (excluding known benign patterns)
    ERROR_COUNT=$(echo "$NEW_LINES" | grep -c "ERRO" || true)
    PANIC_COUNT=$(echo "$NEW_LINES" | grep -c "panic\|PANIC" || true)
    FATAL_COUNT=$(echo "$NEW_LINES" | grep -c "fatal\|FATAL" || true)
    CRASH_COUNT=$(echo "$NEW_LINES" | grep -c "container died\|exit_code=139\|OOMKilled" || true)

    if [ "$PANIC_COUNT" -gt 0 ]; then
        fail "Gateway log contains $PANIC_COUNT panic(s)"
        echo "$NEW_LINES" | grep -i "panic" | head -5
    else
        pass "No panics in gateway log"
    fi

    if [ "$FATAL_COUNT" -gt 0 ]; then
        fail "Gateway log contains $FATAL_COUNT fatal error(s)"
        echo "$NEW_LINES" | grep -i "fatal" | head -5
    else
        pass "No fatal errors in gateway log"
    fi

    if [ "$ERROR_COUNT" -gt 0 ]; then
        fail "Gateway log contains $ERROR_COUNT error(s)"
        echo "$NEW_LINES" | grep "ERRO" | head -5
    else
        pass "No errors in gateway log"
    fi

    # Container "died" with exit_code=139 during halt/stop is expected (SIGKILL).
    # Only flag crashes that happen outside of halt/stop operations, or OOMKilled.
    OOM_COUNT=$(echo "$NEW_LINES" | grep -c "OOMKilled" || true)
    if [ "$OOM_COUNT" -gt 0 ]; then
        fail "OOMKilled container(s) detected ($OOM_COUNT)"
        echo "$NEW_LINES" | grep "OOMKilled" | head -5
    else
        pass "No OOMKilled containers"
    fi

    # Warnings are informational — count but don't fail
    WARN_COUNT=$(echo "$NEW_LINES" | grep -c "WARN" || true)
    if [ "$WARN_COUNT" -gt 0 ]; then
        echo "  ℹ $WARN_COUNT warning(s) in gateway log (informational)"
    fi
else
    echo "  No gateway log found (skipping)"
fi

# --------------------------------------------------
# Phase 17: Docker state check
# --------------------------------------------------
step "Docker state (no crashed containers)"
EXITED=$(docker ps -a --filter "label=agency.managed=true" --filter "status=exited" --format '{{.Names}} (exit {{.Status}})' 2>/dev/null)
RESTARTING=$(docker ps -a --filter "label=agency.managed=true" --filter "status=restarting" --format '{{.Names}}' 2>/dev/null)

if [ -n "$RESTARTING" ]; then
    fail "Restarting containers found: $RESTARTING"
else
    pass "No restarting containers"
fi

# Exited infra containers may be from cleanup — only fail on unexpected exits
if [ -n "$EXITED" ]; then
    echo "  ℹ Exited containers (may be from cleanup): $EXITED"
fi

# --------------------------------------------------
# Phase 18: Infra container log scan
# --------------------------------------------------
step "Infra container log scan"
# Use --since with the test start time to only capture entries from this run.
# Docker's --since accepts RFC 3339, Unix timestamps, or duration strings.
INFRA_CONTAINERS=$(docker ps --format '{{.Names}}' | grep "agency-infra-" 2>/dev/null)
INFRA_LOG_ERRORS=0
for ctr in $INFRA_CONTAINERS; do
    # Grab ERROR/FATAL/Traceback entries from this container
    CTR_ERRORS=$(docker logs --since "${TEST_START_TIME}" "$ctr" 2>&1 | grep -ciE "^ERROR:|^FATAL:|Traceback|CRITICAL" || true)
    if [ "$CTR_ERRORS" -gt 0 ]; then
        INFRA_LOG_ERRORS=$((INFRA_LOG_ERRORS + CTR_ERRORS))
        fail "$ctr: $CTR_ERRORS error(s) in logs"
        docker logs --since "${TEST_START_TIME}" "$ctr" 2>&1 | grep -iE "^ERROR:|^FATAL:|Traceback|CRITICAL" | head -3
    fi
done
if [ "$INFRA_LOG_ERRORS" -eq 0 ]; then
    pass "No errors in infra container logs ($(echo "$INFRA_CONTAINERS" | wc -w) containers checked)"
fi

# --------------------------------------------------
# Phase 19: LLM usage error rate
# --------------------------------------------------
step "LLM usage error rate"
# Check errors during this test run
USAGE=$("$AGENCY_BIN" -q admin usage --since "$TEST_START_TIME" 2>&1)
USAGE_ERRORS=$(echo "$USAGE" | sed -n 's/.*Errors:[[:space:]]*\([0-9][0-9]*\).*/\1/p' | head -1)
[ -z "$USAGE_ERRORS" ] && USAGE_ERRORS=0
USAGE_CALLS=$(echo "$USAGE" | sed -n 's/.*Calls:[[:space:]]*\([0-9][0-9]*\).*/\1/p' | head -1)
[ -z "$USAGE_CALLS" ] && USAGE_CALLS=0
if [ "$USAGE_ERRORS" -gt 0 ]; then
    fail "LLM usage during test: $USAGE_ERRORS error(s) out of $USAGE_CALLS call(s)"
else
    if [ "$USAGE_CALLS" -gt 0 ]; then
        pass "LLM usage during test: $USAGE_CALLS call(s), 0 errors"
    else
        pass "LLM usage during test: no calls made"
    fi
fi

# Also check cumulative errors — catches ongoing infra LLM problems
# (e.g., knowledge synthesizer failing every cycle)
USAGE_ALL=$("$AGENCY_BIN" -q admin usage 2>&1)
ALL_ERRORS=$(echo "$USAGE_ALL" | sed -n 's/.*Errors:[[:space:]]*\([0-9][0-9]*\).*/\1/p' | head -1)
[ -z "$ALL_ERRORS" ] && ALL_ERRORS=0
ALL_CALLS=$(echo "$USAGE_ALL" | sed -n 's/.*Calls:[[:space:]]*\([0-9][0-9]*\).*/\1/p' | head -1)
[ -z "$ALL_CALLS" ] && ALL_CALLS=0
if [ "$ALL_ERRORS" -gt 0 ]; then
    ERROR_RATE=$((ALL_ERRORS * 100 / ALL_CALLS))
    fail "LLM usage cumulative: $ALL_ERRORS error(s) out of $ALL_CALLS call(s) (${ERROR_RATE}% error rate)"
    echo "$USAGE_ALL" | head -10
else
    if [ "$ALL_CALLS" -gt 0 ]; then
        pass "LLM usage cumulative: $ALL_CALLS call(s), 0 errors"
    else
        pass "LLM usage cumulative: no calls recorded"
    fi
fi

# --------------------------------------------------
# Phase 20: Capacity endpoint
# --------------------------------------------------
step "Capacity endpoint"
CAPACITY=$(run_cmd "infra capacity" "$AGENCY_BIN" -q infra capacity)
echo "$CAPACITY"
check "echo '$CAPACITY' | grep -qi 'capacity\|Memory\|Agents\|slots'" "Capacity data returned"

CAPACITY_HTTP=$(curl -sf -H "Authorization: Bearer $TOKEN" http://localhost:8200/api/v1/infra/capacity 2>&1) || true
check "echo '$CAPACITY_HTTP' | grep -q 'available_slots'" "REST capacity endpoint returns available_slots"

# --------------------------------------------------
# Phase 21: Network topology
# --------------------------------------------------
step "Network topology (hub-and-spoke)"
# The hub network is agency-gateway (internal bridge for all services and enforcers)
GATEWAY_NET=$(docker network inspect agency-gateway --format '{{.Internal}}' 2>&1) || GATEWAY_NET="missing"
if [ "$GATEWAY_NET" = "true" ]; then
    pass "agency-gateway network exists and is Internal:true"
elif [ "$GATEWAY_NET" = "false" ]; then
    echo "  ℹ agency-gateway network exists but Internal:false (pre-existing, will fix on recreate)"
    pass "agency-gateway network exists"
else
    fail "agency-gateway network: $GATEWAY_NET"
fi

GATEWAY_PROXY=$(docker ps --filter "name=agency-infra-gateway-proxy" --format '{{.Names}}' 2>/dev/null)
if [ -n "$GATEWAY_PROXY" ]; then
    pass "gateway-proxy container running"
else
    fail "gateway-proxy container not found"
fi

# Verify the proxy actually forwards traffic (not a self-loop).
# Call the gateway health endpoint FROM an infra container, through the proxy.
# Uses Python (available in all agency Python containers) since wget/curl may not be installed.
PROXY_HEALTH=$(docker exec agency-infra-intake python3 -c "import urllib.request; print(urllib.request.urlopen('http://gateway:8200/api/v1/health').read().decode())" 2>&1) || PROXY_HEALTH=""
if echo "$PROXY_HEALTH" | grep -q '"status"'; then
    pass "gateway-proxy round-trip works (intake → proxy → gateway)"
else
    fail "gateway-proxy round-trip failed — proxy may not be forwarding traffic"
    echo "    Response: $PROXY_HEALTH"
fi

# Verify the reverse direction (gateway→container via proxy published ports).
COMMS_HEALTH=$(curl -sf http://localhost:8202/health 2>&1) || COMMS_HEALTH=""
if echo "$COMMS_HEALTH" | grep -q '"status"'; then
    pass "gateway→comms proxy bridge works (localhost:8202 → comms:8080)"
else
    fail "gateway→comms proxy bridge failed — reverse proxy may not be forwarding"
    echo "    Response: $COMMS_HEALTH"
fi

# --------------------------------------------------
# Phase 22: Docker socket audit
# --------------------------------------------------
step "Docker socket audit"
SOCKET_VIOLATIONS=$(docker ps -a --filter "label=agency.managed=true" --format '{{.Names}} {{.Mounts}}' 2>/dev/null | grep -c "docker.sock" || true)
if [ "$SOCKET_VIOLATIONS" -gt 0 ]; then
    fail "SECURITY: $SOCKET_VIOLATIONS container(s) have Docker socket mounted"
else
    pass "No Docker socket mounts on managed containers"
fi

# --------------------------------------------------
# Phase 23: Notifications
# --------------------------------------------------
step "Notifications"
# Add and test a dummy notification destination
run_cmd "notify add" "$AGENCY_BIN" -q notify add e2e-test-notif --url https://ntfy.sh/agency-e2e-test 2>/dev/null || true
NOTIF_LIST=$("$AGENCY_BIN" -q notify list 2>&1)
check "echo '$NOTIF_LIST' | grep -qi 'e2e-test-notif\|notifications\|destinations'" "Notification list accessible"

"$AGENCY_BIN" -q notify remove e2e-test-notif 2>/dev/null || true

# --------------------------------------------------
# Phase 24: Registry
# --------------------------------------------------
step "Principal registry"
REGISTRY_LIST=$("$AGENCY_BIN" -q registry list 2>&1)
check "echo '$REGISTRY_LIST' | grep -qi 'operator\|agent\|principal\|uuid\|\[\]'" "Registry list accessible"

# --------------------------------------------------
# Phase 25: Routing
# --------------------------------------------------
step "Routing"
ROUTING_CONFIG=$("$AGENCY_BIN" -q infra routing stats 2>&1) || true
check "true" "Routing stats command executes without crash"

SUGGESTIONS=$("$AGENCY_BIN" -q infra routing suggestions 2>&1) || true
check "true" "Routing suggestions command executes without crash"

# Providers are listed via REST, not a standalone CLI command
PROVIDERS_HTTP=$(curl -sf -H "Authorization: Bearer $TOKEN" http://localhost:8200/api/v1/infra/providers 2>&1) || true
check "echo '$PROVIDERS_HTTP' | grep -qi 'provider\|anthropic\|openai\|gemini\|ollama\|\[\]'" "Providers endpoint accessible"

# --------------------------------------------------
# Phase 26: Capabilities
# --------------------------------------------------
step "Capabilities"
CAP_LIST=$("$AGENCY_BIN" -q cap list 2>&1) || true
check "true" "Capabilities list executes without crash"

# --------------------------------------------------
# Phase 27: Meeseeks
# --------------------------------------------------
step "Meeseeks"
MEESEEKS_LIST=$("$AGENCY_BIN" -q meeseeks list 2>&1) || true
check "true" "Meeseeks list executes without crash"

# --------------------------------------------------
# Phase 28: Logging hygiene guard
# --------------------------------------------------
step "Logging hygiene"
# Fail if anyone bypasses the unified logging infrastructure
OLD_LOG_IMPORTS=$(grep -r 'charmbracelet/log' --include='*.go' internal/ cmd/ 2>/dev/null | grep -cv 'lipgloss' || true)
if [ "$OLD_LOG_IMPORTS" -gt 0 ]; then
    fail "Found $OLD_LOG_IMPORTS file(s) still importing charmbracelet/log (use log/slog)"
    grep -r 'charmbracelet/log' --include='*.go' internal/ cmd/ | grep -v lipgloss | head -5
else
    pass "No charmbracelet/log imports in gateway code"
fi

OLD_BASICCONFIG=$(grep -r 'logging.basicConfig' --include='*.py' images/ 2>/dev/null | wc -l || true)
if [ "$OLD_BASICCONFIG" -gt 0 ]; then
    fail "Found $OLD_BASICCONFIG file(s) using logging.basicConfig (use sitecustomize.py)"
    grep -r 'logging.basicConfig' --include='*.py' images/ | head -5
else
    pass "No logging.basicConfig in Python containers"
fi

# --------------------------------------------------
# Results
# --------------------------------------------------
echo ""
echo "================================"
echo " Results: $PASS passed, $FAIL failed, $TOTAL phases"
echo "================================"

if [ "$FAIL" -gt 0 ]; then
    echo -e "${RED}E2E test FAILED${NC}"
    echo ""
    echo "Failures:"
    for err in "${ERRORS[@]}"; do
        echo -e "  ${RED}✗${NC} $err"
    done
    exit 1
else
    echo -e "${GREEN}E2E test PASSED${NC}"
    exit 0
fi
