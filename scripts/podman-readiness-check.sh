#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
AGENCY_BIN="${AGENCY_BIN:-}"
SOURCE_HOME="${AGENCY_SOURCE_HOME:-$HOME/.agency}"
KEEP_HOME="${AGENCY_PODMAN_KEEP_HOME:-0}"
RUN_FULL_E2E=0
SEED_HOME=""
SOCKET_OVERRIDE="${AGENCY_PODMAN_SOCKET:-}"
BOOTSTRAP_PROVIDER="${AGENCY_PODMAN_SETUP_PROVIDER:-openai}"
BOOTSTRAP_API_KEY="${AGENCY_PODMAN_SETUP_API_KEY:-podman-readiness-placeholder-key}"
AGENT_NAME="podman-readiness-$(date +%s)"

usage() {
  cat <<'EOF'
Usage: ./scripts/podman-readiness-check.sh [--full] [--keep-home] [--source-home <path>] [--socket <uri>]

Runs a Podman-backed readiness lane by creating a disposable Agency home,
forcing `hub.deployment_backend=podman`, exercising the runtime smoke path,
and optionally running the full disposable live web E2E.

Options:
  --full                Run the full disposable Playwright suite after smoke
  --keep-home           Preserve the generated Podman seed home
  --source-home <path>  Source Agency home to clone (default: ~/.agency; bootstraps one if missing)
  --socket <uri>        Override the Podman socket URI
  -h, --help            Show this help
EOF
}

log() {
  printf '==> %s\n' "$*"
}

fail() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "Missing required command: $1"
}

resolve_agency_bin() {
  if [ -n "$AGENCY_BIN" ] && [ -x "$AGENCY_BIN" ]; then
    printf '%s\n' "$AGENCY_BIN"
    return 0
  fi
  if [ -x "$ROOT_DIR/agency" ]; then
    printf '%s\n' "$ROOT_DIR/agency"
    return 0
  fi
  if command -v agency >/dev/null 2>&1; then
    command -v agency
    return 0
  fi
  return 1
}

detect_podman_socket() {
  if [ -n "$SOCKET_OVERRIDE" ]; then
    printf '%s\n' "$SOCKET_OVERRIDE"
    return 0
  fi

  python3 - <<'PY'
import json
import subprocess
import sys

proc = subprocess.run(
    ["podman", "info", "--format", "json"],
    capture_output=True,
    text=True,
)
if proc.returncode != 0:
    sys.stderr.write(proc.stderr)
    raise SystemExit(proc.returncode)

data = json.loads(proc.stdout)
path = (
    data.get("host", {})
    .get("remoteSocket", {})
    .get("path")
)
if not path:
    raise SystemExit("Podman remote socket path not found in `podman info`")
print(path)
PY
}

create_seed_home() {
  SEED_HOME="$(python3 - <<'PY'
import tempfile
print(tempfile.mkdtemp(prefix="agency-podman-seed.", dir="/tmp"))
PY
)"
  if [ -d "$SOURCE_HOME" ]; then
    cp -R "$SOURCE_HOME"/. "$SEED_HOME"/
    return 0
  fi
  bootstrap_seed_home
}

bootstrap_seed_home() {
  log "Source Agency home not found at $SOURCE_HOME; bootstrapping disposable setup"
  AGENCY_HOME="$SEED_HOME" \
  AGENCY_NO_BROWSER=1 \
  "$AGENCY_BIN" -q setup \
    --provider "$BOOTSTRAP_PROVIDER" \
    --api-key "$BOOTSTRAP_API_KEY" \
    --no-browser \
    --no-infra >/dev/null
}

patch_seed_config() {
  local socket="$1"
  ruby -e '
    require "yaml"
    path = ARGV[0]
    socket = ARGV[1]
    data = YAML.load_file(path) || {}
    data["hub"] ||= {}
    data["hub"]["deployment_backend"] = "podman"
    data["hub"]["deployment_backend_config"] = {"host" => socket}
    File.write(path, YAML.dump(data))
  ' "$SEED_HOME/config.yaml" "$socket"
}

cleanup() {
  set +e
  if [ -n "$SEED_HOME" ] && [ -x "${AGENCY_BIN:-}" ]; then
    AGENCY_HOME="$SEED_HOME" "$AGENCY_BIN" -q delete "$AGENT_NAME" >/dev/null 2>&1 || true
    AGENCY_HOME="$SEED_HOME" "$AGENCY_BIN" serve stop >/dev/null 2>&1 || true
    AGENCY_HOME="$SEED_HOME" "$AGENCY_BIN" -q infra down >/dev/null 2>&1 || true
  fi
  if [ -n "$SEED_HOME" ] && [ "$KEEP_HOME" != "1" ]; then
    rm -rf "$SEED_HOME"
  elif [ -n "$SEED_HOME" ]; then
    log "Keeping Podman seed home at $SEED_HOME"
  fi
}
trap cleanup EXIT INT TERM HUP

while [ "$#" -gt 0 ]; do
  case "$1" in
    --full)
      RUN_FULL_E2E=1
      shift
      ;;
    --keep-home)
      KEEP_HOME=1
      shift
      ;;
    --source-home)
      [ "$#" -ge 2 ] || fail "--source-home requires a path"
      SOURCE_HOME="$2"
      shift 2
      ;;
    --socket)
      [ "$#" -ge 2 ] || fail "--socket requires a URI"
      SOCKET_OVERRIDE="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      fail "unknown argument: $1"
      ;;
  esac
done

require_cmd podman
require_cmd python3
require_cmd ruby

if ! AGENCY_BIN="$(resolve_agency_bin)"; then
  fail "Could not resolve agency binary. Build it first."
fi

PODMAN_SOCKET="$(detect_podman_socket)"
log "Using Podman socket: $PODMAN_SOCKET"

create_seed_home
patch_seed_config "$PODMAN_SOCKET"

log "Seed home: $SEED_HOME"

export AGENCY_HOME="$SEED_HOME"
export AGENCY_BIN

log "Restarting gateway on Podman-backed seed home"
"$AGENCY_BIN" serve restart >/dev/null

log "Ensuring shared infrastructure is up"
"$AGENCY_BIN" -q infra up >/dev/null

log "Creating disposable readiness agent: $AGENT_NAME"
"$AGENCY_BIN" -q create "$AGENT_NAME" >/dev/null
"$AGENCY_BIN" -q start "$AGENT_NAME" >/dev/null

log "Running Podman runtime smoke"
CONFIG_PATH="$SEED_HOME/config.yaml" AGENT_NAME="$AGENT_NAME" bash "$ROOT_DIR/scripts/runtime-contract-smoke.sh" --agent "$AGENT_NAME" --skip-tests

if [ "$RUN_FULL_E2E" = "1" ]; then
  log "Running full Podman disposable E2E"
  AGENCY_SOURCE_HOME="$SEED_HOME" "$ROOT_DIR/scripts/e2e-live-disposable.sh" --skip-build
fi

log "Podman readiness check passed"
