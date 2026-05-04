#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DUMMY_SHA="0000000000000000000000000000000000000000000000000000000000000000"
TMP_PARENT="${TMPDIR:-/tmp}"

cd "$ROOT_DIR"

./scripts/release/verify-kernel-artifacts.sh
./scripts/release/verify-apple-vf-helper-assets.sh >/dev/null
./scripts/release/build-homebrew-python-wheelhouses.sh
./scripts/release/audit-homebrew-python-wheelhouses.sh

if [ -f .release/homebrew-python-wheelhouses/env.sh ]; then
  # shellcheck disable=SC1091
  source .release/homebrew-python-wheelhouses/env.sh
fi

: "${AGENCY_APPLE_VF_HELPERS_DARWIN_ARM64_SHA256:=$DUMMY_SHA}"
: "${HOMEBREW_TAP_TOKEN:=dummy}"

export AGENCY_APPLE_VF_HELPERS_DARWIN_ARM64_SHA256
export AGENCY_PYTHON_WHEELHOUSE_DARWIN_ARM64_SHA256
export AGENCY_PYTHON_WHEELHOUSE_DARWIN_AMD64_SHA256
export AGENCY_PYTHON_WHEELHOUSE_LINUX_AMD64_SHA256
export AGENCY_PYTHON_WHEELHOUSE_LINUX_ARM64_SHA256
export HOMEBREW_TAP_TOKEN
export GOCACHE="${GOCACHE:-$TMP_PARENT/agency-release-go-cache}"
export GOMODCACHE="${GOMODCACHE:-$TMP_PARENT/agency-release-go-mod-cache}"
mkdir -p "$GOCACHE" "$GOMODCACHE"

goreleaser release --snapshot --clean --skip=publish --config .goreleaser.yaml

formula="$(find dist -path '*/agency.rb' -type f | head -n1)"
[ -n "$formula" ] || {
  echo "generated Homebrew formula not found under dist/" >&2
  exit 1
}

grep -q 'resource "python-wheelhouse-darwin-arm64"' "$formula"
grep -q 'resource "python-wheelhouse-linux-amd64"' "$formula"
grep -q 'virtualenv_create' "$formula"
grep -q 'preserve_rpath' "$formula"
grep -q 'skip_clean "libexec/venv"' "$formula"

echo "Homebrew release preflight passed."
