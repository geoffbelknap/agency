#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
AGENCY_BIN="${AGENCY_BIN:-}"
SOURCE_HOME="${AGENCY_SOURCE_HOME:-$HOME/.agency}"
KEEP_HOME="${AGENCY_PODMAN_KEEP_HOME:-0}"
RUN_FULL_E2E=0
SEED_HOME=""
BOOTSTRAPPED_HOME=0
SOCKET_OVERRIDE="${AGENCY_PODMAN_SOCKET:-}"
BOOTSTRAP_PROVIDER="${AGENCY_PODMAN_SETUP_PROVIDER:-}"
GATEWAY_PORT="${AGENCY_PODMAN_GATEWAY_PORT:-18400}"
WEB_PORT="${AGENCY_PODMAN_WEB_PORT:-18480}"
PROXY_PORT="${AGENCY_PODMAN_GATEWAY_PROXY_PORT:-18402}"
PROXY_KNOWLEDGE_PORT="${AGENCY_PODMAN_GATEWAY_PROXY_KNOWLEDGE_PORT:-18404}"
PROXY_INTAKE_PORT="${AGENCY_PODMAN_GATEWAY_PROXY_INTAKE_PORT:-18405}"
KNOWLEDGE_PORT="${AGENCY_PODMAN_KNOWLEDGE_PORT:-18414}"
INTAKE_PORT="${AGENCY_PODMAN_INTAKE_PORT:-18415}"
WEB_FETCH_PORT="${AGENCY_PODMAN_WEB_FETCH_PORT:-18416}"
GATEWAY_START_TIMEOUT="${AGENCY_PODMAN_GATEWAY_START_TIMEOUT:-180}"
INFRA_UP_ATTEMPTS="${AGENCY_PODMAN_INFRA_UP_ATTEMPTS:-3}"
INFRA_UP_RETRY_DELAY="${AGENCY_PODMAN_INFRA_UP_RETRY_DELAY:-5}"
AGENT_NAME="podman-readiness-$(date +%s)"

usage() {
  cat <<'EOF'
Usage: ./scripts/readiness/podman-readiness-check.sh [--full] [--keep-home] [--source-home <path>] [--socket <uri>]

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

port_in_use() {
  python3 -c 'import socket,sys; s=socket.socket(); s.settimeout(0.2); code=s.connect_ex(("127.0.0.1", int(sys.argv[1]))); s.close(); raise SystemExit(0 if code == 0 else 1)' "$1"
}

pick_free_port() {
  python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()'
}

gateway_health_ok() {
  python3 - "$1" <<'PY'
import sys
import urllib.request

url = sys.argv[1]
try:
    with urllib.request.urlopen(url, timeout=2) as resp:
        raise SystemExit(0 if resp.status == 200 else 1)
except Exception:
    raise SystemExit(1)
PY
}

start_gateway() {
  local health_url="http://127.0.0.1:${GATEWAY_PORT}/api/v1/health"
  local deadline=$((SECONDS + GATEWAY_START_TIMEOUT))

  "$AGENCY_BIN" serve stop >/dev/null 2>&1 || true
  nohup "$AGENCY_BIN" serve >>"$SEED_HOME/gateway.log" 2>&1 &

  while [ "$SECONDS" -lt "$deadline" ]; do
    if gateway_health_ok "$health_url"; then
      return 0
    fi
    sleep 1
  done

  tail -n 80 "$SEED_HOME/gateway.log" >&2 || true
  fail "gateway did not become healthy within ${GATEWAY_START_TIMEOUT}s; check $SEED_HOME/gateway.log"
}

run_with_retries() {
  local attempts="$1"
  local delay="$2"
  shift 2
  local try=1
  while true; do
    if "$@"; then
      return 0
    fi
    if [ "$try" -ge "$attempts" ]; then
      return 1
    fi
    log "Command failed; retrying (${try}/${attempts}) in ${delay}s: $*"
    sleep "$delay"
    try=$((try + 1))
  done
}

sanitize_instance() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9-]+/-/g; s/^-+//; s/-+$//'
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
  BOOTSTRAPPED_HOME=1
  mkdir -p \
    "$SEED_HOME/agents" \
    "$SEED_HOME/teams" \
    "$SEED_HOME/departments" \
    "$SEED_HOME/connectors" \
    "$SEED_HOME/hub" \
    "$SEED_HOME/profiles" \
    "$SEED_HOME/registry/services" \
    "$SEED_HOME/registry/mcp-servers" \
    "$SEED_HOME/registry/skills" \
    "$SEED_HOME/infrastructure/comms/data/channels" \
    "$SEED_HOME/infrastructure/comms/data/cursors" \
    "$SEED_HOME/infrastructure/egress/certs" \
    "$SEED_HOME/infrastructure/egress/blocklists" \
    "$SEED_HOME/run" \
    "$SEED_HOME/knowledge/ontology.d"
  mkdir -m 700 -p "$SEED_HOME/audit"
  cat >"$SEED_HOME/capacity.yaml" <<'EOF'
host_memory_mb: 8192
host_cpu_cores: 4
system_reserve_mb: 2048
infra_overhead_mb: 1264
max_agents: 4
max_concurrent_meesks: 4
agent_slot_mb: 640
meeseeks_slot_mb: 640
network_pool_configured: false
EOF
}

patch_seed_config() {
  local socket="$1"
  local gateway_addr="127.0.0.1:${GATEWAY_PORT}"
  ruby -e '
    require "securerandom"
    require "yaml"
    path = ARGV[0]
    socket = ARGV[1]
    gateway_addr = ARGV[2]
    provider = ARGV[3]
    data = File.exist?(path) ? (YAML.load_file(path) || {}) : {}
    data["token"] ||= SecureRandom.hex(32)
    data["egress_token"] ||= SecureRandom.hex(32)
    if !data["llm_provider"] && provider.to_s.empty?
      raise "missing llm_provider; set AGENCY_PODMAN_SETUP_PROVIDER for disposable bootstrap"
    end
    data["llm_provider"] ||= provider
    data["hub"] ||= {}
    data["gateway_addr"] = gateway_addr
    data["hub"]["deployment_backend"] = "podman"
    data["hub"]["deployment_backend_config"] = {"host" => socket}
    File.write(path, YAML.dump(data))
  ' "$SEED_HOME/config.yaml" "$socket" "$gateway_addr" "$BOOTSTRAP_PROVIDER"
}

choose_ports() {
  local var_name
  for var_name in GATEWAY_PORT WEB_PORT PROXY_PORT PROXY_KNOWLEDGE_PORT PROXY_INTAKE_PORT KNOWLEDGE_PORT INTAKE_PORT WEB_FETCH_PORT; do
    local port="${!var_name}"
    if port_in_use "$port"; then
      printf -v "$var_name" '%s' "$(pick_free_port)"
    fi
  done
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

choose_ports
create_seed_home
patch_seed_config "$PODMAN_SOCKET"

log "Seed home: $SEED_HOME"

export AGENCY_HOME="$SEED_HOME"
export AGENCY_BIN
export AGENCY_INFRA_INSTANCE="$(sanitize_instance "$(basename "$SEED_HOME")")"
export AGENCY_GATEWAY_PROXY_PORT="$PROXY_PORT"
export AGENCY_GATEWAY_PROXY_KNOWLEDGE_PORT="$PROXY_KNOWLEDGE_PORT"
export AGENCY_GATEWAY_PROXY_INTAKE_PORT="$PROXY_INTAKE_PORT"
export AGENCY_KNOWLEDGE_PORT="$KNOWLEDGE_PORT"
export AGENCY_INTAKE_PORT="$INTAKE_PORT"
export AGENCY_WEB_FETCH_PORT="$WEB_FETCH_PORT"
export AGENCY_WEB_PORT="$WEB_PORT"
log "Restarting gateway on Podman-backed seed home"
start_gateway

log "Ensuring shared infrastructure is up"
run_with_retries "$INFRA_UP_ATTEMPTS" "$INFRA_UP_RETRY_DELAY" "$AGENCY_BIN" -q infra up >/dev/null

log "Creating disposable readiness agent: $AGENT_NAME"
"$AGENCY_BIN" -q create "$AGENT_NAME" >/dev/null
"$AGENCY_BIN" -q start "$AGENT_NAME" >/dev/null

log "Running Podman runtime smoke"
CONFIG_PATH="$SEED_HOME/config.yaml" AGENT_NAME="$AGENT_NAME" bash "$ROOT_DIR/scripts/readiness/runtime-contract-smoke.sh" --agent "$AGENT_NAME" --skip-tests

if [ "$RUN_FULL_E2E" = "1" ]; then
  log "Running full Podman disposable E2E"
  AGENCY_SOURCE_HOME="$SEED_HOME" "$ROOT_DIR/scripts/e2e/e2e-live-disposable.sh" --skip-build
fi

log "Podman readiness check passed"
