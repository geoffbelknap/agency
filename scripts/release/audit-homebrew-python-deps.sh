#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP_PARENT="${TMPDIR:-/tmp}"
VENV_DIR="${VENV_DIR:-$TMP_PARENT/agency-homebrew-python-audit-venv}"

detect_target() {
  local os
  local arch

  case "$(uname -s)" in
    Darwin) os="darwin" ;;
    Linux) os="linux" ;;
    *) echo "unsupported OS: $(uname -s)" >&2; return 1 ;;
  esac

  case "$(uname -m)" in
    arm64 | aarch64) arch="arm64" ;;
    x86_64 | amd64) arch="amd64" ;;
    *) echo "unsupported architecture: $(uname -m)" >&2; return 1 ;;
  esac

  printf '%s-%s\n' "$os" "$arch"
}

TARGET="${TARGET:-$(detect_target)}"
TARGET_OS="${TARGET%%-*}"
WHEELHOUSE_DIR="${WHEELHOUSE_DIR:-$ROOT_DIR/.release/homebrew-python-wheelhouses/$TARGET}"
REQ_FILE="${REQ_FILE:-$ROOT_DIR/.release/homebrew-python-wheelhouses/requirements-$TARGET.txt}"

if [ ! -d "$WHEELHOUSE_DIR" ] || [ ! -f "$REQ_FILE" ]; then
  "$ROOT_DIR/scripts/release/build-homebrew-python-wheelhouses.sh" >/dev/null
fi

rm -rf "$VENV_DIR"
python3 -m venv "$VENV_DIR"
"$VENV_DIR/bin/python" -m pip install \
  --no-index \
  --find-links "$WHEELHOUSE_DIR" \
  --requirement "$REQ_FILE" >/dev/null

"$VENV_DIR/bin/python" -m pip check
"$VENV_DIR/bin/python" "$ROOT_DIR/scripts/release/audit-homebrew-python-deps.py" \
  --requirements "$REQ_FILE" \
  --source-root "$ROOT_DIR/services" \
  --target-os "$TARGET_OS"
