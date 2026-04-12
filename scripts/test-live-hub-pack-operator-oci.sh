#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
SOURCE_HOME="${AGENCY_SOURCE_HOME:-${HOME}/.agency}"
DISPOSABLE_HOME="${AGENCY_PACK_OCI_HOME:-}"
GATEWAY_PORT="${AGENCY_PACK_OCI_GATEWAY_PORT:-}"
KEEP_HOME="${AGENCY_PACK_OCI_KEEP_HOME:-0}"
PACK_NAME="${AGENCY_PACK_OCI_PACK_NAME:-security-ops}"

usage() {
  cat <<'EOF'
Usage: ./scripts/test-live-hub-pack-operator-oci.sh [--keep-home]

Runs a live Hub OCI pack install/deploy/teardown check against a disposable
Agency home and isolated gateway port.

The test verifies:
  - `agency -q hub update` syncs the OCI catalog
  - `agency -q hub install <pack> --kind pack --yes` installs the pack and dependencies
  - `agency -q deploy <pack>` deploys the pack from the installed hub instance
  - `agency -q teardown <pack> --delete` removes the deployed resources

Environment:
  AGENCY_SOURCE_HOME               Source Agency home to clone (default: ~/.agency)
  AGENCY_PACK_OCI_HOME             Disposable home path (default: mktemp)
  AGENCY_PACK_OCI_GATEWAY_PORT     Gateway host port (default: auto-selected free port)
  AGENCY_PACK_OCI_KEEP_HOME=1      Preserve disposable home after the run
  AGENCY_PACK_OCI_PACK_NAME        Pack name to exercise (default: security-ops)
  AGENCY_BIN                       Agency binary to test (default: repo ./agency, then PATH)
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
  DISPOSABLE_HOME="$(mktemp -d "${TMPDIR:-/tmp}/agency-pack-oci-home.XXXXXX")"
else
  mkdir -p "$DISPOSABLE_HOME"
fi

if [ -z "$GATEWAY_PORT" ]; then
  GATEWAY_PORT="$(python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"
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
export AGENCY_PACK_OCI_GATEWAY_PORT="$GATEWAY_PORT"

CONFIG_PATH="$AGENCY_HOME/config.yaml"
if [ -f "$CONFIG_PATH" ]; then
  grep -Ev '^(gateway_addr|token):' "$CONFIG_PATH" > "$CONFIG_PATH.tmp" || true
else
  : > "$CONFIG_PATH.tmp"
fi
printf 'gateway_addr: "127.0.0.1:%s"\n' "$AGENCY_PACK_OCI_GATEWAY_PORT" >> "$CONFIG_PATH.tmp"
if [ -f "$CONFIG_PATH" ] && grep -Eq '^token:' "$CONFIG_PATH"; then
  grep -E '^token:' "$CONFIG_PATH" >> "$CONFIG_PATH.tmp"
else
  printf 'token: "agency-pack-oci-live-check-token"\n' >> "$CONFIG_PATH.tmp"
fi
mv "$CONFIG_PATH.tmp" "$CONFIG_PATH"

echo "==> Disposable Agency home: $DISPOSABLE_HOME"
echo "==> Gateway port:           $GATEWAY_PORT"
echo "==> Agency binary:          $AGENCY_BIN"
echo "==> Pack under test:        $PACK_NAME"

"$AGENCY_BIN" -q hub update
"$AGENCY_BIN" -q hub install "$PACK_NAME" --kind pack --yes

pack_info="$("$AGENCY_BIN" -q hub show "$PACK_NAME")"
printf '%s\n' "$pack_info" | grep -Eq "kind:[[:space:]]+pack|\"kind\":[[:space:]]*\"pack\"" ||
  { echo "expected installed pack info for $PACK_NAME" >&2; printf '%s\n' "$pack_info" >&2; exit 1; }
echo "  ✓ pack is installed in hub registry"

"$AGENCY_BIN" -q hub deploy "$PACK_NAME"
echo "  ✓ pack deploy completed"

case "$PACK_NAME" in
  security-ops)
    "$AGENCY_BIN" -q show alert-triage >/dev/null
    "$AGENCY_BIN" -q show security-explorer >/dev/null
    "$AGENCY_BIN" -q team show security-ops >/dev/null
    ;;
  red-team)
    "$AGENCY_BIN" -q show red-team-coordinator >/dev/null
    "$AGENCY_BIN" -q show red-team-recon >/dev/null
    "$AGENCY_BIN" -q show red-team-exploit >/dev/null
    "$AGENCY_BIN" -q team show red-team >/dev/null
    ;;
esac
echo "  ✓ deployed resources are visible"

"$AGENCY_BIN" -q hub teardown "$PACK_NAME" --delete
echo "  ✓ pack teardown completed"

case "$PACK_NAME" in
  security-ops)
    if "$AGENCY_BIN" -q show alert-triage >/dev/null 2>&1; then
      echo "alert-triage still exists after teardown" >&2
      exit 1
    fi
    if "$AGENCY_BIN" -q show security-explorer >/dev/null 2>&1; then
      echo "security-explorer still exists after teardown" >&2
      exit 1
    fi
    ;;
  red-team)
    if "$AGENCY_BIN" -q show red-team-coordinator >/dev/null 2>&1; then
      echo "red-team-coordinator still exists after teardown" >&2
      exit 1
    fi
    ;;
esac
echo "  ✓ deployed agents are gone after teardown"
