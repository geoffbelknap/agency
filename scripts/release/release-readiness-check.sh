#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

MODE="published"
TARGET_VERSION=""
TARGET_TAG=""
EXPECTED_RUNTIME_ARTIFACTS=(
  agency-runtime-body
  agency-runtime-enforcer
)

log() {
  printf '==> %s\n' "$*"
}

fail() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

usage() {
  cat <<'EOF'
Usage:
  ./scripts/release/release-readiness-check.sh preflight --version <semver>
  ./scripts/release/release-readiness-check.sh package-smoke
  ./scripts/release/release-readiness-check.sh published [--tag vX.Y.Z]

Modes:
  preflight   Validate local release wiring and build a stamped binary.
  package-smoke
              Build a snapshot release archive and validate packaged host infra deps.
  published   Validate an already-published release, Homebrew formula, and GHCR runtime artifacts.

Examples:
  ./scripts/release/release-readiness-check.sh preflight --version 0.2.0
  ./scripts/release/release-readiness-check.sh package-smoke
  ./scripts/release/release-readiness-check.sh published --tag v0.2.0
EOF
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "Missing required command: $1"
}

latest_release_tag() {
  gh release list --limit 20 --json tagName,isLatest --jq '.[] | select(.isLatest == true) | .tagName' | head -n1
}

formula_download_url() {
  gh api repos/geoffbelknap/homebrew-tap/contents/agency.rb --jq '.download_url'
}

check_formula_for_tag() {
  local tag="$1"
  local version="${tag#v}"
  local formula_content
  formula_content="$(curl -fsSL "$(formula_download_url)")"

  printf '%s\n' "$formula_content" | grep -q "version \"${version}\"" ||
    fail "Homebrew formula version does not match ${version}"
  printf '%s\n' "$formula_content" | grep -q "/download/${tag}/agency_${version}_" ||
    fail "Homebrew formula does not reference release assets for ${tag}"
}

check_release_exists() {
  local tag="$1"
  gh release view "$tag" >/dev/null ||
    fail "GitHub release ${tag} does not exist"
}

check_release_assets() {
  local tag="$1"
  local version="${tag#v}"
  local release_json
  local release_file
  release_json="$(gh release view "$tag" --json assets)"
  release_file="$(mktemp)"
  printf '%s' "$release_json" >"$release_file"

  python3 - "$version" "$release_file" <<'PY'
import json
import sys

version = sys.argv[1]
release_file = sys.argv[2]
with open(release_file, "r", encoding="utf-8") as fh:
    data = json.load(fh)
assets = {asset["name"]: asset for asset in data.get("assets", [])}
expected = [
    f"agency_{version}_darwin_amd64.tar.gz",
    f"agency_{version}_darwin_arm64.tar.gz",
    f"agency_{version}_linux_amd64.tar.gz",
    f"agency_{version}_linux_arm64.tar.gz",
    "agency-firecracker-vmlinux_x86_64",
    "agency-firecracker-vmlinux_x86_64.sha256",
    "checksums.txt",
]
missing = [name for name in expected if name not in assets]
if missing:
    print(f"missing release assets: {missing}", file=sys.stderr)
    sys.exit(1)
for name in expected:
    digest = assets[name].get("digest", "")
    if not digest.startswith("sha256:"):
        print(f"asset {name} missing sha256 digest", file=sys.stderr)
        sys.exit(1)
PY
  rm -f "$release_file"
}

check_formula_sha_matches_release() {
  local tag="$1"
  local version="${tag#v}"
  local formula_content
  local release_json
  local release_file
  local formula_file

  formula_content="$(curl -fsSL "$(formula_download_url)")"
  release_json="$(gh release view "$tag" --json assets)"
  release_file="$(mktemp)"
  formula_file="$(mktemp)"
  printf '%s' "$release_json" >"$release_file"
  printf '%s' "$formula_content" >"$formula_file"

  python3 - "$version" "$release_file" "$formula_file" <<'PY'
import json
import sys

version = sys.argv[1]
release_file = sys.argv[2]
formula_file = sys.argv[3]
with open(release_file, "r", encoding="utf-8") as fh:
    assets = {asset["name"]: asset for asset in json.load(fh).get("assets", [])}
with open(formula_file, "r", encoding="utf-8") as fh:
    formula = fh.read()

expected_pairs = {
    f"agency_{version}_darwin_amd64.tar.gz": None,
    f"agency_{version}_darwin_arm64.tar.gz": None,
    f"agency_{version}_linux_amd64.tar.gz": None,
    f"agency_{version}_linux_arm64.tar.gz": None,
}
for name in expected_pairs:
    digest = assets.get(name, {}).get("digest", "")
    if not digest.startswith("sha256:"):
        print(f"missing release digest for {name}", file=sys.stderr)
        sys.exit(1)
    expected_pairs[name] = digest.split("sha256:", 1)[1]

for name, sha in expected_pairs.items():
    if name not in formula:
        print(f"formula missing URL for {name}", file=sys.stderr)
        sys.exit(1)
    if sha not in formula:
        print(f"formula missing checksum {sha} for {name}", file=sys.stderr)
        sys.exit(1)
PY
  rm -f "$release_file" "$formula_file"
}

check_required_files() {
  local files=(
    ".github/workflows/release.yaml"
    ".github/workflows/release-runtime-artifacts.yml"
    ".goreleaser.yaml"
    ".goreleaser.rc.yaml"
  )
  local file
  for file in "${files[@]}"; do
    [ -f "$ROOT_DIR/$file" ] || fail "Missing required release file: $file"
  done
}

host_os() {
  case "$(uname -s)" in
    Darwin) printf 'darwin' ;;
    Linux) printf 'linux' ;;
    *) fail "Unsupported package smoke OS: $(uname -s)" ;;
  esac
}

host_arch() {
  case "$(uname -m)" in
    arm64|aarch64) printf 'arm64' ;;
    x86_64|amd64) printf 'amd64' ;;
    *) fail "Unsupported package smoke architecture: $(uname -m)" ;;
  esac
}

run_package_smoke() {
  require_cmd goreleaser
  require_cmd npm
  require_cmd python3
  require_cmd tar
  check_required_files

  local os
  local arch
  local archive
  local tmp

  os="$(host_os)"
  arch="$(host_arch)"

  log "Building snapshot release archive for package smoke"
  (cd "$ROOT_DIR" && goreleaser release --snapshot --clean --skip=publish)

  archive="$(find "$ROOT_DIR/dist" -maxdepth 1 -type f -name "agency_*_${os}_${arch}.tar.gz" | sort | tail -n1)"
  [ -n "$archive" ] || fail "Snapshot archive for ${os}/${arch} was not produced"

  tmp="$(mktemp -d "${TMPDIR:-/tmp}/agency-package-smoke.XXXXXX")"
  tar -xzf "$archive" -C "$tmp"

  [ -x "$tmp/agency" ] || fail "Package archive missing executable agency binary"
  [ -f "$tmp/web/dist/index.html" ] || fail "Package archive missing prebuilt web/dist/index.html"
  [ ! -f "$tmp/web/package.json" ] || fail "Package archive contains web/package.json; packaged installs must not run npm"
  [ -f "$tmp/services/logging_config.py" ] || fail "Package archive missing host service logging helper"
  [ -f "$tmp/services/comms/server.py" ] || fail "Package archive missing comms service"
  [ -f "$tmp/services/knowledge/server.py" ] || fail "Package archive missing knowledge service"
  [ -x "$tmp/bin/enforcer" ] || fail "Package archive missing executable Firecracker enforcer helper"
  [ -x "$tmp/bin/agency-vsock-http-bridge" ] || fail "Package archive missing executable Firecracker vsock HTTP bridge"
  [ -f "$tmp/scripts/readiness/firecracker-artifacts.sh" ] || fail "Package archive missing Firecracker binary provisioning script"
  [ -f "$tmp/scripts/readiness/firecracker-kernel-artifacts.sh" ] || fail "Package archive missing Firecracker kernel provisioning script"
  [ -f "$tmp/images/firecracker/buildroot/configs/agency_firecracker_x86_64_defconfig" ] || fail "Package archive missing Firecracker Buildroot config"

  log "Installing packaged host Python dependencies into a fresh venv"
  AGENCY_PYTHON_VENV="$tmp/.venv" "$tmp/scripts/install/host-dependencies.sh" --skip-system-packages

  log "Instantiating packaged host infrastructure services"
  (cd "$tmp" && PYTHONPATH="$tmp" "$tmp/.venv/bin/python" - <<'PY'
from pathlib import Path
from tempfile import TemporaryDirectory

from services.comms.server import create_app as create_comms_app
from services.knowledge.server import create_app as create_knowledge_app

with TemporaryDirectory() as root:
    base = Path(root)
    create_comms_app(data_dir=base / "comms", agents_dir=base / "agents")
    create_knowledge_app(data_dir=base / "knowledge", enable_ingestion=False)
PY
  )

  rm -rf "$tmp"
  log "Package smoke passed"
}

check_runtime_artifact_manifest() {
  local image_ref="$1"
  local attempt
  for attempt in 1 2 3; do
    if go run ./cmd/runtime-oci-artifact --inspect-ref "$image_ref"; then
      return 0
    fi
    sleep 2
  done
  return 1
}

run_preflight() {
  [ -n "$TARGET_VERSION" ] || fail "preflight mode requires --version <semver>"
  require_cmd git
  require_cmd go
  check_required_files

  local short_commit
  local build_date
  local tmp_bin
  local version_out

  short_commit="$(git -C "$ROOT_DIR" rev-parse --short HEAD)"
  build_date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  tmp_bin="$(mktemp "$ROOT_DIR/agency-release-check.XXXXXX")"

  log "Building stamped binary for ${TARGET_VERSION}"
  go build -o "$tmp_bin" \
    -ldflags="-s -w -X main.version=${TARGET_VERSION} -X main.commit=${short_commit} -X main.date=${build_date} -X main.buildID=${short_commit}" \
    ./cmd/gateway

  version_out="$("$tmp_bin" --version)"
  rm -f "$tmp_bin"

  printf '%s\n' "$version_out" | grep -q "agency version ${TARGET_VERSION}" ||
    fail "Stamped binary did not report version ${TARGET_VERSION}"
  printf '%s\n' "$version_out" | grep -q "${short_commit}" ||
    fail "Stamped binary did not report commit/build ID ${short_commit}"
  printf '%s\n' "$version_out" | grep -q "unknown" &&
    fail "Stamped binary still reports unknown metadata"

  if [ "${AGENCY_RELEASE_SKIP_PACKAGE_SMOKE:-0}" != "1" ]; then
    run_package_smoke
  fi

  log "Preflight passed"
}

run_published() {
  require_cmd gh
  require_cmd curl
  require_cmd go
  require_cmd python3
  check_required_files

  if [ -z "$TARGET_TAG" ]; then
    TARGET_TAG="$(latest_release_tag)"
  fi
  [ -n "$TARGET_TAG" ] || fail "Could not determine release tag"

  log "Checking published release ${TARGET_TAG}"
  check_release_exists "$TARGET_TAG"
  check_release_assets "$TARGET_TAG"
  check_formula_for_tag "$TARGET_TAG"
  check_formula_sha_matches_release "$TARGET_TAG"

  local artifact
  local failures=0
  for artifact in "${EXPECTED_RUNTIME_ARTIFACTS[@]}"; do
    log "Checking ${artifact}:${TARGET_TAG}"
    if ! check_runtime_artifact_manifest "ghcr.io/geoffbelknap/${artifact}:${TARGET_TAG}"; then
      printf 'ERROR: Could not inspect runtime artifact manifest for ghcr.io/geoffbelknap/%s:%s\n' "$artifact" "$TARGET_TAG" >&2
      failures=$((failures + 1))
    fi
  done

  if [ "$failures" -ne 0 ]; then
    fail "Published release check found ${failures} runtime artifact manifest failure(s)"
  fi

  log "Published release check passed"
}

main() {
  if [ $# -eq 0 ]; then
    MODE="published"
  else
    MODE="$1"
    shift
  fi

  while [ $# -gt 0 ]; do
    case "$1" in
      --version)
        TARGET_VERSION="${2:-}"
        shift 2
        ;;
      --tag)
        TARGET_TAG="${2:-}"
        shift 2
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        fail "Unknown argument: $1"
        ;;
    esac
  done

  case "$MODE" in
    preflight)
      run_preflight
      ;;
    published)
      run_published
      ;;
    package-smoke)
      run_package_smoke
      ;;
    *)
      fail "Unknown mode: $MODE"
      ;;
  esac
}

main "$@"
