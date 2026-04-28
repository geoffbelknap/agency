#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
GO_BUILD_CACHE="${AGENCY_GO_BUILD_CACHE:-/tmp/agency-go-build-cache}"

if [ "$#" -lt 1 ]; then
  echo "Usage: ./scripts/dev/go-with-cache.sh <go-args...>"
  exit 1
fi

mkdir -p "$GO_BUILD_CACHE"
cd "$ROOT_DIR"
export GOCACHE="$GO_BUILD_CACHE"
exec go "$@"
