#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
AGENCY_BIN="${AGENCY_BIN:-$ROOT/agency}"
AGENCY_HOME_DIR="${AGENCY_HOME:-}"
HELPER_BIN="${AGENCY_APPLE_CONTAINER_HELPER_BIN:-}"
WAIT_HELPER_BIN="${AGENCY_APPLE_CONTAINER_WAIT_HELPER_BIN:-}"
BUILD=1
BUILD_HELPER=1
BUILD_WAIT_HELPER=1
KEEP_RUNNING=0
SETUP_PROVIDER="${AGENCY_APPLE_SETUP_PROVIDER:-}"
API_KEY="${AGENCY_APPLE_SETUP_API_KEY:-placeholder}"
AGENT_NAME="apple-container-smoke-$(date +%s)"
CREATED_HOME=0

usage() {
  cat <<'EOF'
Usage: ./scripts/readiness/apple-container-smoke.sh [--home PATH] [--provider NAME] [--api-key VALUE] [--helper-bin PATH] [--wait-helper-bin PATH] [--skip-build] [--skip-helper-build] [--skip-wait-helper-build] [--keep-running]

Runs a disposable Apple container backend setup smoke and prints diagnostics for
the current failure point. This script expects the Apple container service to
already be running (`container system start --enable-kernel-install`).

Options:
  --home PATH     Use this AGENCY_HOME. Defaults to a fresh /tmp directory.
  --provider NAME Provider adapter to configure during setup. Defaults to
                  AGENCY_APPLE_SETUP_PROVIDER, then ~/.agency/config.yaml.
  --api-key VALUE API key value to pass to setup. Defaults to
                  AGENCY_APPLE_SETUP_API_KEY, then "placeholder".
  --helper-bin PATH
                  Apple Container helper binary path. Defaults to
                  AGENCY_APPLE_CONTAINER_HELPER_BIN, then a file under --home.
  --skip-build    Reuse the existing ./agency binary.
  --skip-helper-build
                  Reuse the helper binary at --helper-bin.
  --wait-helper-bin PATH
                  Apple Container wait helper binary path. Defaults to
                  AGENCY_APPLE_CONTAINER_WAIT_HELPER_BIN, then a file under --home.
  --skip-wait-helper-build
                  Reuse the wait helper binary at --wait-helper-bin.
  --keep-running  Leave the daemon running after diagnostics.

Environment:
  AGENCY_APPLE_SETUP_PROVIDER  Provider adapter fallback.
  AGENCY_APPLE_SETUP_API_KEY   API key fallback.
  AGENCY_APPLE_CONTAINER_HELPER_BIN
                               Helper binary fallback.
  AGENCY_APPLE_CONTAINER_WAIT_HELPER_BIN
                               Wait helper binary fallback.
EOF
}

log() {
  printf '\n==> %s\n' "$1"
}

cleanup() {
  if [[ "$KEEP_RUNNING" == "1" ]]; then
    return 0
  fi
  if [[ -n "$AGENCY_HOME_DIR" && -x "$AGENCY_BIN" ]]; then
    "$AGENCY_BIN" -H "$AGENCY_HOME_DIR" -q delete "$AGENT_NAME" >/dev/null 2>&1 || true
    "$AGENCY_BIN" -H "$AGENCY_HOME_DIR" -q infra down >/dev/null 2>&1 || true
    cleanup_apple_container_artifacts
    cleanup_apple_container_networks
    "$AGENCY_BIN" -H "$AGENCY_HOME_DIR" serve stop >/dev/null 2>&1 || true
  fi
  if [[ "$CREATED_HOME" == "1" && -n "$AGENCY_HOME_DIR" && "$AGENCY_HOME_DIR" == /tmp/agency-apple-smoke.* ]]; then
    rm -rf "$AGENCY_HOME_DIR"
  fi
}
trap cleanup EXIT INT TERM HUP

agency_home_hash() {
  ruby -rdigest -e 'print Digest::SHA256.hexdigest(ARGV[0])[0, 12]' "$AGENCY_HOME_DIR"
}

cleanup_apple_container_artifacts() {
  if [[ -z "$HELPER_BIN" || ! -x "$HELPER_BIN" || -z "$AGENCY_HOME_DIR" ]]; then
    return 0
  fi
  local home_hash
  home_hash="$(agency_home_hash)"
  local owned_json
  owned_json="$("$HELPER_BIN" list-owned --home-hash "$home_hash" 2>/dev/null || true)"
  if [[ -z "$owned_json" ]]; then
    return 0
  fi
  local id
  while IFS= read -r id; do
    [[ -n "$id" ]] || continue
    "$HELPER_BIN" delete --force "$id" >/dev/null 2>&1 || container delete --force "$id" >/dev/null 2>&1 || true
  done < <(ruby -rjson -e 'data = JSON.parse(STDIN.read) rescue {}; (data["containers"] || []).each { |c| puts((c.dig("configuration", "id") || c["id"]).to_s) }' <<<"$owned_json")
}

cleanup_apple_container_networks() {
  if [[ -z "$AGENCY_HOME_DIR" ]]; then
    return 0
  fi
  local home_hash
  home_hash="$(agency_home_hash)"
  local networks_json
  networks_json="$(container network list --format json 2>/dev/null || true)"
  if [[ -z "$networks_json" ]]; then
    return 0
  fi
  local id
  while IFS= read -r id; do
    [[ -n "$id" ]] || continue
    container network delete "$id" >/dev/null 2>&1 || true
  done < <(ruby -rjson -e '
data = JSON.parse(STDIN.read) rescue []
home = ARGV[0].to_s
data.each do |network|
  labels = network.dig("config", "labels") || {}
  next unless labels["agency.managed"] == "true"
  next unless labels["agency.backend"] == "apple-container"
  next unless labels["agency.home"] == home
  puts network["id"].to_s
end
' "$home_hash" <<<"$networks_json")
}

run_diag() {
  printf '\n$ %s\n' "$*"
  "$@" || true
}

require_cmd() {
  printf '\n$ %s\n' "$*"
  "$@"
}

require_log_contains() {
  local file="$1"
  local pattern="$2"
  local message="$3"
  if ! grep -Fq "$pattern" "$file"; then
    echo "$message" >&2
    exit 1
  fi
}

require_log_absent() {
  local file="$1"
  local pattern="$2"
  local message="$3"
  if grep -Fq "$pattern" "$file"; then
    echo "$message" >&2
    exit 1
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --home)
      [[ $# -ge 2 ]] || { echo "--home requires a path" >&2; exit 2; }
      AGENCY_HOME_DIR="$2"
      shift 2
      ;;
    --provider)
      [[ $# -ge 2 ]] || { echo "--provider requires a value" >&2; exit 2; }
      SETUP_PROVIDER="$2"
      shift 2
      ;;
    --api-key)
      [[ $# -ge 2 ]] || { echo "--api-key requires a value" >&2; exit 2; }
      API_KEY="$2"
      shift 2
      ;;
    --helper-bin)
      [[ $# -ge 2 ]] || { echo "--helper-bin requires a path" >&2; exit 2; }
      HELPER_BIN="$2"
      shift 2
      ;;
    --skip-build)
      BUILD=0
      shift
      ;;
    --skip-helper-build)
      BUILD_HELPER=0
      shift
      ;;
    --wait-helper-bin)
      [[ $# -ge 2 ]] || { echo "--wait-helper-bin requires a path" >&2; exit 2; }
      WAIT_HELPER_BIN="$2"
      shift 2
      ;;
    --skip-wait-helper-build)
      BUILD_WAIT_HELPER=0
      shift
      ;;
    --keep-running)
      KEEP_RUNNING=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "$AGENCY_HOME_DIR" ]]; then
  AGENCY_HOME_DIR="$(mktemp -d /tmp/agency-apple-smoke.XXXXXX)"
  CREATED_HOME=1
fi
if [[ -z "$HELPER_BIN" ]]; then
  HELPER_BIN="$AGENCY_HOME_DIR/agency-apple-container-helper"
fi
if [[ -z "$WAIT_HELPER_BIN" ]]; then
  WAIT_HELPER_BIN="$AGENCY_HOME_DIR/agency-apple-container-wait-helper"
fi
SETUP_LOG="$AGENCY_HOME_DIR/setup.log"
CHANNELS_BODY="$AGENCY_HOME_DIR/channels.json"

cd "$ROOT"

log "environment"
printf 'repo=%s\n' "$ROOT"
printf 'agency_home=%s\n' "$AGENCY_HOME_DIR"
printf 'agency_bin=%s\n' "$AGENCY_BIN"
printf 'helper_bin=%s\n' "$HELPER_BIN"
printf 'wait_helper_bin=%s\n' "$WAIT_HELPER_BIN"

if [[ "$BUILD" == "1" ]]; then
  log "build"
  go build -o "$AGENCY_BIN" ./cmd/gateway
fi

if [[ "$BUILD_HELPER" == "1" ]]; then
  log "build helper"
  go build -o "$HELPER_BIN" ./cmd/apple-container-helper
fi
export AGENCY_APPLE_CONTAINER_HELPER_BIN="$HELPER_BIN"

if [[ "$BUILD_WAIT_HELPER" == "1" ]]; then
  log "build wait helper"
  ./scripts/readiness/apple-container-wait-helper-build.sh "$WAIT_HELPER_BIN" >/dev/null
fi
export AGENCY_APPLE_CONTAINER_WAIT_HELPER_BIN="$WAIT_HELPER_BIN"

require_cmd ruby -v

if [[ -z "$SETUP_PROVIDER" ]]; then
  SETUP_PROVIDER="$(ruby - <<'RUBY'
require "yaml"
path = File.expand_path("~/.agency/config.yaml")
if File.exist?(path)
  cfg = YAML.load_file(path) || {}
  print cfg["llm_provider"].to_s.strip
end
RUBY
)"
fi

log "container service"
require_cmd container system status
require_cmd "$HELPER_BIN" health

log "port preflight"
if lsof -nP -iTCP:8200 -sTCP:LISTEN; then
  echo "port 8200 is already in use; stop the existing daemon before running this smoke" >&2
  exit 1
fi

log "setup"
if [[ -z "$SETUP_PROVIDER" ]]; then
  echo "Pass --provider or set AGENCY_APPLE_SETUP_PROVIDER to the provider adapter name to configure." >&2
  exit 1
fi
set +e
"$AGENCY_BIN" -H "$AGENCY_HOME_DIR" setup \
  --backend apple-container \
  --provider "$SETUP_PROVIDER" \
  --api-key "$API_KEY" \
  --no-browser 2>&1 | tee "$SETUP_LOG"
setup_status=${PIPESTATUS[0]}
set -e
printf 'setup_exit=%s\n' "$setup_status"
if [[ "$setup_status" != "0" ]]; then
  exit "$setup_status"
fi
require_log_contains "$SETUP_LOG" "Infrastructure running." "setup completed without confirming infrastructure is running"
require_log_absent "$SETUP_LOG" "Warning: infrastructure did not start" "setup reported that infrastructure did not start"
require_log_absent "$SETUP_LOG" "agency-web image not available, skipping" "web image was unavailable during Apple container smoke"
CONFIG_HELPER_BIN="$(ruby - "$AGENCY_HOME_DIR/config.yaml" <<'RUBY'
require "yaml"
cfg = YAML.load_file(ARGV[0]) || {}
hub = cfg["hub"] || {}
backend_cfg = hub["deployment_backend_config"] || {}
print backend_cfg["helper_binary"].to_s.strip
RUBY
)"
if [[ "$CONFIG_HELPER_BIN" != "$HELPER_BIN" ]]; then
  echo "config helper_binary mismatch: got '$CONFIG_HELPER_BIN', want '$HELPER_BIN'" >&2
  exit 1
fi
CONFIG_WAIT_HELPER_BIN="$(ruby - "$AGENCY_HOME_DIR/config.yaml" <<'RUBY'
require "yaml"
cfg = YAML.load_file(ARGV[0]) || {}
hub = cfg["hub"] || {}
backend_cfg = hub["deployment_backend_config"] || {}
print backend_cfg["wait_helper_binary"].to_s.strip
RUBY
)"
if [[ "$CONFIG_WAIT_HELPER_BIN" != "$WAIT_HELPER_BIN" ]]; then
  echo "config wait_helper_binary mismatch: got '$CONFIG_WAIT_HELPER_BIN', want '$WAIT_HELPER_BIN'" >&2
  exit 1
fi

log "gateway"
require_cmd "$AGENCY_BIN" -H "$AGENCY_HOME_DIR" serve status
run_diag lsof -nP -iTCP:8200 -sTCP:LISTEN
GATEWAY_HEALTH_ADDR="$(ruby - "$AGENCY_HOME_DIR/config.yaml" <<'RUBY'
require "yaml"
cfg = YAML.load_file(ARGV[0]) || {}
addr = cfg["gateway_addr"].to_s.strip
addr = "127.0.0.1:8200" if addr.empty?
host, port = addr.rpartition(":").values_at(0, 2)
host = "127.0.0.1" if host == "0.0.0.0" || host == "::" || host.empty?
print "#{host}:#{port}"
RUBY
)"
require_cmd curl -fsS "http://$GATEWAY_HEALTH_ADDR/api/v1/health"

log "host reverse bridge"
require_cmd curl -fsS --max-time 5 "http://127.0.0.1:8202/channels" -o "$CHANNELS_BODY"
require_log_contains "$CHANNELS_BODY" '"general"' "host reverse bridge did not return the general channel"

log "web"
require_cmd curl -fsS --max-time 5 "http://127.0.0.1:8280/health"

log "containers"
run_diag container list --all --format json

log "gateway-proxy logs"
run_diag container logs -n 160 agency-infra-gateway-proxy

log "comms logs"
run_diag container logs -n 160 agency-infra-comms

log "comms image import"
require_cmd container run --rm agency-comms:latest /bin/sh -c 'find /app/images/models -type f | sort; python - <<PY
import images.models.comms
print("images.models.comms ok")
PY'

log "agent runtime"
require_cmd "$AGENCY_BIN" -H "$AGENCY_HOME_DIR" -q create "$AGENT_NAME"
require_cmd "$AGENCY_BIN" -H "$AGENCY_HOME_DIR" -q start "$AGENT_NAME"
require_cmd "$ROOT/scripts/readiness/runtime-contract-smoke.sh" \
  --agent "$AGENT_NAME" \
  --home "$AGENCY_HOME_DIR" \
  --bin "$AGENCY_HOME_DIR/runtime-smoke-agency" \
  --skip-tests

log "gateway restart recovery"
require_cmd "$AGENCY_BIN" -H "$AGENCY_HOME_DIR" serve restart
require_cmd "$AGENCY_BIN" -H "$AGENCY_HOME_DIR" serve status
require_cmd curl -fsS "http://$GATEWAY_HEALTH_ADDR/api/v1/health"
require_cmd "$ROOT/scripts/readiness/runtime-contract-smoke.sh" \
  --agent "$AGENT_NAME" \
  --home "$AGENCY_HOME_DIR" \
  --bin "$AGENCY_HOME_DIR/runtime-smoke-agency" \
  --skip-tests

log "agent lifecycle controls"
require_cmd "$AGENCY_BIN" -H "$AGENCY_HOME_DIR" -q restart "$AGENT_NAME"
require_cmd "$AGENCY_BIN" -H "$AGENCY_HOME_DIR" -q halt "$AGENT_NAME" --tier supervised --reason "apple container readiness"
require_cmd "$AGENCY_BIN" -H "$AGENCY_HOME_DIR" -q resume "$AGENT_NAME"
require_cmd "$AGENCY_BIN" -H "$AGENCY_HOME_DIR" -q stop "$AGENT_NAME"
require_cmd "$AGENCY_BIN" -H "$AGENCY_HOME_DIR" -q start "$AGENT_NAME"
require_cmd "$AGENCY_BIN" -H "$AGENCY_HOME_DIR" -q delete "$AGENT_NAME"

if [[ -f "$AGENCY_HOME_DIR/gateway.log" ]]; then
  require_log_absent "$AGENCY_HOME_DIR/gateway.log" "network connect" "gateway log contains a Docker-shaped post-create network connect warning"
  require_log_absent "$AGENCY_HOME_DIR/gateway.log" "apple-container backend cannot connect networks yet" "gateway log contains legacy apple-container network attach warning"
  require_log_absent "$AGENCY_HOME_DIR/gateway.log" "apple_container_helper_events" "gateway log contains helper event warning after wait helper was configured"
fi

log "gateway log tail"
if [[ -f "$AGENCY_HOME_DIR/gateway.log" ]]; then
  tail -n 160 "$AGENCY_HOME_DIR/gateway.log" || true
fi

if [[ "$KEEP_RUNNING" == "1" ]]; then
  log "kept running"
  printf 'agency_home=%s\n' "$AGENCY_HOME_DIR"
else
  log "cleanup"
fi

exit "$setup_status"
