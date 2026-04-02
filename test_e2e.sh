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

# --------------------------------------------------
# Phase 1: Init
# --------------------------------------------------
step "agency init"
"$AGENCY" setup 2>&1 || true  # may already be initialized
check "[ -d ~/.agency ]" "~/.agency directory exists"
check "[ -f ~/.agency/config.yaml ]" "config.yaml exists"

# --------------------------------------------------
# Phase 2: Health check (daemon should be running)
# --------------------------------------------------
step "Gateway health"
sleep 2  # give daemon a moment
check "curl -sf http://localhost:18200/api/v1/health" "Gateway is healthy"

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

step "agency list"
LIST=$("$AGENCY" list 2>&1)
echo "$LIST"
check "echo '$LIST' | grep -q '$TEST_AGENT'" "Agent appears in list"

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
"$AGENCY" halt "$TEST_AGENT" --type supervised --reason "E2E test" 2>&1 || true
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
