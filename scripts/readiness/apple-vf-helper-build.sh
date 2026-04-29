#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
HELPER_DIR="$ROOT_DIR/tools/apple-vf-helper"

if [ "$(uname -s)" != "Darwin" ]; then
  echo "apple-vf helper build requires macOS" >&2
  exit 1
fi

cd "$HELPER_DIR"
swift build -c release
".build/release/agency-apple-vf-helper" health
