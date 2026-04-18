#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -lt 2 ]; then
  cat <<'EOF' >&2
Usage: ./scripts/with-agency-home.sh <agency-home> <command> [args...]

Exports AGENCY_HOME for the requested command, then execs it.
EOF
  exit 1
fi

export AGENCY_HOME="$1"
shift

exec "$@"
