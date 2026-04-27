#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PACKAGE_DIR="$ROOT/tools/apple-container-wait-helper"
OUTPUT="${1:-/tmp/agency-apple-container-wait-helper}"

swift build --package-path "$PACKAGE_DIR"
install -m 0755 "$PACKAGE_DIR/.build/debug/agency-apple-container-wait-helper" "$OUTPUT"
printf '%s\n' "$OUTPUT"
