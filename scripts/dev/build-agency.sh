#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
GO_BUILD_CACHE="${AGENCY_GO_BUILD_CACHE:-/tmp/agency-go-build-cache}"

mkdir -p "$GO_BUILD_CACHE"
cd "$ROOT_DIR"
export GOCACHE="$GO_BUILD_CACHE"
exec make build
