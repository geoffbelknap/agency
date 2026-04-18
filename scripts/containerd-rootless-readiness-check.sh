#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

export AGENCY_CONTAINERD_EXPECTED_MODE="${AGENCY_CONTAINERD_EXPECTED_MODE:-rootless}"

exec "$SCRIPT_DIR/with-containerd-env.sh" \
  "$SCRIPT_DIR/containerd-readiness-check.sh" \
  "$@"
