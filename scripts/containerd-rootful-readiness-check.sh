#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

export CONTAINERD_HOST="${CONTAINERD_HOST:-unix:///run/containerd/containerd.sock}"
export AGENCY_CONTAINERD_EXPECTED_MODE="${AGENCY_CONTAINERD_EXPECTED_MODE:-rootful}"

exec "$SCRIPT_DIR/containerd-readiness-check.sh" "$@"
