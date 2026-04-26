#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
SOURCE_HOME="${AGENCY_SOURCE_HOME:-${HOME}/.agency}"
DISPOSABLE_HOME="${AGENCY_UPGRADE_OCI_HOME:-}"
GATEWAY_PORT="${AGENCY_UPGRADE_OCI_GATEWAY_PORT:-}"
KEEP_HOME="${AGENCY_UPGRADE_OCI_KEEP_HOME:-0}"
PACK_NAME="${AGENCY_UPGRADE_OCI_PACK_NAME:-security-ops}"
PROVIDER_NAME="${AGENCY_UPGRADE_OCI_PROVIDER_NAME:-}"

usage() {
  cat <<'EOF'
Usage: ./scripts/hub-oci/test-live-hub-upgrade-oci.sh [--keep-home]

Runs a live Hub OCI upgrade check against a disposable Agency home.

The test verifies:
  - `agency -q hub update` reports available upgrades from OCI sources
  - installed provider and pack survive `agency -q hub upgrade`
  - managed routing remains valid after upgrade
  - the upgraded pack still deploys and tears down cleanly

Environment:
  AGENCY_SOURCE_HOME                 Source Agency home to clone (default: ~/.agency)
  AGENCY_UPGRADE_OCI_HOME            Disposable home path (default: mktemp)
  AGENCY_UPGRADE_OCI_GATEWAY_PORT    Gateway host port (default: auto-selected free port)
  AGENCY_UPGRADE_OCI_KEEP_HOME=1     Preserve disposable home after the run
  AGENCY_UPGRADE_OCI_PACK_NAME       Pack name to exercise (default: security-ops)
  AGENCY_UPGRADE_OCI_PROVIDER_NAME   Provider name to exercise (required)
  AGENCY_BIN                         Agency binary to test (default: repo ./agency, then PATH)
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
  DISPOSABLE_HOME="$(mktemp -d "${TMPDIR:-/tmp}/agency-upgrade-oci-home.XXXXXX")"
else
  mkdir -p "$DISPOSABLE_HOME"
fi

if [ -z "$GATEWAY_PORT" ]; then
  GATEWAY_PORT="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')"
fi

if [ -z "${AGENCY_BIN:-}" ]; then
  if [ -x "$ROOT_DIR/agency" ]; then
    AGENCY_BIN="$ROOT_DIR/agency"
  else
    AGENCY_BIN="$(command -v agency || true)"
  fi
fi

if [ -z "${AGENCY_BIN:-}" ] || [ ! -x "$AGENCY_BIN" ]; then
  echo "agency binary not found. Set AGENCY_BIN or run go build -o agency ./cmd/gateway." >&2
  exit 1
fi

cleanup() {
  local status="$?"
  trap - EXIT INT TERM HUP
  AGENCY_HOME="$DISPOSABLE_HOME" HOME="$DISPOSABLE_HOME" "$AGENCY_BIN" serve stop >/dev/null 2>&1 || true
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
rm -rf "$DISPOSABLE_HOME/run" "$DISPOSABLE_HOME/hub-cache" "$DISPOSABLE_HOME/hub-registry" "$DISPOSABLE_HOME/agents" "$DISPOSABLE_HOME/channels" "$DISPOSABLE_HOME/teams" "$DISPOSABLE_HOME/missions"

export AGENCY_HOME="$DISPOSABLE_HOME"
export HOME="$DISPOSABLE_HOME"
export AGENCY_UPGRADE_OCI_GATEWAY_PORT="$GATEWAY_PORT"

CONFIG_PATH="$AGENCY_HOME/config.yaml"
if [ -f "$CONFIG_PATH" ]; then
  grep -Ev '^(gateway_addr|token):' "$CONFIG_PATH" > "$CONFIG_PATH.tmp" || true
else
  : > "$CONFIG_PATH.tmp"
fi
printf 'gateway_addr: "127.0.0.1:%s"\n' "$AGENCY_UPGRADE_OCI_GATEWAY_PORT" >> "$CONFIG_PATH.tmp"
if [ -f "$CONFIG_PATH" ] && grep -Eq '^token:' "$CONFIG_PATH"; then
  grep -E '^token:' "$CONFIG_PATH" >> "$CONFIG_PATH.tmp"
else
  printf 'token: "agency-upgrade-oci-live-check-token"\n' >> "$CONFIG_PATH.tmp"
fi
mv "$CONFIG_PATH.tmp" "$CONFIG_PATH"

if [ -z "$PROVIDER_NAME" ]; then
  echo "Set AGENCY_UPGRADE_OCI_PROVIDER_NAME to the provider adapter name to exercise." >&2
  exit 1
fi

echo "==> Disposable Agency home: $DISPOSABLE_HOME"
echo "==> Gateway port:           $GATEWAY_PORT"
echo "==> Agency binary:          $AGENCY_BIN"
echo "==> Pack under test:        $PACK_NAME"
echo "==> Provider under test:    $PROVIDER_NAME"

"$AGENCY_BIN" -q hub update

outdated_before="$("$AGENCY_BIN" -q hub outdated || true)"
printf '%s\n' "$outdated_before" | grep -Eq 'routing|ontology' ||
  { echo "expected hub outdated to report managed OCI upgrades" >&2; printf '%s\n' "$outdated_before" >&2; exit 1; }
echo "  ✓ hub outdated reports OCI-managed upgrades"

"$AGENCY_BIN" -q hub install "$PROVIDER_NAME" --kind provider --yes
"$AGENCY_BIN" -q hub install "$PACK_NAME" --kind pack --yes
echo "  ✓ provider and pack installed"

grep -Eq "^  ${PROVIDER_NAME}:" "$DISPOSABLE_HOME/infrastructure/routing.yaml" ||
  { echo "expected ${PROVIDER_NAME} routing before upgrade" >&2; cat "$DISPOSABLE_HOME/infrastructure/routing.yaml" >&2; exit 1; }
echo "  ✓ provider routing present before upgrade"

"$AGENCY_BIN" -q hub upgrade
echo "  ✓ hub upgrade completed"

"$AGENCY_BIN" -q hub show "$PROVIDER_NAME" >/dev/null
"$AGENCY_BIN" -q hub show "$PACK_NAME" >/dev/null
echo "  ✓ installed instances remain after upgrade"

grep -Eq "^  ${PROVIDER_NAME}:" "$DISPOSABLE_HOME/infrastructure/routing.yaml" ||
  { echo "expected ${PROVIDER_NAME} routing after upgrade" >&2; cat "$DISPOSABLE_HOME/infrastructure/routing.yaml" >&2; exit 1; }
echo "  ✓ provider routing survives upgrade"

"$AGENCY_BIN" -q hub deploy "$PACK_NAME"
echo "  ✓ upgraded pack deploy completed"

case "$PACK_NAME" in
  security-ops)
    "$AGENCY_BIN" -q show alert-triage >/dev/null
    "$AGENCY_BIN" -q show security-explorer >/dev/null
    ;;
  red-team)
    "$AGENCY_BIN" -q show red-team-coordinator >/dev/null
    "$AGENCY_BIN" -q show red-team-recon >/dev/null
    "$AGENCY_BIN" -q show red-team-exploit >/dev/null
    ;;
esac
echo "  ✓ upgraded pack resources are visible"

"$AGENCY_BIN" -q hub teardown "$PACK_NAME" --delete
echo "  ✓ upgraded pack teardown completed"
