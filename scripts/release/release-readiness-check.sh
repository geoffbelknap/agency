#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

MODE="published"
TARGET_VERSION=""
TARGET_TAG=""

EXPECTED_IMAGES=(
  agency-python-base
  agency-comms
  agency-knowledge
  agency-body
  agency-egress
  agency-enforcer
  agency-workspace
  agency-web
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
  ./scripts/release/release-readiness-check.sh published [--tag vX.Y.Z]

Modes:
  preflight   Validate local release wiring and build a stamped binary.
  published   Validate an already-published release, Homebrew formula, and GHCR images.

Examples:
  ./scripts/release/release-readiness-check.sh preflight --version 0.2.0
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
    ".github/workflows/release-images.yml"
    ".goreleaser.yaml"
  )
  local file
  for file in "${files[@]}"; do
    [ -f "$ROOT_DIR/$file" ] || fail "Missing required release file: $file"
  done
}

check_image_manifest() {
  local image_ref="$1"
  local manifest_file
  local err_file
  local attempt
  manifest_file="$(mktemp)"
  err_file="$(mktemp)"

  for attempt in 1 2 3; do
    if docker manifest inspect "$image_ref" >"$manifest_file" 2>"$err_file"; then
      break
    fi
    if [ "$attempt" -eq 3 ]; then
      cat "$err_file" >&2
      rm -f "$manifest_file" "$err_file"
      return 1
    fi
    sleep 2
  done

  python3 - "$manifest_file" "$image_ref" <<'PY'
import json
import sys

manifest_path, image_ref = sys.argv[1], sys.argv[2]
with open(manifest_path, "r", encoding="utf-8") as fh:
    data = json.load(fh)

manifests = data.get("manifests") or []
platforms = {
    (m.get("platform") or {}).get("architecture")
    for m in manifests
    if (m.get("platform") or {}).get("os") == "linux"
}
missing = {"amd64", "arm64"} - platforms
if missing:
    print(f"missing {sorted(missing)} in {image_ref}", file=sys.stderr)
    sys.exit(1)
PY

  rm -f "$manifest_file" "$err_file"
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

  log "Preflight passed"
}

run_published() {
  require_cmd gh
  require_cmd curl
  require_cmd docker
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

  local image
  local failures=0
  for image in "${EXPECTED_IMAGES[@]}"; do
    log "Checking ${image}:${TARGET_TAG}"
    if ! check_image_manifest "ghcr.io/geoffbelknap/${image}:${TARGET_TAG}"; then
      printf 'ERROR: Could not inspect image manifest for ghcr.io/geoffbelknap/%s:%s\n' "$image" "$TARGET_TAG" >&2
      failures=$((failures + 1))
    fi
    log "Checking ${image}:latest"
    if ! check_image_manifest "ghcr.io/geoffbelknap/${image}:latest"; then
      printf 'ERROR: Could not inspect image manifest for ghcr.io/geoffbelknap/%s:latest\n' "$image" >&2
      failures=$((failures + 1))
    fi
  done

  if [ "$failures" -ne 0 ]; then
    fail "Published release check found ${failures} image manifest failure(s)"
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
    *)
      fail "Unknown mode: $MODE"
      ;;
  esac
}

main "$@"
