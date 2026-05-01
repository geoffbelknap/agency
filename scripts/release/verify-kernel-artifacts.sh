#!/usr/bin/env bash
set -euo pipefail

KERNEL_RELEASE_TAG="${AGENCY_KERNEL_RELEASE_TAG:-agency-kernels-6.12.22-agency1}"
EXPECTED=(
  agency-kernel-6.12.22-firecracker-x86_64
  agency-kernel-6.12.22-firecracker-aarch64
  agency-kernel-6.12.22-apple-vf-arm64
)

fail() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

command -v gh >/dev/null 2>&1 || fail "Missing required command: gh"
command -v curl >/dev/null 2>&1 || fail "Missing required command: curl"

gh release view "$KERNEL_RELEASE_TAG" >/dev/null ||
  fail "GitHub kernel artifact release ${KERNEL_RELEASE_TAG} does not exist"

json="$(gh release view "$KERNEL_RELEASE_TAG" --json assets)"
tmp="$(mktemp)"
printf '%s' "$json" >"$tmp"
python3 - "$tmp" "$KERNEL_RELEASE_TAG" "${EXPECTED[@]}" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as fh:
    data = json.load(fh)
assets = {asset["name"] for asset in data.get("assets", [])}
missing = []
for name in sys.argv[3:]:
    missing.extend(candidate for candidate in (name, name + ".sha256") if candidate not in assets)
manifest = sys.argv[2] + ".manifest.json"
if manifest not in assets:
    missing.append(manifest)
if missing:
    print(f"missing kernel release assets: {missing}", file=sys.stderr)
    sys.exit(1)
PY
rm -f "$tmp"

for asset in "${EXPECTED[@]}"; do
  checksum="$(curl -fsSL "https://github.com/geoffbelknap/agency/releases/download/${KERNEL_RELEASE_TAG}/${asset}.sha256")"
  grep -q "$asset" <<<"$checksum" || fail "checksum sidecar for ${asset} does not name the artifact"
done

printf 'Kernel artifact release %s is ready.\n' "$KERNEL_RELEASE_TAG"
