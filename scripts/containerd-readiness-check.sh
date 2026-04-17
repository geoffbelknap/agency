#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
AGENCY_BIN="${AGENCY_BIN:-}"
SOURCE_HOME="${AGENCY_SOURCE_HOME:-$HOME/.agency}"
KEEP_HOME="${AGENCY_CONTAINERD_KEEP_HOME:-0}"
SEED_HOME=""
SOCKET_OVERRIDE="${AGENCY_CONTAINERD_SOCKET:-}"
BOOTSTRAPPED_HOME=0
BOOTSTRAP_PROVIDER="${AGENCY_CONTAINERD_SETUP_PROVIDER:-openai}"
BOOTSTRAP_API_KEY="${AGENCY_CONTAINERD_SETUP_API_KEY:-containerd-readiness-placeholder-key}"
GATEWAY_PORT="${AGENCY_CONTAINERD_GATEWAY_PORT:-18500}"
WEB_PORT="${AGENCY_CONTAINERD_WEB_PORT:-18580}"
PROXY_PORT="${AGENCY_CONTAINERD_GATEWAY_PROXY_PORT:-18502}"
PROXY_KNOWLEDGE_PORT="${AGENCY_CONTAINERD_GATEWAY_PROXY_KNOWLEDGE_PORT:-18504}"
PROXY_INTAKE_PORT="${AGENCY_CONTAINERD_GATEWAY_PROXY_INTAKE_PORT:-18505}"
KNOWLEDGE_PORT="${AGENCY_CONTAINERD_KNOWLEDGE_PORT:-18514}"
INTAKE_PORT="${AGENCY_CONTAINERD_INTAKE_PORT:-18515}"
WEB_FETCH_PORT="${AGENCY_CONTAINERD_WEB_FETCH_PORT:-18516}"
AGENT_NAME="containerd-readiness-$(date +%s)"

usage() {
  cat <<'EOF'
Usage: ./scripts/containerd-readiness-check.sh [--keep-home] [--source-home <path>] [--socket <uri>]

Runs a Linux-only readiness lane for the Agency `containerd` backend.
This lane expects a native containerd + nerdctl environment on Linux.

Options:
  --keep-home           Preserve the generated seed home
  --source-home <path>  Source Agency home to clone (default: ~/.agency)
  --socket <uri>        Override the compatibility socket URI
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

detect_containerd_socket() {
  if [ -n "$SOCKET_OVERRIDE" ]; then
    printf '%s\n' "$SOCKET_OVERRIDE"
    return 0
  fi
  if [ -n "${CONTAINERD_HOST:-}" ]; then
    printf '%s\n' "$CONTAINERD_HOST"
    return 0
  fi
  if [ -n "${CONTAINER_HOST:-}" ]; then
    printf '%s\n' "$CONTAINER_HOST"
    return 0
  fi
  if [ -S /run/containerd/containerd.sock ]; then
    printf '%s\n' "unix:///run/containerd/containerd.sock"
    return 0
  fi
  fail "Could not find a containerd socket for the containerd readiness lane"
}

create_seed_home() {
  SEED_HOME="$(python3 - <<'PY'
import tempfile
print(tempfile.mkdtemp(prefix="agency-containerd-seed.", dir="/tmp"))
PY
)"
  if [ -d "$SOURCE_HOME" ]; then
    cp -R "$SOURCE_HOME"/. "$SEED_HOME"/
    return 0
  fi
  bootstrap_seed_home
}

bootstrap_seed_home() {
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
  python3 - "$SEED_HOME/config.yaml" "$socket" "$gateway_addr" <<'PY'
import pathlib
import sys
import yaml

path = pathlib.Path(sys.argv[1])
socket = sys.argv[2]
gateway_addr = sys.argv[3]
data = {}
if path.exists():
    loaded = yaml.safe_load(path.read_text()) or {}
    if isinstance(loaded, dict):
        data = loaded
hub = data.get("hub")
if not isinstance(hub, dict):
    hub = {}
data["hub"] = hub
data["gateway_addr"] = gateway_addr
data["llm_provider"] = "openai"
hub["deployment_backend"] = "containerd"
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
    log "Keeping containerd seed home at $SEED_HOME"
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

[ "$(uname -s)" = "Linux" ] || fail "containerd readiness is Linux-only"
require_cmd nerdctl
require_cmd python3

if ! AGENCY_BIN="$(resolve_agency_bin)"; then
  fail "Could not resolve agency binary. Build it first."
fi

CONTAINERD_SOCKET="$(detect_containerd_socket)"
log "Using containerd socket for containerd backend: $CONTAINERD_SOCKET"

choose_ports
create_seed_home
write_routing_config
patch_seed_config "$CONTAINERD_SOCKET"

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

log "Restarting gateway on containerd-backed seed home"
"$AGENCY_BIN" serve restart >/dev/null

log "Ensuring shared infrastructure is up"
"$AGENCY_BIN" -q infra up >/dev/null

log "Creating disposable readiness agent: $AGENT_NAME"
"$AGENCY_BIN" -q create "$AGENT_NAME" >/dev/null
"$AGENCY_BIN" -q start "$AGENT_NAME" >/dev/null

log "Running runtime contract smoke"
CONFIG_PATH="$SEED_HOME/config.yaml" AGENT_NAME="$AGENT_NAME" bash "$ROOT_DIR/scripts/runtime-contract-smoke.sh" --agent "$AGENT_NAME" --skip-tests

log "Exercising lifecycle controls"
"$AGENCY_BIN" -q stop "$AGENT_NAME" >/dev/null
"$AGENCY_BIN" -q start "$AGENT_NAME" >/dev/null
"$AGENCY_BIN" -q restart "$AGENT_NAME" >/dev/null
"$AGENCY_BIN" -q halt "$AGENT_NAME" --tier supervised --reason "containerd readiness" >/dev/null
"$AGENCY_BIN" -q resume "$AGENT_NAME" >/dev/null

log "containerd readiness check passed"
