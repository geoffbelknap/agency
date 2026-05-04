#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
REQ_FILE="$ROOT_DIR/scripts/release/homebrew-python-requirements.txt"
OUT_DIR="$ROOT_DIR/.release/homebrew-python-wheelhouses"
PYTHON_BIN="${PYTHON_BIN:-python3}"
ENV_FILE="$OUT_DIR/env.sh"

rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR"
: > "$ENV_FILE"

build_wheelhouse() {
  local os="$1"
  local arch="$2"
  local platforms="$3"
  local abi="$4"
  local dest="$OUT_DIR/${os}-${arch}"
  local archive="$OUT_DIR/agency-python-wheelhouse-${os}-${arch}.tar.gz"
  local target_req="$OUT_DIR/requirements-${os}-${arch}.txt"
  local platform_args=()
  local platform

  mkdir -p "$dest"
  awk -v target_os="$os" '
    /^mitmproxy-macos==/ {
      if (target_os == "darwin") {
        sub(/[[:space:]]*;.*/, "")
        print
      }
      next
    }
    /^mitmproxy-linux==/ {
      if (target_os == "linux") {
        sub(/[[:space:]]*;.*/, "")
        print
      }
      next
    }
    { print }
  ' "$REQ_FILE" > "$target_req"

  case "$os" in
    darwin)
      grep -q '^mitmproxy-macos==' "$target_req"
      ! grep -q '^mitmproxy-linux==' "$target_req"
      ;;
    linux)
      grep -q '^mitmproxy-linux==' "$target_req"
      ! grep -q '^mitmproxy-macos==' "$target_req"
      ;;
  esac

  IFS=',' read -r -a platform_args_raw <<< "$platforms"
  for platform in "${platform_args_raw[@]}"; do
    platform_args+=(--platform "$platform")
  done

  "$PYTHON_BIN" -m pip download \
    --dest "$dest" \
    --requirement "$target_req" \
    --only-binary=:all: \
    --no-deps \
    --implementation cp \
    --python-version 314 \
    --abi "$abi" \
    --abi abi3 \
    --abi none \
    "${platform_args[@]}"

  tar -C "$dest" -czf "$archive" .
  shasum -a 256 "$archive" | awk "{print \$1}" > "$archive.sha256"

  local output_name
  output_name="$(printf 'AGENCY_PYTHON_WHEELHOUSE_%s_%s_SHA256' "$os" "$arch" | tr '[:lower:]-' '[:upper:]_')"
  if [ -n "${GITHUB_OUTPUT:-}" ]; then
    printf '%s=%s\n' "$output_name" "$(cat "$archive.sha256")" >> "$GITHUB_OUTPUT"
  fi
  printf 'export %s=%s\n' "$output_name" "$(cat "$archive.sha256")" >> "$ENV_FILE"
  printf '%s=%s\n' "$output_name" "$(cat "$archive.sha256")"
}

build_wheelhouse darwin arm64 macosx_11_0_arm64 cp314
build_wheelhouse darwin amd64 macosx_10_15_x86_64 cp314
build_wheelhouse linux amd64 manylinux_2_28_x86_64,manylinux_2_26_x86_64,manylinux_2_17_x86_64,manylinux2014_x86_64 cp314
build_wheelhouse linux arm64 manylinux_2_28_aarch64,manylinux_2_26_aarch64,manylinux_2_17_aarch64,manylinux2014_aarch64 cp314
