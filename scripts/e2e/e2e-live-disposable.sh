#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
SOURCE_HOME="${AGENCY_SOURCE_HOME:-${HOME}/.agency}"
DISPOSABLE_HOME="${AGENCY_DISPOSABLE_HOME:-}"
GATEWAY_PORT="${AGENCY_DISPOSABLE_GATEWAY_PORT:-18200}"
WEB_PORT="${AGENCY_DISPOSABLE_WEB_PORT:-18280}"
PROXY_PORT="${AGENCY_DISPOSABLE_GATEWAY_PROXY_PORT:-18202}"
PROXY_KNOWLEDGE_PORT="${AGENCY_DISPOSABLE_GATEWAY_PROXY_KNOWLEDGE_PORT:-18204}"
PROXY_INTAKE_PORT="${AGENCY_DISPOSABLE_GATEWAY_PROXY_INTAKE_PORT:-18205}"
KNOWLEDGE_PORT="${AGENCY_DISPOSABLE_KNOWLEDGE_PORT:-18214}"
INTAKE_PORT="${AGENCY_DISPOSABLE_INTAKE_PORT:-18215}"
WEB_FETCH_PORT="${AGENCY_DISPOSABLE_WEB_FETCH_PORT:-18216}"
EGRESS_PORT="${AGENCY_DISPOSABLE_EGRESS_PROXY_PORT:-8312}"
KEEP_HOME="${AGENCY_DISPOSABLE_KEEP_HOME:-0}"
SKIP_BUILD="${AGENCY_E2E_SKIP_BUILD:-0}"
PLAYWRIGHT_CONFIG="${AGENCY_PLAYWRIGHT_CONFIG:-playwright.live.config.ts}"
RUNTIME_BACKEND="${AGENCY_RUNTIME_BACKEND:-}"
ROOTFS_OCI_REF="${AGENCY_MICROVM_ROOTFS_OCI_REF:-}"
ENFORCER_OCI_REF="${AGENCY_MICROVM_ENFORCER_OCI_REF:-}"
HOST_ENFORCER_BIN="${AGENCY_MICROAGENT_ENFORCER_BIN:-$ROOT_DIR/bin/agency-enforcer-host}"
MOCK_LLM="${AGENCY_DISPOSABLE_MOCK_LLM:-0}"
MOCK_LLM_PORT="${AGENCY_DISPOSABLE_MOCK_LLM_PORT:-}"
MOCK_LLM_PID=""

usage() {
  cat <<'EOF'
Usage: ./scripts/e2e/e2e-live-disposable.sh [options] [playwright test filters...]

Creates an isolated temporary Agency home, rewrites gateway/web host ports,
starts the disposable stack, and runs the live-safe suite by default.

Options:
  --risky            Run the live-risky suite instead of live-safe
  --config <path>    Playwright config file relative to web/
  --backend <name>   Runtime backend for the disposable home
  --rootfs-oci-ref <ref>
                    Rootfs OCI ref for microVM-backed runs
  --enforcer-oci-ref <ref>
                    Enforcer OCI ref for microVM-backed runs
  --mock-llm        Start a local OpenAI-compatible smoke LLM endpoint
  --keep-home        Preserve the disposable Agency home after the run
  --skip-build       Reuse the current local Agency binary and images
  -h, --help         Show this help

Environment:
  AGENCY_SOURCE_HOME             Source Agency home to clone (default: ~/.agency)
  AGENCY_DISPOSABLE_HOME         Target disposable home (default: mktemp)
  AGENCY_DISPOSABLE_GATEWAY_PORT Gateway host port for disposable stack (default: 18200)
  AGENCY_DISPOSABLE_WEB_PORT     Web host port for disposable stack (default: 18280)
  AGENCY_DISPOSABLE_GATEWAY_PROXY_PORT           Gateway-proxy host port for :8202 (default: 18202)
  AGENCY_DISPOSABLE_GATEWAY_PROXY_KNOWLEDGE_PORT Gateway-proxy host port for :8204 (default: 18204)
  AGENCY_DISPOSABLE_GATEWAY_PROXY_INTAKE_PORT    Gateway-proxy host port for :8205 (default: 18205)
  AGENCY_DISPOSABLE_KNOWLEDGE_PORT               Knowledge host port (default: 18214)
  AGENCY_DISPOSABLE_INTAKE_PORT                  Intake host port (default: 18215)
  AGENCY_DISPOSABLE_WEB_FETCH_PORT               Web-fetch host port (default: 18216)
  AGENCY_DISPOSABLE_EGRESS_PROXY_PORT            Egress proxy host port (default: 8312)
  AGENCY_DISPOSABLE_KEEP_HOME=1  Keep disposable home after the run
  AGENCY_RUNTIME_BACKEND          Runtime backend for the disposable home
  AGENCY_MICROVM_ROOTFS_OCI_REF   Rootfs OCI ref for microVM-backed runs
  AGENCY_MICROVM_ENFORCER_OCI_REF Enforcer OCI ref for microVM-backed runs
  AGENCY_DISPOSABLE_MOCK_LLM=1    Start a local smoke LLM endpoint
EOF
}

PLAYWRIGHT_ARGS=()
while [ "$#" -gt 0 ]; do
  case "$1" in
    --risky)
      PLAYWRIGHT_CONFIG="playwright.live.risky.config.ts"
      shift
      ;;
    --config)
      if [ "$#" -lt 2 ]; then
        echo "--config requires a path"
        exit 1
      fi
      PLAYWRIGHT_CONFIG="$2"
      shift 2
      ;;
    --backend)
      if [ "$#" -lt 2 ]; then
        echo "--backend requires a value"
        exit 1
      fi
      RUNTIME_BACKEND="$2"
      shift 2
      ;;
    --rootfs-oci-ref)
      if [ "$#" -lt 2 ]; then
        echo "--rootfs-oci-ref requires a value"
        exit 1
      fi
      ROOTFS_OCI_REF="$2"
      shift 2
      ;;
    --enforcer-oci-ref)
      if [ "$#" -lt 2 ]; then
        echo "--enforcer-oci-ref requires a value"
        exit 1
      fi
      ENFORCER_OCI_REF="$2"
      shift 2
      ;;
    --mock-llm)
      MOCK_LLM=1
      shift
      ;;
    --keep-home)
      KEEP_HOME=1
      shift
      ;;
    --skip-build)
      SKIP_BUILD=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --)
      shift
      while [ "$#" -gt 0 ]; do
        PLAYWRIGHT_ARGS+=("$1")
        shift
      done
      ;;
    *)
      PLAYWRIGHT_ARGS+=("$1")
      shift
      ;;
  esac
done

if [ "$PLAYWRIGHT_CONFIG" = "playwright.live.danger.config.ts" ]; then
  echo "Use ./scripts/e2e/e2e-live-danger-disposable.sh for live-danger."
  exit 1
fi

port_in_use() {
  python3 -c 'import socket,sys; s=socket.socket(); s.settimeout(0.2); code=s.connect_ex(("127.0.0.1", int(sys.argv[1]))); s.close(); raise SystemExit(0 if code == 0 else 1)' "$1"
}

pick_free_port() {
  python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()'
}

start_mock_llm() {
  if [ "$MOCK_LLM" != "1" ]; then
    return 0
  fi
  if [ -z "$MOCK_LLM_PORT" ]; then
    MOCK_LLM_PORT="$(pick_free_port)"
  fi
  export SMOKE_API_KEY="${SMOKE_API_KEY:-agency-disposable-smoke-key}"
  python3 - "$MOCK_LLM_PORT" >"$DISPOSABLE_HOME/mock-llm.log" 2>&1 <<'PY' &
import http.server
import json
import re
import socketserver
import sys

port = int(sys.argv[1])

class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        return

    def do_GET(self):
        if self.path == "/health":
            self.send_response(200)
            self.end_headers()
            self.wfile.write(b"ok")
            return
        self.send_response(404)
        self.end_headers()

    def do_POST(self):
        length = int(self.headers.get("content-length", "0"))
        body = self.rfile.read(length) if length else b"{}"
        try:
            payload = json.loads(body)
        except Exception:
            payload = {}
        messages = payload.get("messages") or []
        prompt = ""
        for message in reversed(messages):
            if message.get("role") == "user":
                content = message.get("content")
                if isinstance(content, str):
                    prompt = content
                elif isinstance(content, list):
                    prompt = " ".join(str(part.get("text", "")) for part in content if isinstance(part, dict))
                break
        match = re.search(r"Reply with exactly this token and nothing else:\s*([A-Za-z0-9_.:-]+)", prompt)
        text = match.group(1) if match else "ok"
        model = payload.get("model") or "smoke-model"
        self.send_response(200)
        if payload.get("stream"):
            self.send_header("content-type", "text/event-stream")
            self.end_headers()
            chunk = {"choices": [{"delta": {"content": text}, "finish_reason": None}]}
            done = {"choices": [{"delta": {}, "finish_reason": "stop"}]}
            self.wfile.write(("data: " + json.dumps(chunk) + "\n\n").encode())
            self.wfile.write(("data: " + json.dumps(done) + "\n\n").encode())
            self.wfile.write(b"data: [DONE]\n\n")
            return
        self.send_header("content-type", "application/json")
        self.end_headers()
        response = {
            "id": "chatcmpl-smoke",
            "object": "chat.completion",
            "model": model,
            "choices": [{"message": {"role": "assistant", "content": text}, "finish_reason": "stop"}],
        }
        self.wfile.write(json.dumps(response).encode())

with socketserver.TCPServer(("127.0.0.1", port), Handler) as server:
    server.serve_forever()
PY
  MOCK_LLM_PID="$!"
  local deadline=$((SECONDS + 10))
  while [ "$SECONDS" -lt "$deadline" ]; do
    if curl -fsS "http://127.0.0.1:${MOCK_LLM_PORT}/health" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.25
  done
  echo "mock LLM did not become healthy on port $MOCK_LLM_PORT"
  cat "$DISPOSABLE_HOME/mock-llm.log" 2>/dev/null || true
  exit 1
}

sanitize_instance() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9-]+/-/g; s/^-+//; s/-+$//'
}

if [ ! -d "$SOURCE_HOME" ]; then
  echo "Source Agency home does not exist: $SOURCE_HOME"
  exit 1
fi

if [ -z "$DISPOSABLE_HOME" ]; then
  DISPOSABLE_HOME="$(mktemp -d "${TMPDIR:-/tmp}/agency-live-home.XXXXXX")"
else
  mkdir -p "$DISPOSABLE_HOME"
fi

if [ -z "${AGENCY_DISPOSABLE_GATEWAY_PORT:-}" ] && port_in_use "$GATEWAY_PORT"; then
  GATEWAY_PORT="$(pick_free_port)"
fi

if [ -z "${AGENCY_DISPOSABLE_WEB_PORT:-}" ] && port_in_use "$WEB_PORT"; then
  WEB_PORT="$(pick_free_port)"
fi

if [ -z "${AGENCY_DISPOSABLE_GATEWAY_PROXY_PORT:-}" ] && port_in_use "$PROXY_PORT"; then
  PROXY_PORT="$(pick_free_port)"
fi

if [ -z "${AGENCY_DISPOSABLE_GATEWAY_PROXY_KNOWLEDGE_PORT:-}" ] && port_in_use "$PROXY_KNOWLEDGE_PORT"; then
  PROXY_KNOWLEDGE_PORT="$(pick_free_port)"
fi

if [ -z "${AGENCY_DISPOSABLE_GATEWAY_PROXY_INTAKE_PORT:-}" ] && port_in_use "$PROXY_INTAKE_PORT"; then
  PROXY_INTAKE_PORT="$(pick_free_port)"
fi

if [ -z "${AGENCY_DISPOSABLE_KNOWLEDGE_PORT:-}" ] && port_in_use "$KNOWLEDGE_PORT"; then
  KNOWLEDGE_PORT="$(pick_free_port)"
fi

if [ -z "${AGENCY_DISPOSABLE_INTAKE_PORT:-}" ] && port_in_use "$INTAKE_PORT"; then
  INTAKE_PORT="$(pick_free_port)"
fi

if [ -z "${AGENCY_DISPOSABLE_WEB_FETCH_PORT:-}" ] && port_in_use "$WEB_FETCH_PORT"; then
  WEB_FETCH_PORT="$(pick_free_port)"
fi

if [ -z "${AGENCY_DISPOSABLE_EGRESS_PROXY_PORT:-}" ] && port_in_use "$EGRESS_PORT"; then
  EGRESS_PORT="$(pick_free_port)"
fi

mkdir -p "$DISPOSABLE_HOME"
cp -R "$SOURCE_HOME"/. "$DISPOSABLE_HOME"/ 2>/dev/null || true
rm -f "$DISPOSABLE_HOME/gateway.pid" "$DISPOSABLE_HOME/gateway.log"
rm -rf "$DISPOSABLE_HOME/run"

export AGENCY_HOME="$DISPOSABLE_HOME"
export AGENCY_INFRA_INSTANCE="$(sanitize_instance "$(basename "$DISPOSABLE_HOME")")"
export AGENCY_BIN="${AGENCY_BIN:-$ROOT_DIR/agency}"
export AGENCY_GATEWAY_URL="http://127.0.0.1:${GATEWAY_PORT}"
export AGENCY_GATEWAY_PORT="$GATEWAY_PORT"
export AGENCY_WEB_PORT="$WEB_PORT"
export AGENCY_GATEWAY_PROXY_PORT="$PROXY_PORT"
export AGENCY_GATEWAY_PROXY_KNOWLEDGE_PORT="$PROXY_KNOWLEDGE_PORT"
export AGENCY_GATEWAY_PROXY_INTAKE_PORT="$PROXY_INTAKE_PORT"
export AGENCY_KNOWLEDGE_PORT="$KNOWLEDGE_PORT"
export AGENCY_INTAKE_PORT="$INTAKE_PORT"
export AGENCY_WEB_FETCH_PORT="$WEB_FETCH_PORT"
export AGENCY_EGRESS_PROXY_PORT="$EGRESS_PORT"
export AGENCY_WEB_BASE_URL="http://127.0.0.1:${WEB_PORT}"
export AGENCY_GATEWAY_HEALTH_URL="http://127.0.0.1:${GATEWAY_PORT}/api/v1/health"
export AGENCY_DISPOSABLE_GATEWAY_PORT="$GATEWAY_PORT"
export AGENCY_E2E_DISPOSABLE=1

stop_pid() {
  local pid="$1"
  local waited=0

  if [ -z "$pid" ] || ! kill -0 "$pid" 2>/dev/null; then
    return 0
  fi

  kill -TERM "$pid" 2>/dev/null || true
  while kill -0 "$pid" 2>/dev/null && [ "$waited" -lt 10 ]; do
    waited=$((waited + 1))
    sleep 1
  done

  if kill -0 "$pid" 2>/dev/null; then
    kill -KILL "$pid" 2>/dev/null || true
  fi
  wait "$pid" 2>/dev/null || true
}

cleanup_scoped_infra_runtime() {
  if [ -z "${AGENCY_INFRA_INSTANCE:-}" ]; then
    return 0
  fi

  local backend=""
  local runtime_cli=""
  local config_path="${DISPOSABLE_HOME:-}/config.yaml"

  if [ -f "$config_path" ] && command -v ruby >/dev/null 2>&1; then
    backend="$(ruby -e 'require "yaml"; path = ARGV[0]; data = YAML.load_file(path) || {}; hub = data["hub"].is_a?(Hash) ? data["hub"] : {}; puts hub["deployment_backend"].to_s.strip' "$config_path" 2>/dev/null || true)"
  fi

  case "$backend" in
    docker)
      runtime_cli="docker"
      ;;
    podman)
      runtime_cli="podman"
      ;;
    *)
      return 0
      ;;
  esac

  if ! command -v "$runtime_cli" >/dev/null 2>&1; then
    return 0
  fi

  local container_ids
  local network_ids

  container_ids="$("$runtime_cli" ps -aq \
    --filter label=agency.managed=true \
    --filter label=agency.role=infra \
    --filter "label=agency.instance=${AGENCY_INFRA_INSTANCE}")"
  if [ -n "$container_ids" ]; then
    "$runtime_cli" rm -f $container_ids >/dev/null 2>&1 || true
  fi

  network_ids="$("$runtime_cli" network ls -q \
    --filter label=agency.managed=true \
    --filter label=agency.role=infra \
    --filter "label=agency.instance=${AGENCY_INFRA_INSTANCE}")"
  if [ -n "$network_ids" ]; then
    "$runtime_cli" network rm $network_ids >/dev/null 2>&1 || true
  fi
}

cleanup() {
  local status="$?"
  trap - EXIT INT TERM HUP

  echo "==> Cleaning up disposable Agency runtime"
  stop_pid "$MOCK_LLM_PID"
  AGENCY_HOME="$DISPOSABLE_HOME" AGENCY_INFRA_INSTANCE="$AGENCY_INFRA_INSTANCE" "$AGENCY_BIN" -q infra down >/dev/null 2>&1 || true
  cleanup_scoped_infra_runtime
  AGENCY_HOME="$DISPOSABLE_HOME" "$AGENCY_BIN" serve stop >/dev/null 2>&1 || true

  if [ -f "$DISPOSABLE_HOME/gateway.pid" ]; then
    stop_pid "$(cat "$DISPOSABLE_HOME/gateway.pid" 2>/dev/null || true)"
    rm -f "$DISPOSABLE_HOME/gateway.pid"
  fi

  if [ "${KEEP_HOME}" = "1" ]; then
    echo "Keeping disposable Agency home at $DISPOSABLE_HOME"
  else
    rm -rf "$DISPOSABLE_HOME"
  fi

  exit "$status"
}
trap cleanup EXIT INT TERM HUP

CONFIG_PATH="$AGENCY_HOME/config.yaml"
TMP_CONFIG="$(mktemp "${TMPDIR:-/tmp}/agency-disposable-config.XXXXXX")"
DISPOSABLE_GATEWAY_ADDR="127.0.0.1:${AGENCY_DISPOSABLE_GATEWAY_PORT}"

if [ -f "$CONFIG_PATH" ]; then
  awk -v gateway_addr="$DISPOSABLE_GATEWAY_ADDR" '
    BEGIN { saw_gateway = 0; saw_token = 0 }
    /^gateway_addr:[[:space:]]*/ {
      print "gateway_addr: " gateway_addr
      saw_gateway = 1
      next
    }
    /^token:[[:space:]]*/ {
      print
      saw_token = 1
      next
    }
    { print }
    END {
      if (!saw_gateway) {
        print "gateway_addr: " gateway_addr
      }
      if (!saw_token) {
        print "token: agency-disposable-live-check-token"
      }
    }
  ' "$CONFIG_PATH" >"$TMP_CONFIG"
else
  cat >"$TMP_CONFIG" <<EOF
gateway_addr: ${DISPOSABLE_GATEWAY_ADDR}
token: agency-disposable-live-check-token
EOF
fi

mv "$TMP_CONFIG" "$CONFIG_PATH"

start_mock_llm

if [ ! -f "$DISPOSABLE_HOME/capacity.yaml" ]; then
  cat >"$DISPOSABLE_HOME/capacity.yaml" <<'EOF'
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
fi

if [ -n "$RUNTIME_BACKEND" ] || [ -n "$ROOTFS_OCI_REF" ] || [ -n "$ENFORCER_OCI_REF" ]; then
  ruby - "$CONFIG_PATH" "$RUNTIME_BACKEND" "$ROOTFS_OCI_REF" "$ENFORCER_OCI_REF" "$HOST_ENFORCER_BIN" <<'RUBY'
require "yaml"

path, backend, rootfs_ref, enforcer_ref, host_enforcer_bin = ARGV
data = File.exist?(path) ? (YAML.load_file(path) || {}) : {}
data["hub"] = {} unless data["hub"].is_a?(Hash)
data["hub"]["deployment_backend"] = backend unless backend.to_s.strip.empty?
data["hub"]["deployment_backend_config"] = {} unless data["hub"]["deployment_backend_config"].is_a?(Hash)
config = data["hub"]["deployment_backend_config"]
config["rootfs_oci_ref"] = rootfs_ref unless rootfs_ref.to_s.strip.empty?
config["enforcer_oci_ref"] = enforcer_ref unless enforcer_ref.to_s.strip.empty?
if backend == "microagent" && config["enforcer_binary_path"].to_s.strip.empty? && !host_enforcer_bin.to_s.strip.empty?
  config["enforcer_binary_path"] = host_enforcer_bin
end
File.write(path, YAML.dump(data))
RUBY
fi

if [ "$MOCK_LLM" = "1" ]; then
  ruby - "$CONFIG_PATH" <<'RUBY'
require "yaml"

path = ARGV[0]
data = File.exist?(path) ? (YAML.load_file(path) || {}) : {}
data["llm_provider"] = "smoke"
File.write(path, YAML.dump(data))
RUBY
  mkdir -p "$DISPOSABLE_HOME/infrastructure"
  cat >"$DISPOSABLE_HOME/infrastructure/routing.yaml" <<EOF
providers:
  smoke:
    api_base: http://localhost:${MOCK_LLM_PORT}/v1
    auth_env: SMOKE_API_KEY
models:
  standard:
    provider: smoke
    provider_model: smoke-model
    capabilities: [tools, streaming]
  fast:
    provider: smoke
    provider_model: smoke-model
    capabilities: [tools, streaming]
  mini:
    provider: smoke
    provider_model: smoke-model
    capabilities: [tools, streaming]
  frontier:
    provider: smoke
    provider_model: smoke-model
    capabilities: [tools, streaming]
  nano:
    provider: smoke
    provider_model: smoke-model
    capabilities: [tools, streaming]
tiers:
  standard:
    - model: standard
      preference: 1
  fast:
    - model: fast
      preference: 1
  mini:
    - model: mini
      preference: 1
  frontier:
    - model: frontier
      preference: 1
  nano:
    - model: nano
      preference: 1
settings:
  default_tier: standard
EOF
  "$AGENCY_BIN" -q creds set SMOKE_API_KEY \
    --value "$SMOKE_API_KEY" \
    --kind provider \
    --protocol api-key \
    --scope platform >/dev/null
fi

echo "==> Disposable Agency home: $DISPOSABLE_HOME"
echo "==> Disposable infra id:    $AGENCY_INFRA_INSTANCE"
echo "==> Disposable gateway:     $AGENCY_GATEWAY_HEALTH_URL"
echo "==> Disposable web:         $AGENCY_WEB_BASE_URL"
echo "==> Playwright config:      $PLAYWRIGHT_CONFIG"

if [ "${#PLAYWRIGHT_ARGS[@]}" -gt 0 ]; then
  "$ROOT_DIR/scripts/e2e/e2e-live-web.sh" \
    --force-infra-up \
    $([ "$SKIP_BUILD" = "1" ] && printf '%s' '--skip-build') \
    --config "$PLAYWRIGHT_CONFIG" \
    "${PLAYWRIGHT_ARGS[@]}"
else
  "$ROOT_DIR/scripts/e2e/e2e-live-web.sh" \
    --force-infra-up \
    $([ "$SKIP_BUILD" = "1" ] && printf '%s' '--skip-build') \
    --config "$PLAYWRIGHT_CONFIG"
fi
