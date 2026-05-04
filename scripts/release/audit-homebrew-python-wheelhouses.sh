#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT_DIR="$ROOT_DIR/.release/homebrew-python-wheelhouses"
PYTHON_BIN="${PYTHON_BIN:-python3}"

if [ ! -d "$OUT_DIR" ]; then
  "$ROOT_DIR/scripts/release/build-homebrew-python-wheelhouses.sh" >/dev/null
fi

audit_target() {
  local target="$1"
  local platforms="$2"
  local abi="$3"
  local wheelhouse="$OUT_DIR/$target"
  local req_file="$OUT_DIR/requirements-$target.txt"
  local dry_run_target="$OUT_DIR/audit-$target"
  local platform_args=()
  local platform

  [ -d "$wheelhouse" ] || {
    echo "missing wheelhouse: $wheelhouse" >&2
    return 1
  }
  [ -f "$req_file" ] || {
    echo "missing requirements file: $req_file" >&2
    return 1
  }

  rm -rf "$dry_run_target"
  mkdir -p "$dry_run_target"

  IFS=',' read -r -a platform_args_raw <<< "$platforms"
  for platform in "${platform_args_raw[@]}"; do
    platform_args+=(--platform "$platform")
  done

  "$PYTHON_BIN" -m pip install \
    --dry-run \
    --ignore-installed \
    --no-deps \
    --no-index \
    --find-links "$wheelhouse" \
    --requirement "$req_file" \
    --only-binary=:all: \
    --implementation cp \
    --python-version 314 \
    --abi "$abi" \
    --abi abi3 \
    --abi none \
    --target "$dry_run_target" \
    "${platform_args[@]}" >/dev/null

  echo "wheelhouse resolver audit passed: $target"
}

audit_target darwin-arm64 macosx_11_0_arm64 cp314
audit_target darwin-amd64 macosx_10_15_x86_64 cp314
audit_target linux-amd64 manylinux_2_28_x86_64,manylinux_2_26_x86_64,manylinux_2_17_x86_64,manylinux2014_x86_64 cp314
audit_target linux-arm64 manylinux_2_28_aarch64,manylinux_2_26_aarch64,manylinux_2_17_aarch64,manylinux2014_aarch64 cp314

"$ROOT_DIR/scripts/release/audit-homebrew-python-deps.sh"
