#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -eq 0 ]; then
  cat <<'EOF' >&2
Usage: ./scripts/dev/with-containerd-env.sh <command> [args...]

Exports the standard rootless containerd environment for this host, then execs
the requested command.
EOF
  exit 1
fi

if [ -z "${CONTAINERD_HOST:-}" ]; then
  uid="$(id -u)"
  if [ -n "${XDG_RUNTIME_DIR:-}" ] && [ -S "${XDG_RUNTIME_DIR}/containerd/containerd.sock" ]; then
    export CONTAINERD_HOST="unix://${XDG_RUNTIME_DIR}/containerd/containerd.sock"
  elif [ -S "/run/user/${uid}/containerd/containerd.sock" ]; then
    export CONTAINERD_HOST="unix:///run/user/${uid}/containerd/containerd.sock"
  else
    export CONTAINERD_HOST="unix:///run/containerd/containerd.sock"
  fi
fi
export CONTAINERD_NAMESPACE="${CONTAINERD_NAMESPACE:-default}"

exec "$@"
