#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
WORKSPACE_ENV_FILE="${AGENCY_WORKSPACE_ENV_FILE:-$(cd "$ROOT_DIR/.." && pwd)/.env-agency}"
SOURCE_HOME="${AGENCY_SOURCE_HOME:-${HOME}/.agency}"
DISPOSABLE_HOME="${AGENCY_SETUP_HOME:-}"
GATEWAY_PORT="${AGENCY_SETUP_GATEWAY_PORT:-18300}"
WEB_PORT="${AGENCY_SETUP_WEB_PORT:-18380}"
PROXY_PORT="${AGENCY_SETUP_GATEWAY_PROXY_PORT:-18302}"
PROXY_KNOWLEDGE_PORT="${AGENCY_SETUP_GATEWAY_PROXY_KNOWLEDGE_PORT:-18304}"
PROXY_INTAKE_PORT="${AGENCY_SETUP_GATEWAY_PROXY_INTAKE_PORT:-18305}"
KNOWLEDGE_PORT="${AGENCY_SETUP_KNOWLEDGE_PORT:-18314}"
INTAKE_PORT="${AGENCY_SETUP_INTAKE_PORT:-18315}"
WEB_FETCH_PORT="${AGENCY_SETUP_WEB_FETCH_PORT:-18316}"
PROVIDER=""
PROVIDER_LABEL="${AGENCY_SETUP_PROVIDER_LABEL:-Google Gemini}"
PROVIDER_CREDENTIAL="${AGENCY_SETUP_PROVIDER_CREDENTIAL:-GEMINI_API_KEY}"
PROVIDER_API_KEY="${AGENCY_SETUP_PROVIDER_API_KEY:-}"
KEEP_HOME="${AGENCY_SETUP_KEEP_HOME:-0}"
KEEP_HOME_ON_FAILURE="${AGENCY_SETUP_KEEP_HOME_ON_FAILURE:-1}"
WAIT_FOR_REPLY="${AGENCY_SETUP_WAIT_FOR_REPLY:-0}"
SKIP_BUILD="${AGENCY_E2E_SKIP_BUILD:-0}"

usage() {
  cat <<'EOF'
Usage: ./scripts/setup-wizard-readiness-check.sh [options]

Runs a disposable first-run Web setup wizard check. The script creates an empty
Agency home, starts an isolated local stack, completes the setup wizard in the
Web UI, verifies chat opens and the initial prompt is sent, then cleans up.

Options:
  --keep-home     Preserve the disposable Agency home after the run
  --skip-build    Reuse the current local Agency binary/images
  --wait-for-reply
                  Also wait for a real agent/provider reply (slow deep check)
  -h, --help      Show this help

Environment:
  AGENCY_SETUP_PROVIDER              Provider name (default: google)
  AGENCY_SETUP_PROVIDER_LABEL        Provider button label (default: Google Gemini)
  AGENCY_SETUP_PROVIDER_API_KEY      API key to enter in the wizard
  AGENCY_SETUP_PROVIDER_CREDENTIAL   Source credential to copy when API key is unset (default: GEMINI_API_KEY)
  AGENCY_SOURCE_HOME                 Source home for credential copy (default: ~/.agency)
  AGENCY_SETUP_KEEP_HOME=1           Preserve disposable home
  AGENCY_SETUP_KEEP_HOME_ON_FAILURE=1
                                     Preserve disposable home automatically when the check fails (default: 1)
  AGENCY_SETUP_WAIT_FOR_REPLY=1      Wait for a real agent reply
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --keep-home)
      KEEP_HOME=1
      shift
      ;;
    --skip-build)
      SKIP_BUILD=1
      shift
      ;;
    --wait-for-reply)
      WAIT_FOR_REPLY=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

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
  if [ -n "${AGENCY_BIN:-}" ] && [ -x "$AGENCY_BIN" ]; then
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
  if [ -x "$HOME/.agency/bin/agency" ]; then
    printf '%s\n' "$HOME/.agency/bin/agency"
    return 0
  fi
  return 1
}

load_workspace_env() {
  if [ -f "$WORKSPACE_ENV_FILE" ]; then
    set -a
    # shellcheck disable=SC1090
    . "$WORKSPACE_ENV_FILE"
    set +a
  fi
}

extract_credential_value() {
  local line
  local output
  output="$(AGENCY_HOME="$SOURCE_HOME" "$AGENCY_BIN" -q creds show "$PROVIDER_CREDENTIAL" --show-value 2>/dev/null || true)"
  while IFS= read -r line; do
    case "$line" in
      *Value:*)
        line="${line#*Value:}"
        line="${line#"${line%%[![:space:]]*}"}"
        printf '%s\n' "$line"
        return 0
        ;;
    esac
  done <<EOF
$output
EOF
}

read_config_value() {
  local key="$1"
  local line
  while IFS= read -r line; do
    case "$line" in
      "$key":*)
        line="${line#*:}"
        line="${line#"${line%%[![:space:]]*}"}"
        line="${line%\"}"
        line="${line#\"}"
        printf '%s\n' "$line"
        return 0
        ;;
    esac
  done < "$DISPOSABLE_HOME/config.yaml"
}

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
}

cleanup_scoped_infra_runtime() {
  if [ -z "${AGENCY_INFRA_INSTANCE:-}" ]; then
    return 0
  fi

  local backend="docker"
  local runtime_cli="docker"
  local config_path="${DISPOSABLE_HOME:-}/config.yaml"

  if [ -f "$config_path" ] && command -v ruby >/dev/null 2>&1; then
    backend="$(ruby -e 'require "yaml"; path = ARGV[0]; data = YAML.load_file(path) || {}; hub = data["hub"].is_a?(Hash) ? data["hub"] : {}; value = hub["deployment_backend"].to_s.strip; puts(value.empty? ? "docker" : value)' "$config_path" 2>/dev/null || printf '%s' 'docker')"
  fi

  case "$backend" in
    podman)
      runtime_cli="podman"
      ;;
    *)
      runtime_cli="docker"
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

setup_api_request() {
  local method="$1"
  local path="$2"
  if [ -z "${AGENCY_GATEWAY_URL:-}" ]; then
    return 1
  fi
  if [ -n "${AGENCY_SETUP_API_TOKEN:-}" ]; then
    curl -fsS -X "$method" \
      -H "Authorization: Bearer ${AGENCY_SETUP_API_TOKEN}" \
      -H "Content-Type: application/json" \
      -d '{}' \
      "${AGENCY_GATEWAY_URL}/api/v1${path}" >/dev/null
  else
    curl -fsS -X "$method" \
      -H "Content-Type: application/json" \
      -d '{}' \
      "${AGENCY_GATEWAY_URL}/api/v1${path}" >/dev/null
  fi
}

cleanup_setup_agent() {
  if [ -z "${AGENCY_SETUP_AGENT_NAME:-}" ]; then
    return 0
  fi
  setup_api_request "DELETE" "/agents/${AGENCY_SETUP_AGENT_NAME}" >/dev/null 2>&1 || true
  setup_api_request "POST" "/comms/channels/dm-${AGENCY_SETUP_AGENT_NAME}/archive" >/dev/null 2>&1 || true
}

cleanup() {
  local status="$?"
  local keep_home="${KEEP_HOME}"
  trap - EXIT INT TERM HUP
  echo "==> Cleaning up setup wizard runtime"
  cleanup_setup_agent
  if [ -n "${AGENCY_SETUP_AGENT_NAME:-}" ]; then
    docker rm -f "agency-${AGENCY_SETUP_AGENT_NAME}-workspace" "agency-${AGENCY_SETUP_AGENT_NAME}-enforcer" >/dev/null 2>&1 || true
    docker network rm "agency-${AGENCY_SETUP_AGENT_NAME}-internal" >/dev/null 2>&1 || true
  fi
  AGENCY_HOME="$DISPOSABLE_HOME" AGENCY_INFRA_INSTANCE="$AGENCY_INFRA_INSTANCE" "$AGENCY_BIN" -q infra down >/dev/null 2>&1 || true
  cleanup_scoped_infra_runtime
  AGENCY_HOME="$DISPOSABLE_HOME" "$AGENCY_BIN" serve stop >/dev/null 2>&1 || true
  if [ -f "$DISPOSABLE_HOME/gateway.pid" ]; then
    stop_pid "$(cat "$DISPOSABLE_HOME/gateway.pid" 2>/dev/null || true)"
    rm -f "$DISPOSABLE_HOME/gateway.pid"
  fi
  if [ "$status" -ne 0 ] && [ "$KEEP_HOME_ON_FAILURE" = "1" ]; then
    keep_home=1
    echo "Setup wizard check failed; preserving disposable home for debugging."
  fi
  if [ "$keep_home" = "1" ]; then
    echo "Keeping setup wizard home at $DISPOSABLE_HOME"
  else
    fix_home_permissions "$DISPOSABLE_HOME"
    rm -rf "$DISPOSABLE_HOME"
  fi
  exit "$status"
}

fix_home_permissions() {
  local target="$1"
  if [ -z "$target" ] || [ ! -d "$target" ] || ! command -v docker >/dev/null 2>&1; then
    return 0
  fi
  docker run --rm --entrypoint sh \
    -v "$target:/target" \
    --user 0:0 \
    alpine:3.21 \
    -c "chown -R $(id -u):$(id -g) /target 2>/dev/null || true; chmod -R u+rwX /target 2>/dev/null || true" \
    >/dev/null 2>&1 || true
}

if [ "$SKIP_BUILD" != "1" ]; then
  echo "==> Building Agency"
  make -C "$ROOT_DIR" build >/dev/null
fi

if ! AGENCY_BIN="$(resolve_agency_bin)"; then
  echo "agency binary not found. Set AGENCY_BIN or build the local repo binary first." >&2
  exit 1
fi

if ! command -v cosign >/dev/null 2>&1; then
  echo "cosign is required for OCI Hub signature verification during setup." >&2
  echo "Install cosign, then rerun this check." >&2
  echo "See: https://docs.sigstore.dev/cosign/system_config/installation/" >&2
  exit 1
fi

load_workspace_env

if [ -z "$PROVIDER_API_KEY" ]; then
  PROVIDER_API_KEY="${!PROVIDER_CREDENTIAL:-}"
fi
if [ -z "$PROVIDER_API_KEY" ]; then
  if [ -d "$SOURCE_HOME" ]; then
    PROVIDER_API_KEY="$(extract_credential_value)"
  fi
fi
if [ -z "$PROVIDER_API_KEY" ] || [ "$PROVIDER_API_KEY" = "[redacted]" ]; then
  echo "No provider API key available. Set AGENCY_SETUP_PROVIDER_API_KEY or store $PROVIDER_CREDENTIAL in $SOURCE_HOME." >&2
  exit 1
fi

if [ -z "$DISPOSABLE_HOME" ]; then
  DISPOSABLE_HOME="$(mktemp -d "${TMPDIR:-/tmp}/agency-setup-home.XXXXXX")"
else
  mkdir -p "$DISPOSABLE_HOME"
fi

for var_name in GATEWAY_PORT WEB_PORT PROXY_PORT PROXY_KNOWLEDGE_PORT PROXY_INTAKE_PORT KNOWLEDGE_PORT INTAKE_PORT WEB_FETCH_PORT; do
  port="${!var_name}"
  if port_in_use "$port"; then
    printf -v "$var_name" '%s' "$(pick_free_port)"
  fi
done

export AGENCY_HOME="$DISPOSABLE_HOME"
export AGENCY_INFRA_INSTANCE="$(sanitize_instance "$(basename "$DISPOSABLE_HOME")")"
export AGENCY_GATEWAY_URL="http://127.0.0.1:${GATEWAY_PORT}"
export AGENCY_WEB_PORT="$WEB_PORT"
export AGENCY_GATEWAY_PROXY_PORT="$PROXY_PORT"
export AGENCY_GATEWAY_PROXY_KNOWLEDGE_PORT="$PROXY_KNOWLEDGE_PORT"
export AGENCY_GATEWAY_PROXY_INTAKE_PORT="$PROXY_INTAKE_PORT"
export AGENCY_KNOWLEDGE_PORT="$KNOWLEDGE_PORT"
export AGENCY_INTAKE_PORT="$INTAKE_PORT"
export AGENCY_WEB_FETCH_PORT="$WEB_FETCH_PORT"
export AGENCY_WEB_BASE_URL="http://127.0.0.1:${WEB_PORT}"
export AGENCY_GATEWAY_HEALTH_URL="http://127.0.0.1:${GATEWAY_PORT}/api/v1/health"
export AGENCY_SETUP_PROVIDER="$PROVIDER"
export AGENCY_SETUP_PROVIDER_LABEL="$PROVIDER_LABEL"
export AGENCY_SETUP_PROVIDER_API_KEY="$PROVIDER_API_KEY"
export AGENCY_SETUP_AGENT_NAME="alpha-setup-$(date +%s)"
export AGENCY_SETUP_WAIT_FOR_REPLY="$WAIT_FOR_REPLY"
export AGENCY_PLAYWRIGHT_CONFIG="playwright.live.setup.config.ts"

trap cleanup EXIT INT TERM HUP

mkdir -p "$DISPOSABLE_HOME"
{
  printf 'gateway_addr: 127.0.0.1:%s\n' "$GATEWAY_PORT"
} > "$DISPOSABLE_HOME/config.yaml"

echo "==> Setup wizard Agency home: $DISPOSABLE_HOME"
echo "==> Setup wizard infra id:    $AGENCY_INFRA_INSTANCE"
echo "==> Setup wizard gateway:     $AGENCY_GATEWAY_HEALTH_URL"
echo "==> Setup wizard web:         $AGENCY_WEB_BASE_URL"
echo "==> Setup wizard provider:    $PROVIDER"

"$AGENCY_BIN" setup --no-browser --no-infra >/dev/null
export AGENCY_SETUP_API_TOKEN="$(read_config_value token)"

"$ROOT_DIR/scripts/e2e-live-web.sh" --force-infra-up --skip-build --config "$AGENCY_PLAYWRIGHT_CONFIG"

echo "==> Setup wizard readiness check passed"
