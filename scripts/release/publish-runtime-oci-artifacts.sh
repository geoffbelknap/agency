#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VERSION="${VERSION:-}"
IMAGE_PREFIX="${IMAGE_PREFIX:-ghcr.io/geoffbelknap}"
BUILD_ID="${BUILD_ID:-$(git -C "$ROOT_DIR" rev-parse HEAD)}"
WORK_DIR="${AGENCY_RUNTIME_OCI_WORK_DIR:-$(mktemp -d)}"

BODY_DEPS=(
  "httpx==0.28.1"
  "aiohttp==3.13.3"
  "pyyaml==6.0.3"
  "pydantic==2.12.5"
)

log() {
  printf '[runtime-oci] %s\n' "$*" >&2
}

fail() {
  printf '[runtime-oci] ERROR: %s\n' "$*" >&2
  exit 1
}

usage() {
  cat <<'EOF'
Usage:
  VERSION=0.2.0 scripts/release/publish-runtime-oci-artifacts.sh

Publishes daemonless microVM runtime OCI filesystem artifacts:
  - ghcr.io/geoffbelknap/agency-runtime-body:vVERSION
  - ghcr.io/geoffbelknap/agency-runtime-enforcer:vVERSION

Required environment:
  VERSION              Version without leading v.
  GHCR_USERNAME        GHCR username. GitHub Actions sets this to github.actor.
  GHCR_TOKEN           GHCR token with packages:write.

Optional environment:
  IMAGE_PREFIX         Registry prefix. Defaults to ghcr.io/geoffbelknap.
  BUILD_ID             Build/revision label. Defaults to current git HEAD.
  AGENCY_RUNTIME_OCI_WORK_DIR
EOF
}

cleanup() {
  if [ -z "${AGENCY_RUNTIME_OCI_WORK_DIR:-}" ] && [ -n "${WORK_DIR:-}" ]; then
    rm -rf "$WORK_DIR"
  fi
}
trap cleanup EXIT

case "${1:-}" in
  -h|--help)
    usage
    exit 0
    ;;
  "")
    ;;
  *)
    fail "unknown argument: $1"
    ;;
esac

[ -n "$VERSION" ] || fail "VERSION is required"
case "$VERSION" in
  latest|v*|*:*)
    fail "VERSION must be a concrete version without leading v: $VERSION"
    ;;
esac

command -v go >/dev/null 2>&1 || fail "go is required"
command -v python3 >/dev/null 2>&1 || fail "python3 is required"

mkdir -p "$WORK_DIR/body-deps-amd64/usr/local/lib/python3.13/site-packages"
mkdir -p "$WORK_DIR/body-deps-arm64/usr/local/lib/python3.13/site-packages"
mkdir -p "$WORK_DIR/bin"

log "Installing body Python dependencies for linux/amd64"
python3 -m pip install \
  --disable-pip-version-check \
  --only-binary=:all: \
  --implementation cp \
  --python-version 3.13 \
  --abi cp313 \
  --platform manylinux2014_x86_64 \
  --target "$WORK_DIR/body-deps-amd64/usr/local/lib/python3.13/site-packages" \
  "${BODY_DEPS[@]}"

log "Installing body Python dependencies for linux/arm64"
python3 -m pip install \
  --disable-pip-version-check \
  --only-binary=:all: \
  --implementation cp \
  --python-version 3.13 \
  --abi cp313 \
  --platform manylinux2014_aarch64 \
  --target "$WORK_DIR/body-deps-arm64/usr/local/lib/python3.13/site-packages" \
  "${BODY_DEPS[@]}"

log "Building enforcer binaries"
(
  cd "$ROOT_DIR/images/enforcer"
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o "$WORK_DIR/bin/enforcer-linux-amd64" .
  CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o "$WORK_DIR/bin/enforcer-linux-arm64" .
)

ca_bundle="/etc/ssl/certs/ca-certificates.crt"
[ -r "$ca_bundle" ] || ca_bundle=""

log "Publishing ${IMAGE_PREFIX}/agency-runtime-body:v${VERSION}"
go run ./cmd/runtime-oci-artifact \
  --artifact body \
  --repo "${IMAGE_PREFIX}/agency-runtime-body" \
  --version "$VERSION" \
  --source-root "$ROOT_DIR" \
  --build-id "$BUILD_ID" \
  --body-deps-amd64 "$WORK_DIR/body-deps-amd64" \
  --body-deps-arm64 "$WORK_DIR/body-deps-arm64"

log "Publishing ${IMAGE_PREFIX}/agency-runtime-enforcer:v${VERSION}"
go run ./cmd/runtime-oci-artifact \
  --artifact enforcer \
  --repo "${IMAGE_PREFIX}/agency-runtime-enforcer" \
  --version "$VERSION" \
  --source-root "$ROOT_DIR" \
  --build-id "$BUILD_ID" \
  --enforcer-amd64 "$WORK_DIR/bin/enforcer-linux-amd64" \
  --enforcer-arm64 "$WORK_DIR/bin/enforcer-linux-arm64" \
  --ca-bundle "$ca_bundle"
