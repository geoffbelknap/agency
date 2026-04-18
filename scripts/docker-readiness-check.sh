#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
AGENCY_BIN="${AGENCY_BIN:-}"
SOURCE_HOME="${AGENCY_SOURCE_HOME:-$HOME/.agency}"
KEEP_HOME="${AGENCY_DOCKER_KEEP_HOME:-0}"
SEED_HOME=""
BOOTSTRAPPED_HOME=0
SOCKET_OVERRIDE="${AGENCY_DOCKER_SOCKET:-}"
BOOTSTRAP_PROVIDER="${AGENCY_DOCKER_SETUP_PROVIDER:-openai}"
BOOTSTRAP_API_KEY="${AGENCY_DOCKER_SETUP_API_KEY:-docker-readiness-placeholder-key}"
GATEWAY_PORT="${AGENCY_DOCKER_GATEWAY_PORT:-18300}"
WEB_PORT="${AGENCY_DOCKER_WEB_PORT:-18380}"
PROXY_PORT="${AGENCY_DOCKER_GATEWAY_PROXY_PORT:-18302}"
PROXY_KNOWLEDGE_PORT="${AGENCY_DOCKER_GATEWAY_PROXY_KNOWLEDGE_PORT:-18304}"
PROXY_INTAKE_PORT="${AGENCY_DOCKER_GATEWAY_PROXY_INTAKE_PORT:-18305}"
KNOWLEDGE_PORT="${AGENCY_DOCKER_KNOWLEDGE_PORT:-18314}"
INTAKE_PORT="${AGENCY_DOCKER_INTAKE_PORT:-18315}"
WEB_FETCH_PORT="${AGENCY_DOCKER_WEB_FETCH_PORT:-18316}"
GATEWAY_START_TIMEOUT="${AGENCY_DOCKER_GATEWAY_START_TIMEOUT:-180}"
INFRA_UP_ATTEMPTS="${AGENCY_DOCKER_INFRA_UP_ATTEMPTS:-3}"
INFRA_UP_RETRY_DELAY="${AGENCY_DOCKER_INFRA_UP_RETRY_DELAY:-5}"
AGENT_NAME="docker-readiness-$(date +%s)"

usage() {
  cat <<'EOF'
Usage: ./scripts/docker-readiness-check.sh [--keep-home] [--source-home <path>] [--socket <uri>]

Runs a Docker-backed readiness lane by creating a disposable Agency home,
forcing hub.deployment_backend=docker, exercising the runtime smoke path,
and verifying the backend-neutral Docker contract on the selected socket.

Options:
  --keep-home           Preserve the generated Docker seed home
  --source-home <path>  Source Agency home to clone (default: ~/.agency; bootstraps one if missing)
  --socket <uri>        Override the Docker socket URI
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

provider_env_var() {
  case "$1" in
    anthropic)
      printf '%s\n' "ANTHROPIC_API_KEY"
      ;;
    google)
      printf '%s\n' "GEMINI_API_KEY"
      ;;
    *)
      printf '%s\n' "OPENAI_API_KEY"
      ;;
  esac
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

detect_docker_socket() {
  if [ -n "$SOCKET_OVERRIDE" ]; then
    printf '%s\n' "$SOCKET_OVERRIDE"
    return 0
  fi
  if [ -n "${DOCKER_HOST:-}" ]; then
    printf '%s\n' "$DOCKER_HOST"
    return 0
  fi
  if docker context inspect >/dev/null 2>&1; then
    local context_host
    context_host="$(docker context inspect --format '{{ (index .Endpoints "docker").Host }}' 2>/dev/null || true)"
    context_host="${context_host//<no value>/}"
    context_host="$(printf '%s' "$context_host" | tr -d '\r')"
    if [ -n "$context_host" ]; then
      printf '%s\n' "$context_host"
      return 0
    fi
  fi
  if [ -S "$HOME/.docker/run/docker.sock" ]; then
    printf 'unix://%s\n' "$HOME/.docker/run/docker.sock"
    return 0
  fi
  if [ -S /var/run/docker.sock ]; then
    printf '%s\n' "unix:///var/run/docker.sock"
    return 0
  fi
  fail "Could not find a Docker socket for the Docker readiness lane"
}

create_seed_home() {
  SEED_HOME="$(python3 - <<'PY'
import tempfile
print(tempfile.mkdtemp(prefix="agency-docker-seed.", dir="/tmp"))
PY
)"
  if [ -d "$SOURCE_HOME" ]; then
    cp -R "$SOURCE_HOME"/. "$SEED_HOME"/
    purge_synthetic_readiness_agents
    return 0
  fi
  bootstrap_seed_home
}

purge_synthetic_readiness_agents() {
  local agents_dir="$SEED_HOME/agents"
  [ -d "$agents_dir" ] || return 0
  find "$agents_dir" -mindepth 1 -maxdepth 1 -type d \
    \( -name 'containerd-readiness-*' -o -name 'podman-readiness-*' -o -name 'docker-readiness-*' \) \
    -exec rm -rf {} +
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
  write_routing_config
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

write_routing_config() {
  cat >"$SEED_HOME/infrastructure/routing.yaml" <<'EOF'
version: "0.1"
providers:
  anthropic:
    api_base: https://api.anthropic.com/v1
    auth_env: ANTHROPIC_API_KEY
    auth_header: x-api-key
    auth_prefix: ""
  openai:
    api_base: https://api.openai.com/v1
    auth_env: OPENAI_API_KEY
    auth_header: Authorization
    auth_prefix: "Bearer "
models:
  claude-sonnet:
    provider: anthropic
    provider_model: claude-sonnet-4-20250514
  claude-haiku:
    provider: anthropic
    provider_model: claude-haiku-4-5-20251001
  gpt-4o:
    provider: openai
    provider_model: gpt-4o
  gpt-4o-mini:
    provider: openai
    provider_model: gpt-4o-mini
tiers:
  standard:
    - model: claude-sonnet
      preference: 0
    - model: gpt-4o
      preference: 1
  mini:
    - model: claude-haiku
      preference: 0
    - model: gpt-4o-mini
      preference: 1
settings:
  default_tier: standard
EOF
}

patch_seed_config() {
  local socket="$1"
  local gateway_addr="127.0.0.1:${GATEWAY_PORT}"
  python3 - "$SEED_HOME/config.yaml" "$socket" "$gateway_addr" "$BOOTSTRAP_PROVIDER" <<'PY'
import pathlib
import secrets
import sys
import yaml

path = pathlib.Path(sys.argv[1])
socket = sys.argv[2]
gateway_addr = sys.argv[3]
provider = sys.argv[4]
data = {}
if path.exists():
    loaded = yaml.safe_load(path.read_text()) or {}
    if isinstance(loaded, dict):
        data = loaded
hub = data.get("hub")
if not isinstance(hub, dict):
    hub = {}
data["hub"] = hub
data["token"] = data.get("token") or secrets.token_hex(32)
data["egress_token"] = data.get("egress_token") or secrets.token_hex(32)
data["gateway_addr"] = gateway_addr
data["llm_provider"] = data.get("llm_provider") or provider
hub["deployment_backend"] = "docker"
hub["deployment_backend_config"] = {"host": socket}
path.write_text(yaml.safe_dump(data, sort_keys=False))
PY
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
    log "Keeping Docker seed home at $SEED_HOME"
  fi
}
trap cleanup EXIT INT TERM HUP

while [ "$#" -gt 0 ]; do
  case "$1" in
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

require_cmd docker
require_cmd python3

if ! AGENCY_BIN="$(resolve_agency_bin)"; then
  fail "Could not resolve agency binary. Build it first."
fi

DOCKER_SOCKET="$(detect_docker_socket)"
log "Using Docker socket: $DOCKER_SOCKET"

choose_ports
create_seed_home
write_routing_config
patch_seed_config "$DOCKER_SOCKET"

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
if [ "$BOOTSTRAPPED_HOME" = "1" ]; then
  provider_env="$(provider_env_var "$BOOTSTRAP_PROVIDER")"
  if [ -z "${!provider_env:-}" ]; then
    export "$provider_env=$BOOTSTRAP_API_KEY"
  fi
fi
if [ -z "${OPENAI_API_KEY:-}" ] && [ -z "${ANTHROPIC_API_KEY:-}" ] && [ -z "${GEMINI_API_KEY:-}" ]; then
  export OPENAI_API_KEY="$BOOTSTRAP_API_KEY"
fi

log "Restarting gateway on Docker-backed seed home"
start_gateway

log "Ensuring shared infrastructure is up"
run_with_retries "$INFRA_UP_ATTEMPTS" "$INFRA_UP_RETRY_DELAY" "$AGENCY_BIN" -q infra up >/dev/null

log "Creating disposable readiness agent: $AGENT_NAME"
"$AGENCY_BIN" -q create "$AGENT_NAME" >/dev/null
"$AGENCY_BIN" -q start "$AGENT_NAME" >/dev/null

log "Running Docker runtime smoke"
CONFIG_PATH="$SEED_HOME/config.yaml" AGENT_NAME="$AGENT_NAME" bash "$ROOT_DIR/scripts/runtime-contract-smoke.sh" --agent "$AGENT_NAME" --skip-tests

log "Asserting reported Docker backend endpoint"
runtime_status_json="$("$AGENCY_BIN" -q runtime status "$AGENT_NAME")"
python3 - "$DOCKER_SOCKET" "$runtime_status_json" <<'PY'
import json
import sys

expected_endpoint = sys.argv[1]
body = json.loads(sys.argv[2])

if body.get("backend") != "docker":
    raise SystemExit(f"unexpected backend in runtime status: {body.get('backend')!r}")
endpoint = body.get("backendEndpoint", "")
if endpoint != expected_endpoint:
    raise SystemExit(f"unexpected backendEndpoint in runtime status: {endpoint!r}")
PY

log "Exercising lifecycle controls"
"$AGENCY_BIN" -q stop "$AGENT_NAME" >/dev/null
"$AGENCY_BIN" -q start "$AGENT_NAME" >/dev/null
"$AGENCY_BIN" -q restart "$AGENT_NAME" >/dev/null
"$AGENCY_BIN" -q halt "$AGENT_NAME" --tier supervised --reason "docker readiness" >/dev/null
"$AGENCY_BIN" -q resume "$AGENT_NAME" >/dev/null

log "Docker readiness check passed"
