#!/bin/sh
set -e

# Structured readiness logging — each phase emits a timestamped line
# so operators can diagnose startup delays and failures.
log_phase() {
  printf '[agency-entrypoint] %s | %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$1"
}

# Body runtime: agent loop runs via docker exec from the host.
# Container stays alive for exec access (body.py is invoked externally).

log_phase "Body runtime — waiting for exec"

# Keep container alive so body runtime exec and verification checks work.
exec tail -f /dev/null
