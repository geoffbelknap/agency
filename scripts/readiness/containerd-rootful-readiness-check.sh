#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

exec "$SCRIPT_DIR/with-containerd-rootful-env.sh" "$SCRIPT_DIR/containerd-readiness-check.sh" "$@"
