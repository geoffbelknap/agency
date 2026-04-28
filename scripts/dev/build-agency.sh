#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"

exec "$ROOT_DIR/scripts/dev/go-with-cache.sh" build -o agency ./cmd/gateway
