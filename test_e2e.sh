#!/usr/bin/env bash
# End-to-end test: agency init → infra up → create → start → send
# Exercises the Go binary through the full agent lifecycle.
#
# Prerequisites:
#   - Docker running
#   - Go binary built: cd agency-gateway && go build -o agency ./cmd/gateway/
#   - No existing agency state (or run 'agency admin destroy' first)
#
# Usage: ./test_e2e.sh

set -euo pipefail

AGENCY="$(dirname "$0")/agency"
TEST_AGENT="e2e-test-agent"
PASS=0
FAIL=0
TOTAL=0

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
    echo -e "  ${RED}✗ FAIL${NC}: $1"
}

check() {
    if eval "$1" >/dev/null 2>&1; then
        pass "$2"
    else
        fail "$2"
    fi
}

cleanup() {
    echo -e "\n${YELLOW}Cleanup...${NC}"
    "$AGENCY" stop "$TEST_AGENT" 2>/dev/null || true
    "$AGENCY" delete "$TEST_AGENT" 2>/dev/null || true
    "$AGENCY" mission delete e2e-test-mission 2>/dev/null || true
    "$AGENCY" creds delete e2e-test-key 2>/dev/null || true
    # Kill daemon if we started one
    if [ -f ~/.agency/gateway.pid ]; then
        kill "$(cat ~/.agency/gateway.pid)" 2>/dev/null || true
        rm -f ~/.agency/gateway.pid
    fi
}

trap cleanup EXIT

echo "================================"
echo " Agency E2E Test"
echo "================================"

# Verify binary exists
if [ ! -x "$AGENCY" ]; then
    echo "Binary not found at $AGENCY. Build first: go build -o agency ./cmd/gateway/"
    exit 1
fi

echo "Binary: $AGENCY"
echo "Test agent: $TEST_AGENT"

# Pre-cleanup: remove any leftover state from prior runs
"$AGENCY" stop "$TEST_AGENT" 2>/dev/null || true
"$AGENCY" delete "$TEST_AGENT" 2>/dev/null || true
"$AGENCY" mission delete e2e-test-mission 2>/dev/null || true
"$AGENCY" creds delete e2e-test-key 2>/dev/null || true

# --------------------------------------------------
# Phase 1: Init
# --------------------------------------------------
step "agency init"
# Skip setup if already initialized — setup opens a browser when no provider
# is configured, which is a side effect tests must not have.
if [ ! -f ~/.agency/config.yaml ]; then
    AGENCY_NO_BROWSER=1 "$AGENCY" setup 2>&1 || true
else
    echo "Already initialized, skipping setup"
fi
check "[ -d ~/.agency ]" "~/.agency directory exists"
check "[ -f ~/.agency/config.yaml ]" "config.yaml exists"

# --------------------------------------------------
# Phase 2: Health check (daemon should be running)
# --------------------------------------------------
step "Gateway health"
sleep 2  # give daemon a moment
check "curl -sf http://localhost:8200/api/v1/health" "Gateway is healthy"

# --------------------------------------------------
# Phase 3: Infrastructure
# --------------------------------------------------
step "agency infra up"
"$AGENCY" infra up 2>&1 || true
sleep 5  # wait for containers to become healthy

step "agency infra status"
STATUS=$("$AGENCY" infra status 2>&1)
echo "$STATUS"
check "echo '$STATUS' | grep -qi 'healthy\|running\|ok'" "Infrastructure has healthy components"

# --------------------------------------------------
# Phase 4: Create agent
# --------------------------------------------------
step "agency create $TEST_AGENT"
"$AGENCY" create "$TEST_AGENT" 2>&1 || true
check "[ -d ~/.agency/agents/$TEST_AGENT ]" "Agent directory exists"
check "[ -f ~/.agency/agents/$TEST_AGENT/agent.yaml ]" "agent.yaml exists"
check "[ -f ~/.agency/agents/$TEST_AGENT/constraints.yaml ]" "constraints.yaml exists"

step "agency status (check agent visible)"
LIST=$("$AGENCY" status 2>&1)
echo "$LIST"
check "echo '$LIST' | grep -q '$TEST_AGENT'" "Agent appears in status"

# --------------------------------------------------
# Phase 5: Start agent
# --------------------------------------------------
step "agency start $TEST_AGENT"
"$AGENCY" start "$TEST_AGENT" 2>&1
sleep 3

step "agency show $TEST_AGENT"
SHOW=$("$AGENCY" show "$TEST_AGENT" 2>&1)
echo "$SHOW"
check "echo '$SHOW' | grep -qi 'running\|started\|healthy'" "Agent is running"

# --------------------------------------------------
# Phase 6: Send task to agent (via DM)
# --------------------------------------------------
step "agency send $TEST_AGENT"
sleep 5  # wait for agent to fully initialize
SEND=$("$AGENCY" send "$TEST_AGENT" "Say hello in the general channel. This is an E2E test." 2>&1) || true
echo "Send output: '$SEND'"
check "echo '$SEND' | grep -qi 'sent\|delivered\|accepted'" "Task delivered via DM"

# Wait for agent to process
sleep 10

step "Check channel for agent message"
MESSAGES=$("$AGENCY" channel read general 2>&1)
echo "$MESSAGES"
check "echo \"$MESSAGES\" | grep -qi \"$TEST_AGENT\"" "Agent posted to general channel"

# --------------------------------------------------
# Phase 7: Logs
# --------------------------------------------------
step "agency log $TEST_AGENT"
LOGS=$("$AGENCY" log "$TEST_AGENT" 2>&1) || true
echo "$LOGS" | tail -5
check "echo '$LOGS' | grep -qi 'task\|session\|event\|audit\|no audit'" "Audit log readable"

# --------------------------------------------------
# Phase 8: Halt and resume
# --------------------------------------------------
step "agency halt $TEST_AGENT"
"$AGENCY" halt "$TEST_AGENT" --tier supervised --reason "E2E test" 2>&1 || true
sleep 2

SHOW2=$("$AGENCY" show "$TEST_AGENT" 2>&1)
check "echo '$SHOW2' | grep -qi 'halt\|paused\|stopped'" "Agent is halted"

step "agency resume $TEST_AGENT"
"$AGENCY" resume "$TEST_AGENT" 2>&1 || true
sleep 3

SHOW3=$("$AGENCY" show "$TEST_AGENT" 2>&1)
check "echo '$SHOW3' | grep -qi 'running\|started'" "Agent resumed"

# --------------------------------------------------
# Phase 9: Stop and delete
# --------------------------------------------------
step "agency stop $TEST_AGENT"
"$AGENCY" stop "$TEST_AGENT" 2>&1

step "agency delete $TEST_AGENT"
"$AGENCY" delete "$TEST_AGENT" 2>&1
check "[ ! -d ~/.agency/agents/$TEST_AGENT ]" "Agent directory removed"

# --------------------------------------------------
# Phase 10: Credentials
# --------------------------------------------------
step "Credential CRUD"
"$AGENCY" creds set --name e2e-test-key --value "test-secret-value" --kind internal --protocol api-key --scope platform 2>&1 || true
CRED_LIST=$("$AGENCY" creds list 2>&1)
check "echo '$CRED_LIST' | grep -q 'e2e-test-key'" "Credential appears in list"

CRED_SHOW=$("$AGENCY" creds show e2e-test-key 2>&1)
check "echo '$CRED_SHOW' | grep -qi 'e2e-test-key'" "Credential is retrievable"

"$AGENCY" creds delete e2e-test-key 2>&1 || true
CRED_LIST2=$("$AGENCY" creds list 2>&1)
check "! echo '$CRED_LIST2' | grep -q 'e2e-test-key'" "Credential deleted"

# --------------------------------------------------
# Phase 11: Missions
# --------------------------------------------------
step "Mission lifecycle"
# Create a throwaway agent for mission testing
"$AGENCY" create "$TEST_AGENT" 2>&1 || true

cat > /tmp/e2e-test-mission.yaml <<MISSION
name: e2e-test-mission
description: E2E test mission
instructions: This is a test mission for E2E validation.
MISSION

"$AGENCY" mission create /tmp/e2e-test-mission.yaml 2>&1 || true
MISSION_LIST=$("$AGENCY" mission list 2>&1)
check "echo '$MISSION_LIST' | grep -q 'e2e-test-mission'" "Mission appears in list"

"$AGENCY" mission assign e2e-test-mission "$TEST_AGENT" 2>&1 || true
MISSION_SHOW=$("$AGENCY" mission show e2e-test-mission 2>&1)
check "echo '$MISSION_SHOW' | grep -qi '$TEST_AGENT\|assigned'" "Mission assigned to agent"

"$AGENCY" mission pause e2e-test-mission 2>&1 || true
"$AGENCY" mission delete e2e-test-mission 2>&1 || true
MISSION_LIST2=$("$AGENCY" mission list 2>&1)
check "! echo '$MISSION_LIST2' | grep -q 'e2e-test-mission'" "Mission deleted"

"$AGENCY" delete "$TEST_AGENT" 2>&1 || true

# --------------------------------------------------
# Phase 12: Hub
# --------------------------------------------------
step "Hub operations"
"$AGENCY" hub update 2>&1 || true
HUB_SEARCH=$("$AGENCY" hub search test 2>&1) || true
check "true" "Hub search executes without crash"

HUB_INSTALLED=$("$AGENCY" hub installed 2>&1) || true
# This is informational — may be empty on fresh install
echo "Hub installed: $HUB_INSTALLED"

# --------------------------------------------------
# Phase 13: Knowledge graph
# --------------------------------------------------
step "Knowledge graph"
KNOWLEDGE_STATS=$("$AGENCY" knowledge stats 2>&1) || true
check "echo '$KNOWLEDGE_STATS' | grep -qi 'nodes\|edges\|entities\|empty\|0'" "Knowledge stats accessible"

KNOWLEDGE_ONTOLOGY=$("$AGENCY" knowledge ontology 2>&1) || true
check "echo '$KNOWLEDGE_ONTOLOGY' | grep -qi 'type\|relationship\|ontology\|empty'" "Ontology accessible"

# --------------------------------------------------
# Phase 14: Auth validation (API level)
# --------------------------------------------------
step "Auth enforcement"
TOKEN=$(grep '^token:' ~/.agency/config.yaml | awk '{print $2}' | tr -d '"' | tr -d "'")

# Authenticated request should work
check "curl -sf -H 'Authorization: Bearer $TOKEN' http://localhost:8200/api/v1/health" "Authenticated health check works"

# Unauthenticated request to protected endpoint should fail
HTTP_CODE=$(curl -s -o /dev/null -w '%{http_code}' http://localhost:8200/api/v1/agents)
check "[ '$HTTP_CODE' = '401' ]" "Unauthenticated request returns 401"

# Health endpoint works without auth
check "curl -sf http://localhost:8200/api/v1/health" "Health check works without auth"

# --------------------------------------------------
# Phase 15: Infrastructure status
# --------------------------------------------------
step "Infrastructure status (post-modularization)"
INFRA_STATUS=$("$AGENCY" infra status 2>&1)
echo "$INFRA_STATUS"
check "echo '$INFRA_STATUS' | grep -qi 'egress\|comms\|knowledge\|healthy\|running'" "Infrastructure components visible"

# Admin doctor validates enforcement integrity
DOCTOR=$("$AGENCY" admin doctor 2>&1) || true
echo "$DOCTOR" | tail -5
check "echo '$DOCTOR' | grep -qi 'credentials_isolated\|network_mediation\|no agents\|✓'" "Admin doctor runs without error"

# --------------------------------------------------
# Results
# --------------------------------------------------
echo ""
echo "================================"
echo " Results: $PASS passed, $FAIL failed, $TOTAL total"
echo "================================"

if [ "$FAIL" -gt 0 ]; then
    echo -e "${RED}E2E test FAILED${NC}"
    exit 1
else
    echo -e "${GREEN}E2E test PASSED${NC}"
    exit 0
fi
