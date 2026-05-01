#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
AGENCY_HOME_DIR="${AGENCY_HOME:-$HOME/.agency}"
ARTIFACT_DIR="${AGENCY_FIRECRACKER_ARTIFACT_DIR:-$AGENCY_HOME_DIR/runtime/firecracker/artifacts}"
BUILD_DIR="${AGENCY_FIRECRACKER_BUILD_DIR:-$AGENCY_HOME_DIR/runtime/firecracker/build}"
FIRECRACKER_VERSION="${AGENCY_FIRECRACKER_VERSION:-v1.12.1}"
FIRECRACKER_ARCH="${AGENCY_FIRECRACKER_ARCH:-$(uname -m)}"
case "$FIRECRACKER_ARCH" in
  amd64) FIRECRACKER_ARCH="x86_64" ;;
  arm64) FIRECRACKER_ARCH="aarch64" ;;
esac
RELEASE_BASE_URL="${AGENCY_FIRECRACKER_RELEASE_BASE_URL:-https://github.com/firecracker-microvm/firecracker/releases/download/$FIRECRACKER_VERSION}"
TARBALL_NAME="firecracker-$FIRECRACKER_VERSION-$FIRECRACKER_ARCH.tgz"
TARBALL_URL="$RELEASE_BASE_URL/$TARBALL_NAME"
SHA_URL="$TARBALL_URL.sha256.txt"
DOWNLOAD_DIR="$BUILD_DIR/downloads/$FIRECRACKER_VERSION"
EXTRACT_DIR="$BUILD_DIR/extract/$FIRECRACKER_VERSION"
TARBALL="$DOWNLOAD_DIR/$TARBALL_NAME"
SHA_FILE="$TARBALL.sha256.txt"
RELEASE_DIR="$EXTRACT_DIR/release-$FIRECRACKER_VERSION-$FIRECRACKER_ARCH"
SOURCE_BIN="$RELEASE_DIR/firecracker-$FIRECRACKER_VERSION-$FIRECRACKER_ARCH"
DEST_DIR="$ARTIFACT_DIR/$FIRECRACKER_VERSION"
DEST_BIN="$DEST_DIR/firecracker-$FIRECRACKER_VERSION-$FIRECRACKER_ARCH"
case "$FIRECRACKER_ARCH" in
  aarch64) DEFAULT_KERNEL_PATH="$ARTIFACT_DIR/Image" ;;
  *) DEFAULT_KERNEL_PATH="$ARTIFACT_DIR/vmlinux" ;;
esac
KERNEL_PATH="${AGENCY_FIRECRACKER_KERNEL:-$DEFAULT_KERNEL_PATH}"

usage() {
  cat <<EOF
Usage: scripts/readiness/firecracker-artifacts.sh [--fetch-only] [--verify-existing] [--skip-checksum]

Fetch and verify the pinned upstream Firecracker release artifact:
  $DEST_BIN

This script does not build the Firecracker kernel. The kernel must come from
Agency's Linux build artifact pipeline and be placed at:
  $KERNEL_PATH

Environment:
  AGENCY_HOME                         default: $HOME/.agency
  AGENCY_FIRECRACKER_ARTIFACT_DIR      output artifact directory
  AGENCY_FIRECRACKER_BUILD_DIR         download/extract workspace
  AGENCY_FIRECRACKER_VERSION           default: $FIRECRACKER_VERSION
  AGENCY_FIRECRACKER_ARCH              default: $(uname -m)
  AGENCY_FIRECRACKER_RELEASE_BASE_URL  default: GitHub release URL
  AGENCY_FIRECRACKER_KERNEL            default: $DEFAULT_KERNEL_PATH
EOF
}

log() {
  printf '[firecracker-artifacts] %s\n' "$*" >&2
}

fail() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

verify_existing() {
  require_cmd sha256sum
  require_cmd file
  [[ -x "$DEST_BIN" ]] || fail "missing executable Firecracker artifact: $DEST_BIN"
  local version_out version_line
  version_out="$("$DEST_BIN" --version 2>&1 || true)"
  version_line="$(printf '%s\n' "$version_out" | grep -Eo 'Firecracker v[^[:space:]]+' | head -1)"
  [[ -n "$version_line" ]] || version_line="$(printf '%s' "$version_out" | tr '\n' ' ')"
  printf 'firecracker_binary_path=%s\n' "$DEST_BIN"
  printf 'firecracker_sha256=%s\n' "$(sha256sum "$DEST_BIN" | awk '{print $1}')"
  printf 'firecracker_version=%s\n' "$version_line"
  if [[ -r "$KERNEL_PATH" ]]; then
    printf 'kernel_path=%s\n' "$KERNEL_PATH"
    printf 'kernel_sha256=%s\n' "$(sha256sum "$KERNEL_PATH" | awk '{print $1}')"
    printf 'kernel_file=%s\n' "$(file -b "$KERNEL_PATH")"
  else
    printf 'kernel_path_missing=%s\n' "$KERNEL_PATH"
  fi
}

verify_checksum() {
  require_cmd sha256sum
  local expected actual
  expected="$(awk '{print $1; exit}' "$SHA_FILE")"
  [[ -n "$expected" ]] || fail "could not read checksum from $SHA_FILE"
  actual="$(sha256sum "$TARBALL" | awk '{print $1}')"
  if [[ "$actual" != "$expected" ]]; then
    fail "Firecracker tarball SHA256 mismatch: got $actual want $expected"
  fi
  log "Firecracker tarball SHA256 verified: $actual"
}

fetch_only=0
verify_only=0
skip_checksum=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --fetch-only)
      fetch_only=1
      shift
      ;;
    --verify-existing)
      verify_only=1
      shift
      ;;
    --skip-checksum)
      skip_checksum=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage >&2
      fail "unknown argument: $1"
      ;;
  esac
done

case "$FIRECRACKER_ARCH" in
  x86_64|aarch64) ;;
  *) fail "unsupported Firecracker release architecture: $FIRECRACKER_ARCH" ;;
esac

if [[ "$verify_only" == "1" ]]; then
  verify_existing
  exit 0
fi

require_cmd curl
require_cmd tar
require_cmd chmod
require_cmd cp

mkdir -p "$DOWNLOAD_DIR" "$EXTRACT_DIR" "$DEST_DIR"

if [[ ! -s "$TARBALL" ]]; then
  log "Downloading $TARBALL_URL"
  curl -fL "$TARBALL_URL" -o "$TARBALL"
fi

if [[ "$skip_checksum" != "1" ]]; then
  if [[ ! -s "$SHA_FILE" ]]; then
    log "Downloading $SHA_URL"
    curl -fL "$SHA_URL" -o "$SHA_FILE"
  fi
  verify_checksum
else
  log "Skipping Firecracker checksum verification by request"
fi

if [[ "$fetch_only" == "1" ]]; then
  log "Fetch complete"
  exit 0
fi

log "Extracting Firecracker release"
rm -rf "$RELEASE_DIR"
tar -C "$EXTRACT_DIR" -xzf "$TARBALL"
[[ -f "$SOURCE_BIN" ]] || fail "release did not contain $SOURCE_BIN"

cp "$SOURCE_BIN" "$DEST_BIN"
chmod 0755 "$DEST_BIN"

log "Firecracker artifact ready"
verify_existing
