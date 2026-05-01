#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HELPER_VERSION="${AGENCY_APPLE_VF_HELPER_VERSION:-0.1.0}"
OUT_DIR="${AGENCY_APPLE_VF_HELPERS_OUT_DIR:-$ROOT_DIR/dist/apple-vf-helpers}"
STAGE_DIR="${AGENCY_APPLE_VF_HELPERS_STAGE_DIR:-$ROOT_DIR/dist/apple-vf-helpers-stage}"
ASSET_NAME="agency-apple-vf-helpers-${HELPER_VERSION}-darwin-arm64.tar.gz"
ASSET_PATH="$OUT_DIR/$ASSET_NAME"

fail() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

if [[ "$(uname -s)" != "Darwin" ]]; then
  fail "Apple VF helper assets must be built on macOS"
fi
if [[ "$(uname -m)" != "arm64" ]]; then
  fail "Apple VF helper assets must be built on Apple silicon"
fi
[[ -n "$HELPER_VERSION" ]] || fail "Apple VF helper version is not set"

require_cmd go
require_cmd swift
require_cmd codesign
require_cmd tar
require_cmd shasum

rm -rf "$OUT_DIR" "$STAGE_DIR"
mkdir -p "$OUT_DIR" "$STAGE_DIR/bin"

"$ROOT_DIR/scripts/readiness/apple-vf-helper-build.sh"
cp "$ROOT_DIR/tools/apple-vf-helper/.build/release/agency-apple-vf-helper" "$STAGE_DIR/bin/agency-apple-vf-helper"

(cd "$ROOT_DIR/images/enforcer" && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o "$STAGE_DIR/bin/agency-enforcer-host" .)
(cd "$ROOT_DIR" && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o "$STAGE_DIR/bin/agency-vsock-http-bridge-linux-arm64" ./cmd/agency-vsock-http-bridge)

chmod 0755 "$STAGE_DIR/bin/"*
tar -C "$STAGE_DIR" -czf "$ASSET_PATH" bin
sha="$(shasum -a 256 "$ASSET_PATH" | awk '{print $1}')"
printf '%s  %s\n' "$sha" "$ASSET_NAME" >"$ASSET_PATH.sha256"

if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
  {
    printf 'asset_name=%s\n' "$ASSET_NAME"
    printf 'asset_path=%s\n' "$ASSET_PATH"
    printf 'helper_version=%s\n' "$HELPER_VERSION"
    printf 'sha256=%s\n' "$sha"
  } >>"$GITHUB_OUTPUT"
fi

printf 'asset_name=%s\n' "$ASSET_NAME"
printf 'helper_version=%s\n' "$HELPER_VERSION"
printf 'sha256=%s\n' "$sha"
