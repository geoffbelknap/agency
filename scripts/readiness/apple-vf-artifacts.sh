#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
AGENCY_HOME_DIR="${AGENCY_HOME:-$HOME/.agency}"
ARTIFACT_DIR="${AGENCY_APPLE_VF_ARTIFACT_DIR:-$AGENCY_HOME_DIR/runtime/apple-vf-microvm/artifacts}"
BUILD_DIR="${AGENCY_APPLE_VF_BUILD_DIR:-$AGENCY_HOME_DIR/runtime/apple-vf-microvm/build}"
BUILDROOT_VERSION="${AGENCY_APPLE_VF_BUILDROOT_VERSION:-2026.02.1}"
BUILDROOT_URL="${AGENCY_APPLE_VF_BUILDROOT_URL:-https://buildroot.org/downloads/buildroot-${BUILDROOT_VERSION}.tar.xz}"
BUILDROOT_SIGN_URL="${BUILDROOT_URL}.sign"
BUILDROOT_TARBALL="$BUILD_DIR/downloads/buildroot-${BUILDROOT_VERSION}.tar.xz"
BUILDROOT_SIGN="$BUILDROOT_TARBALL.sign"
BUILDROOT_SRC="$BUILD_DIR/src/buildroot-${BUILDROOT_VERSION}"
BUILDROOT_OUTPUT="$BUILD_DIR/output"
BR2_EXTERNAL_DIR="$ROOT_DIR/images/apple-vf/buildroot"

usage() {
  cat <<EOF
usage: scripts/readiness/apple-vf-artifacts.sh [--fetch-only] [--configure-only] [--skip-signature-check]

Build the bootstrap ARM64 Linux kernel artifact for apple-vf-microvm:
  $ARTIFACT_DIR/Image

This script intentionally does not build a rootfs. Apple VF should use Agency's
OCI-to-ext4 image realization path, shared with Firecracker where possible.

Environment:
  AGENCY_HOME                         default: $HOME/.agency
  AGENCY_APPLE_VF_ARTIFACT_DIR         output artifact directory
  AGENCY_APPLE_VF_BUILD_DIR            Buildroot workspace/cache
  AGENCY_APPLE_VF_BUILDROOT_VERSION    default: $BUILDROOT_VERSION
  AGENCY_APPLE_VF_BUILDROOT_URL        default: $BUILDROOT_URL
  AGENCY_APPLE_VF_SKIP_SIGNATURE_CHECK  set 1 to skip Buildroot signature verification
EOF
}

log() {
  printf '[apple-vf-artifacts] %s\n' "$*" >&2
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

fetch_only=0
configure_only=0
skip_signature="${AGENCY_APPLE_VF_SKIP_SIGNATURE_CHECK:-0}"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --fetch-only)
      fetch_only=1
      shift
      ;;
    --configure-only)
      configure_only=1
      shift
      ;;
    --skip-signature-check)
      skip_signature=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 64
      ;;
  esac
done

require_cmd curl
require_cmd tar
require_cmd make
require_cmd cp

mkdir -p "$BUILD_DIR/downloads" "$BUILD_DIR/src" "$ARTIFACT_DIR"

if [[ ! -s "$BUILDROOT_TARBALL" ]]; then
  log "Downloading Buildroot $BUILDROOT_VERSION"
  curl -fL "$BUILDROOT_URL" -o "$BUILDROOT_TARBALL"
fi

if [[ "$skip_signature" != "1" ]]; then
  require_cmd gpg
  if [[ ! -s "$BUILDROOT_SIGN" ]]; then
    log "Downloading Buildroot signature"
    curl -fL "$BUILDROOT_SIGN_URL" -o "$BUILDROOT_SIGN"
  fi
  log "Verifying Buildroot signature"
  gpg --verify "$BUILDROOT_SIGN" "$BUILDROOT_TARBALL"
else
  log "Skipping Buildroot signature verification by request"
fi

if [[ "$fetch_only" == "1" ]]; then
  log "Fetch complete"
  exit 0
fi

if [[ ! -d "$BUILDROOT_SRC" ]]; then
  log "Extracting Buildroot"
  tar -C "$BUILD_DIR/src" -xf "$BUILDROOT_TARBALL"
fi

log "Configuring Buildroot external tree"
make -C "$BUILDROOT_SRC" O="$BUILDROOT_OUTPUT" BR2_EXTERNAL="$BR2_EXTERNAL_DIR" agency_apple_vf_aarch64_defconfig

if [[ "$configure_only" == "1" ]]; then
  log "Configure complete"
  exit 0
fi

log "Building ARM64 kernel"
make -C "$BUILDROOT_SRC" O="$BUILDROOT_OUTPUT"

kernel="$BUILDROOT_OUTPUT/images/Image"
if [[ ! -s "$kernel" ]]; then
  echo "Buildroot did not produce $kernel" >&2
  exit 1
fi

cp "$kernel" "$ARTIFACT_DIR/Image"

log "Kernel artifact ready:"
printf 'kernel_path=%s\n' "$ARTIFACT_DIR/Image"
printf 'rootfs_path=<from Agency OCI image realization>\n'
