#!/usr/bin/env bash
set -euo pipefail

HELPER_VERSION="${AGENCY_APPLE_VF_HELPER_VERSION:-0.1.0}"
HELPER_RELEASE_TAG="${AGENCY_APPLE_VF_HELPER_RELEASE_TAG:-agency-apple-vf-helpers-${HELPER_VERSION}-r1}"
ASSET_NAME="agency-apple-vf-helpers-${HELPER_VERSION}-darwin-arm64.tar.gz"
MANIFEST_NAME="${HELPER_RELEASE_TAG}.manifest.json"

fail() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

command -v gh >/dev/null 2>&1 || fail "Missing required command: gh"
command -v curl >/dev/null 2>&1 || fail "Missing required command: curl"

gh release view "$HELPER_RELEASE_TAG" >/dev/null ||
  fail "GitHub Apple VF helper release ${HELPER_RELEASE_TAG} does not exist"

json="$(gh release view "$HELPER_RELEASE_TAG" --json assets)"
tmp="$(mktemp)"
printf '%s' "$json" >"$tmp"
python3 - "$tmp" "$ASSET_NAME" "$MANIFEST_NAME" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as fh:
    data = json.load(fh)
assets = {asset["name"] for asset in data.get("assets", [])}
missing = [name for name in (sys.argv[2], sys.argv[2] + ".sha256", sys.argv[3]) if name not in assets]
if missing:
    print(f"missing Apple VF helper release assets: {missing}", file=sys.stderr)
    sys.exit(1)
PY
rm -f "$tmp"

checksum="$(curl -fsSL "https://github.com/geoffbelknap/agency/releases/download/${HELPER_RELEASE_TAG}/${ASSET_NAME}.sha256")"
sha="$(printf '%s\n' "$checksum" | awk -v asset="$ASSET_NAME" '$2 == asset && $1 ~ /^[0-9a-f]{64}$/ { print $1; exit }')"
[[ -n "$sha" ]] || fail "checksum sidecar for ${ASSET_NAME} does not contain a valid digest"

if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
  {
    printf 'release_tag=%s\n' "$HELPER_RELEASE_TAG"
    printf 'asset_name=%s\n' "$ASSET_NAME"
    printf 'sha256=%s\n' "$sha"
  } >>"$GITHUB_OUTPUT"
fi

printf 'Apple VF helper release %s is ready.\n' "$HELPER_RELEASE_TAG"
