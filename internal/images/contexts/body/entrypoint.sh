#!/bin/sh
set -e

log_phase() {
  printf '[body-entrypoint] %s | %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$1"
}

# Copy operator-generated context files to workspace
[ -f /agency/AGENTS.md ] && cp /agency/AGENTS.md /workspace/AGENTS.md 2>/dev/null || true
[ -f /agency/FRAMEWORK.md ] && cp /agency/FRAMEWORK.md /workspace/FRAMEWORK.md 2>/dev/null || true
[ -f /agency/identity.md ] && cp /agency/identity.md /workspace/identity.md 2>/dev/null || true

log_phase "Context files copied to workspace"

# Wait for enforcer to be reachable before starting body runtime.
# Uses python instead of curl since curl is not in python:3.12-slim.
log_phase "Waiting for enforcer"
ENFORCER_READY=0
for i in $(seq 1 20); do
  if python -c "import httpx; httpx.get('http://enforcer:3128/health', timeout=2).raise_for_status()" 2>/dev/null; then
    ENFORCER_READY=1
    break
  fi
  [ "$i" -le 3 ] || log_phase "Enforcer poll ($i/20)..."
  sleep 0.5
done

if [ "$ENFORCER_READY" = "1" ]; then
  log_phase "Enforcer healthy"
else
  log_phase "WARNING: Enforcer not reachable after 10s, starting anyway"
fi

log_phase "Starting body runtime"
exec python -u /app/body.py
