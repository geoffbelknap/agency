#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
SOURCE_HOME="${AGENCY_SOURCE_HOME:-${HOME}/.agency}"
DISPOSABLE_HOME="${AGENCY_OPERATOR_OCI_HOME:-}"
GATEWAY_PORT="${AGENCY_OPERATOR_OCI_GATEWAY_PORT:-18231}"
KEEP_HOME="${AGENCY_OPERATOR_OCI_KEEP_HOME:-0}"

usage() {
  cat <<'EOF'
Usage: ./scripts/test-live-hub-operator-oci.sh [--keep-home]

Runs an opt-in live operator-path Hub OCI check against GHCR using a disposable
Agency home and isolated gateway port.

The test verifies:
  - `agency -q hub update` pulls the published OCI catalog through the gateway
  - key connector, service, provider, routing, setup, and skill artifacts are cached
  - Markdown skills are discoverable through `agency -q hub search`
  - setup wizard config is served from the OCI-synced setup component
  - provider install/remove works and cleans routing when cosign is available
  - managed routing remains update/upgrade surface, not installable search output

Environment:
  AGENCY_SOURCE_HOME                  Source Agency home to clone (default: ~/.agency)
  AGENCY_OPERATOR_OCI_HOME            Disposable home path (default: mktemp)
  AGENCY_OPERATOR_OCI_GATEWAY_PORT    Gateway host port (default: 18231)
  AGENCY_OPERATOR_OCI_KEEP_HOME=1     Preserve disposable home after the run
  AGENCY_BIN                          Agency binary to test (default: repo ./agency, then PATH)

Notes:
  cosign is required for OCI component install signature verification. If cosign
  is missing, this script still validates update/search/setup and skips the
  provider install/remove subtest.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --keep-home)
      KEEP_HOME=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [ ! -d "$SOURCE_HOME" ]; then
  echo "Source Agency home does not exist: $SOURCE_HOME" >&2
  exit 1
fi

if [ -z "$DISPOSABLE_HOME" ]; then
  DISPOSABLE_HOME="$(mktemp -d "${TMPDIR:-/tmp}/agency-operator-oci-home.XXXXXX")"
else
  mkdir -p "$DISPOSABLE_HOME"
fi

if [ -z "${AGENCY_BIN:-}" ]; then
  if [ -x "$ROOT_DIR/agency" ]; then
    AGENCY_BIN="$ROOT_DIR/agency"
  else
    AGENCY_BIN="$(command -v agency || true)"
  fi
fi

if [ -z "${AGENCY_BIN:-}" ] || [ ! -x "$AGENCY_BIN" ]; then
  echo "agency binary not found. Set AGENCY_BIN or run make build." >&2
  exit 1
fi

cleanup() {
  local status="$?"
  trap - EXIT INT TERM HUP
  AGENCY_HOME="$DISPOSABLE_HOME" "$AGENCY_BIN" serve stop >/dev/null 2>&1 || true
  if [ "$KEEP_HOME" = "1" ]; then
    echo "Keeping disposable Agency home at $DISPOSABLE_HOME"
  else
    rm -rf "$DISPOSABLE_HOME"
  fi
  exit "$status"
}
trap cleanup EXIT INT TERM HUP

mkdir -p "$DISPOSABLE_HOME"
cp -R "$SOURCE_HOME"/. "$DISPOSABLE_HOME"/ 2>/dev/null || true
rm -f "$DISPOSABLE_HOME/gateway.pid" "$DISPOSABLE_HOME/gateway.log"
rm -rf "$DISPOSABLE_HOME/run" "$DISPOSABLE_HOME/hub-cache" "$DISPOSABLE_HOME/infrastructure/routing.yaml"

export AGENCY_HOME="$DISPOSABLE_HOME"
export AGENCY_TEST_OCI_LIVE=1
export AGENCY_OPERATOR_OCI_GATEWAY_PORT="$GATEWAY_PORT"

python3 -c 'import os, yaml
p = os.path.join(os.environ["AGENCY_HOME"], "config.yaml")
data = {}
if os.path.exists(p):
    with open(p, encoding="utf-8") as f:
        data = yaml.safe_load(f) or {}
data["gateway_addr"] = "127.0.0.1:" + os.environ["AGENCY_OPERATOR_OCI_GATEWAY_PORT"]
if not data.get("token"):
    data["token"] = "agency-operator-oci-live-check-token"
with open(p, "w", encoding="utf-8") as f:
    yaml.safe_dump(data, f, sort_keys=False)
'

echo "==> Disposable Agency home: $DISPOSABLE_HOME"
echo "==> Gateway port:           $GATEWAY_PORT"
echo "==> Agency binary:          $AGENCY_BIN"

"$AGENCY_BIN" -q hub update

CACHE_ROOT="$(find "$DISPOSABLE_HOME/hub-cache" -mindepth 1 -maxdepth 1 -type d | head -1)"
if [ -z "$CACHE_ROOT" ]; then
  echo "expected hub cache source directory after hub update" >&2
  exit 1
fi
echo "==> Hub cache source:       $CACHE_ROOT"

required_paths=(
  "connectors/limacharlie/connector.yaml"
  "pricing/routing.yaml"
  "providers/openai/provider.yaml"
  "setup/default-wizard/setup.yaml"
  "services/github/service.yaml"
  "skills/code-review/SKILL.md"
)

for relative_path in "${required_paths[@]}"; do
  if [ ! -f "$CACHE_ROOT/$relative_path" ]; then
    echo "missing expected OCI-cached artifact: $relative_path" >&2
    exit 1
  fi
  echo "  ✓ $relative_path"
done

skill_search="$("$AGENCY_BIN" -q hub search code-review)"
if ! printf '%s\n' "$skill_search" | grep -Eq 'code-review[[:space:]]+skill'; then
  echo "expected code-review skill in hub search output" >&2
  printf '%s\n' "$skill_search" >&2
  exit 1
fi
echo "  ✓ code-review skill is discoverable"

setup_search="$("$AGENCY_BIN" -q hub search default-wizard)"
if ! printf '%s\n' "$setup_search" | grep -Eq 'default-wizard[[:space:]]+setup'; then
  echo "expected default-wizard setup in hub search output" >&2
  printf '%s\n' "$setup_search" >&2
  exit 1
fi
echo "  ✓ default setup wizard is discoverable"

token="$(python3 -c 'import os, yaml
p = os.path.join(os.environ["AGENCY_HOME"], "config.yaml")
with open(p, encoding="utf-8") as f:
    print((yaml.safe_load(f) or {}).get("token", ""))
')"

curl -fsS \
  -H "X-Agency-Token: $token" \
  "http://127.0.0.1:$GATEWAY_PORT/api/v1/infra/setup/config" |
  python3 -c 'import json, sys
data = json.load(sys.stdin)
assert data.get("kind") == "setup", data
assert "capability_tiers" in data, data
'
echo "  ✓ setup config is served from OCI cache"

routing_search="$("$AGENCY_BIN" -q hub search routing)"
if printf '%s\n' "$routing_search" | grep -Eq 'routing[[:space:]]+routing'; then
  echo "managed routing should not appear as an installable hub search result" >&2
  printf '%s\n' "$routing_search" >&2
  exit 1
fi
echo "  ✓ managed routing remains non-installable search surface"

if command -v cosign >/dev/null 2>&1; then
  "$AGENCY_BIN" -q hub remove openai --kind provider >/dev/null 2>&1 || true
  "$AGENCY_BIN" -q hub install openai --kind provider --yes
  find "$DISPOSABLE_HOME/hub-registry/providers" -name provider.yaml -print0 |
    xargs -0 grep -Eq 'name:[[:space:]]+openai'
  grep -Eq 'openai:|gpt-4o' "$DISPOSABLE_HOME/infrastructure/routing.yaml"
  "$AGENCY_BIN" -q hub remove openai --kind provider
  if grep -Eq 'openai:|gpt-4o' "$DISPOSABLE_HOME/infrastructure/routing.yaml"; then
    echo "provider routing remained after remove" >&2
    exit 1
  fi
  echo "  ✓ provider install/remove verifies signatures and cleans routing"
else
  echo "  ! cosign not found; provider install/remove signature-verification subtest skipped"
fi
