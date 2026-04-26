#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -eq 0 ]; then
  cat <<'EOF' >&2
Usage: ./scripts/dev/with-containerd-rootful-env.sh <command> [args...]

Exports the standard rootful containerd environment for this host, then execs
the requested command.
EOF
  exit 1
fi

export CONTAINERD_HOST="${CONTAINERD_HOST:-unix:///run/containerd/containerd.sock}"
export CONTAINERD_NAMESPACE="${CONTAINERD_NAMESPACE:-default}"
export AGENCY_CONTAINERD_EXPECTED_MODE="${AGENCY_CONTAINERD_EXPECTED_MODE:-rootful}"

exec "$@"
