#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
AGENCY_HOME_DIR="${AGENCY_HOME:-$HOME/.agency}"
ARTIFACT_DIR="${AGENCY_FIRECRACKER_ARTIFACT_DIR:-$AGENCY_HOME_DIR/runtime/firecracker/artifacts}"
BUILD_DIR="${AGENCY_FIRECRACKER_KERNEL_BUILD_DIR:-$AGENCY_HOME_DIR/runtime/firecracker/kernel-build}"
BUILDROOT_VERSION="${AGENCY_FIRECRACKER_BUILDROOT_VERSION:-2026.02.1}"
BUILDROOT_URL="${AGENCY_FIRECRACKER_BUILDROOT_URL:-https://buildroot.org/downloads/buildroot-${BUILDROOT_VERSION}.tar.xz}"
BUILDROOT_SIGN_URL="$BUILDROOT_URL.sign"
BUILDROOT_TARBALL="$BUILD_DIR/downloads/buildroot-${BUILDROOT_VERSION}.tar.xz"
BUILDROOT_SIGN="$BUILDROOT_TARBALL.sign"
BUILDROOT_SRC="$BUILD_DIR/src/buildroot-${BUILDROOT_VERSION}"
BUILDROOT_OUTPUT_BASE="$BUILD_DIR/output"
BUILDROOT_GPG_HOME="$BUILD_DIR/gnupg"
BUILDROOT_SIGNING_KEY_URL="${AGENCY_FIRECRACKER_BUILDROOT_SIGNING_KEY_URL:-https://gitlab.com/-/snippets/4836881/raw/main/arnout@rnout.be.asc}"
BR2_EXTERNAL_DIR="$ROOT_DIR/images/firecracker/buildroot"
LINUX_VERSION="6.12.22"
TARGET_ARCH="${AGENCY_FIRECRACKER_KERNEL_ARCH:-$(uname -m)}"

normalize_arch() {
  case "$1" in
    x86_64|amd64) printf 'x86_64' ;;
    aarch64|arm64) printf 'aarch64' ;;
    *) return 1 ;;
  esac
}

TARGET_ARCH="$(normalize_arch "$TARGET_ARCH")" || {
  echo "unsupported Firecracker kernel architecture: ${AGENCY_FIRECRACKER_KERNEL_ARCH:-$(uname -m)}" >&2
  exit 1
}
case "$TARGET_ARCH" in
  x86_64)
    KERNEL_FILE="vmlinux"
    KERNEL_FORMAT="elf-vmlinux"
    BUILDROOT_DEFCONFIG="agency_firecracker_x86_64_defconfig"
    ;;
  aarch64)
    KERNEL_FILE="Image"
    KERNEL_FORMAT="arm64-Image"
    BUILDROOT_DEFCONFIG="agency_firecracker_aarch64_defconfig"
    ;;
esac
KERNEL_ARTIFACT="$ARTIFACT_DIR/$KERNEL_FILE"
BUILDROOT_OUTPUT="$BUILDROOT_OUTPUT_BASE-$TARGET_ARCH"

usage() {
  cat <<EOF
Usage: scripts/readiness/firecracker-kernel-artifacts.sh [--arch x86_64|aarch64] [--fetch-only] [--configure-only] [--verify-existing] [--skip-signature-check]

Build the pinned Linux kernel artifact for Firecracker:
  $KERNEL_ARTIFACT

This script intentionally builds only the Firecracker guest kernel artifact.
Firecracker rootfs artifacts must come from Agency's OCI-to-ext4 realization
path, shared with Apple VF where possible.

Environment:
  AGENCY_HOME                                  default: $HOME/.agency
  AGENCY_FIRECRACKER_ARTIFACT_DIR              output artifact directory
  AGENCY_FIRECRACKER_KERNEL_ARCH               x86_64 or aarch64
  AGENCY_FIRECRACKER_KERNEL_BUILD_DIR          Buildroot workspace/cache
  AGENCY_FIRECRACKER_BUILDROOT_VERSION         default: $BUILDROOT_VERSION
  AGENCY_FIRECRACKER_BUILDROOT_URL             default: $BUILDROOT_URL
  AGENCY_FIRECRACKER_BUILDROOT_SIGNING_KEY_URL
  AGENCY_FIRECRACKER_SKIP_SIGNATURE_CHECK      set 1 to skip Buildroot signature verification
EOF
}

log() {
  printf '[firecracker-kernel-artifacts] %s\n' "$*" >&2
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

verify_existing_artifact() {
  if [[ ! -r "$KERNEL_ARTIFACT" ]]; then
    echo "missing readable artifact: $KERNEL_ARTIFACT" >&2
    exit 1
  fi
  require_cmd sha256sum
  require_cmd file
  if [[ "$KERNEL_FORMAT" == "elf-vmlinux" ]] && ! python3 - "$KERNEL_ARTIFACT" <<'PY'
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
if path.read_bytes()[:4] != b"\x7fELF":
    raise SystemExit(1)
PY
  then
    echo "artifact is not an uncompressed ELF vmlinux: $KERNEL_ARTIFACT" >&2
    exit 1
  fi
  printf 'kernel_path=%s\n' "$KERNEL_ARTIFACT"
  printf 'kernel_arch=%s\n' "$TARGET_ARCH"
  printf 'kernel_format=%s\n' "$KERNEL_FORMAT"
  printf 'sha256=%s\n' "$(sha256sum "$KERNEL_ARTIFACT" | awk '{print $1}')"
  printf 'file=%s\n' "$(file -b "$KERNEL_ARTIFACT")"
  printf 'size_bytes=%s\n' "$(wc -c <"$KERNEL_ARTIFACT" | tr -d '[:space:]')"
}

verify_buildroot_download() {
  require_cmd gpg
  require_cmd sha256sum
  mkdir -p "$BUILDROOT_GPG_HOME"
  chmod 700 "$BUILDROOT_GPG_HOME"
  if ! GNUPGHOME="$BUILDROOT_GPG_HOME" gpg --list-keys 18C7DF2819C1733D822D599EA500D6EE9CB0E540 >/dev/null 2>&1; then
    log "Importing Buildroot signing key"
    curl -fL "$BUILDROOT_SIGNING_KEY_URL" | GNUPGHOME="$BUILDROOT_GPG_HOME" gpg --import
  fi
  log "Verifying Buildroot signed checksum"
  GNUPGHOME="$BUILDROOT_GPG_HOME" gpg --verify "$BUILDROOT_SIGN"
  local expected actual
  expected="$(awk -v file="buildroot-${BUILDROOT_VERSION}.tar.xz" '$1 == "SHA256:" && $2 ~ /^[0-9a-f]+$/ && $3 == file { print $2 }' "$BUILDROOT_SIGN")"
  if [[ -z "$expected" ]]; then
    echo "could not find SHA256 for buildroot-${BUILDROOT_VERSION}.tar.xz in $BUILDROOT_SIGN" >&2
    exit 1
  fi
  actual="$(sha256sum "$BUILDROOT_TARBALL" | awk '{print $1}')"
  if [[ "$actual" != "$expected" ]]; then
    echo "Buildroot SHA256 mismatch: got $actual want $expected" >&2
    exit 1
  fi
  log "Buildroot SHA256 verified: $actual"
}

fetch_only=0
configure_only=0
verify_existing=0
skip_signature="${AGENCY_FIRECRACKER_SKIP_SIGNATURE_CHECK:-0}"
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
    --verify-existing)
      verify_existing=1
      shift
      ;;
    --skip-signature-check)
      skip_signature=1
      shift
      ;;
    --arch)
      TARGET_ARCH="$(normalize_arch "${2:-}")" || {
        echo "unsupported Firecracker kernel architecture: ${2:-}" >&2
        exit 64
      }
      case "$TARGET_ARCH" in
        x86_64)
          KERNEL_FILE="vmlinux"
          KERNEL_FORMAT="elf-vmlinux"
          BUILDROOT_DEFCONFIG="agency_firecracker_x86_64_defconfig"
          ;;
        aarch64)
          KERNEL_FILE="Image"
          KERNEL_FORMAT="arm64-Image"
          BUILDROOT_DEFCONFIG="agency_firecracker_aarch64_defconfig"
          ;;
      esac
      KERNEL_ARTIFACT="$ARTIFACT_DIR/$KERNEL_FILE"
      BUILDROOT_OUTPUT="$BUILDROOT_OUTPUT_BASE-$TARGET_ARCH"
      shift 2
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

if [[ "$verify_existing" == "1" ]]; then
  verify_existing_artifact
  exit 0
fi

if [[ "$(uname -s)" != "Linux" ]]; then
  echo "Firecracker kernel artifacts currently build on Linux" >&2
  exit 1
fi

require_cmd curl
require_cmd tar
require_cmd make
require_cmd cp
require_cmd python3

mkdir -p "$BUILD_DIR/downloads" "$BUILD_DIR/src" "$ARTIFACT_DIR"

if [[ ! -s "$BUILDROOT_TARBALL" ]]; then
  log "Downloading Buildroot $BUILDROOT_VERSION"
  curl -fL "$BUILDROOT_URL" -o "$BUILDROOT_TARBALL"
fi

if [[ "$skip_signature" != "1" ]]; then
  if [[ ! -s "$BUILDROOT_SIGN" ]]; then
    log "Downloading Buildroot signature"
    curl -fL "$BUILDROOT_SIGN_URL" -o "$BUILDROOT_SIGN"
  fi
  verify_buildroot_download
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
make -C "$BUILDROOT_SRC" O="$BUILDROOT_OUTPUT" BR2_EXTERNAL="$BR2_EXTERNAL_DIR" "$BUILDROOT_DEFCONFIG"

if [[ "$configure_only" == "1" ]]; then
  log "Configure complete"
  exit 0
fi

log "Building Firecracker $TARGET_ARCH $KERNEL_FILE"
make -C "$BUILDROOT_SRC" O="$BUILDROOT_OUTPUT"

kernel=""
if [[ "$KERNEL_FILE" == "vmlinux" ]]; then
  for candidate in \
    "$BUILDROOT_OUTPUT/images/vmlinux" \
    "$BUILDROOT_OUTPUT/build/linux-$LINUX_VERSION/vmlinux" \
    "$BUILDROOT_OUTPUT/build/linux-custom/vmlinux"; do
    if [[ -s "$candidate" ]]; then
      kernel="$candidate"
      break
    fi
  done
else
  kernel="$BUILDROOT_OUTPUT/images/Image"
fi
if [[ -z "$kernel" ]]; then
  echo "Buildroot did not produce $KERNEL_FILE artifact" >&2
  exit 1
fi

cp "$kernel" "$KERNEL_ARTIFACT"

log "Firecracker kernel artifact ready:"
verify_existing_artifact
printf 'rootfs_path=<from Agency OCI image realization>\n'
