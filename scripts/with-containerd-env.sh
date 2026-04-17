#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -eq 0 ]; then
  cat <<'EOF' >&2
Usage: ./scripts/with-containerd-env.sh <command> [args...]

Exports the standard rootless containerd environment for this host, then execs
the requested command.
EOF
  exit 1
fi

export CONTAINERD_HOST="${CONTAINERD_HOST:-unix:///run/containerd/containerd.sock}"
export CONTAINERD_NAMESPACE="${CONTAINERD_NAMESPACE:-default}"

exec "$@"
