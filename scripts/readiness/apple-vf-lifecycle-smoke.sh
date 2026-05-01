#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
AGENCY_BIN="${AGENCY_BIN:-/tmp/agency-apple-vf-lifecycle}"
HELPER_BIN="${AGENCY_APPLE_VF_HELPER_BIN:-$ROOT/tools/apple-vf-helper/.build/release/agency-apple-vf-helper}"
KERNEL_PATH="${AGENCY_APPLE_VF_KERNEL:-$HOME/.agency/runtime/apple-vf-microvm/artifacts/Image}"
MKE2FS_PATH="${AGENCY_MKE2FS:-}"
ENFORCER_BIN="${AGENCY_APPLE_VF_ENFORCER_BIN:-/tmp/agency-enforcer-host}"
ENFORCER_OCI_REF="${AGENCY_APPLE_VF_ENFORCER_OCI_REF:-}"
VSOCK_BRIDGE_BIN="${AGENCY_APPLE_VF_VSOCK_BRIDGE_BIN:-/tmp/agency-vsock-http-bridge-linux-arm64}"
AGENT_NAME="${AGENT_NAME:-apple-vf-lifecycle-$(date +%s)}"
ROOTFS_SIZE_MIB="${AGENCY_APPLE_VF_ROOTFS_SIZE_MIB:-1024}"
ROOTFS_OCI_REF="${AGENCY_APPLE_VF_ROOTFS_OCI_REF:-}"
BUILD_HELPER=1
KEEP_HOME=0
KEEP_AGENT=0
SMOKE_HOME=""
GATEWAY_PID=""
COMMS_PID=""
DUMMY_PID=""
GATEWAY_PORT=""
COMMS_PORT=""
KNOWLEDGE_PORT=""
WEB_FETCH_PORT=""
EGRESS_PORT=""
TOKEN=""

usage() {
  cat <<'EOF'
Usage: ./scripts/readiness/apple-vf-lifecycle-smoke.sh [options]

Runs a disposable macOS Apple VF lifecycle smoke through the public gateway:
  1. builds/verifies Apple VF helper, kernel, enforcer, and guest bridge artifacts
  2. starts a disposable Agency gateway on a free localhost port
  3. starts lifecycle-required host comms plus inert host service endpoints
  4. creates an agent through /api/v1/agents
  5. starts, validates, restarts, stops, and deletes that agent through API/CLI lifecycle surfaces

Options:
  --home PATH             Use a specific disposable Agency home.
  --agent NAME            Agent name for the disposable smoke runtime.
  --agency-bin PATH       Agency binary output path. Defaults to /tmp/agency-apple-vf-lifecycle.
  --helper-bin PATH       Signed agency-apple-vf-helper path.
  --kernel PATH           Linux ARM64 kernel Image path.
  --mke2fs PATH           mke2fs path. Defaults to PATH lookup, then Homebrew e2fsprogs.
  --enforcer-bin PATH     Host-process enforcer output path.
  --enforcer-oci-ref REF  Versioned enforcer OCI artifact reference. When set,
                          extracts darwin/arm64 /usr/local/bin/enforcer and
                          uses it as the host-process enforcer binary.
  --vsock-bridge-bin PATH Linux ARM64 agency-vsock-http-bridge output path.
  --rootfs-oci-ref REF    Versioned OCI artifact reference for the body rootfs source.
  --rootfs-size-mib N     Rootfs image size. Defaults to 1024.
  --skip-helper-build     Reuse --helper-bin instead of building/signing it.
  --keep-home             Keep the disposable Agency home after the run.
  --keep-agent            Keep the disposable Agency home and agent runtime after validation.

Environment:
  AGENCY_BIN
  AGENCY_APPLE_VF_HELPER_BIN
  AGENCY_APPLE_VF_KERNEL
  AGENCY_MKE2FS
  AGENCY_APPLE_VF_ENFORCER_BIN
  AGENCY_APPLE_VF_ENFORCER_OCI_REF
  AGENCY_APPLE_VF_VSOCK_BRIDGE_BIN
  AGENCY_APPLE_VF_ROOTFS_OCI_REF
  AGENCY_APPLE_VF_ROOTFS_SIZE_MIB
EOF
}

log() {
  printf '==> %s\n' "$1"
}

fail() {
  printf 'ERROR: %s\n' "$1" >&2
  exit 1
}

api_url() {
  printf 'http://127.0.0.1:%s%s' "$GATEWAY_PORT" "$1"
}

cleanup() {
  set +e
  if [[ "$KEEP_AGENT" != "1" && -n "$GATEWAY_PORT" && -n "$TOKEN" && -n "$AGENT_NAME" ]]; then
    curl -fsS -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
      -X POST "$(api_url "/api/v1/agents/$AGENT_NAME/stop")" \
      -d '{"type":"immediate","reason":"apple-vf lifecycle smoke cleanup"}' >/dev/null 2>&1
    curl -fsS -H "Authorization: Bearer $TOKEN" \
      -X DELETE "$(api_url "/api/v1/agents/$AGENT_NAME")" >/dev/null 2>&1
  fi
  if [[ -n "$SMOKE_HOME" && -x "$AGENCY_BIN" ]]; then
    AGENCY_HOME="$SMOKE_HOME" "$AGENCY_BIN" serve stop >/dev/null 2>&1
  fi
  if [[ -n "$GATEWAY_PID" ]] && kill -0 "$GATEWAY_PID" >/dev/null 2>&1; then
    kill "$GATEWAY_PID" >/dev/null 2>&1
    wait "$GATEWAY_PID" >/dev/null 2>&1
  fi
  if [[ -n "$COMMS_PID" ]] && kill -0 "$COMMS_PID" >/dev/null 2>&1; then
    kill "$COMMS_PID" >/dev/null 2>&1
    wait "$COMMS_PID" >/dev/null 2>&1
  fi
  if [[ -n "$DUMMY_PID" ]] && kill -0 "$DUMMY_PID" >/dev/null 2>&1; then
    kill "$DUMMY_PID" >/dev/null 2>&1
    wait "$DUMMY_PID" >/dev/null 2>&1
  fi
  if [[ "$KEEP_HOME" != "1" && -n "$SMOKE_HOME" && "$SMOKE_HOME" == /tmp/agency-apple-vf-lifecycle.* ]]; then
    rm -rf "$SMOKE_HOME"
  fi
}
trap cleanup EXIT INT TERM HUP

while [[ $# -gt 0 ]]; do
  case "$1" in
    --home)
      [[ $# -ge 2 ]] || fail "--home requires a path"
      SMOKE_HOME="$2"
      shift 2
      ;;
    --agent)
      [[ $# -ge 2 ]] || fail "--agent requires a name"
      AGENT_NAME="$2"
      shift 2
      ;;
    --agency-bin)
      [[ $# -ge 2 ]] || fail "--agency-bin requires a path"
      AGENCY_BIN="$2"
      shift 2
      ;;
    --helper-bin)
      [[ $# -ge 2 ]] || fail "--helper-bin requires a path"
      HELPER_BIN="$2"
      shift 2
      ;;
    --kernel)
      [[ $# -ge 2 ]] || fail "--kernel requires a path"
      KERNEL_PATH="$2"
      shift 2
      ;;
    --mke2fs)
      [[ $# -ge 2 ]] || fail "--mke2fs requires a path"
      MKE2FS_PATH="$2"
      shift 2
      ;;
    --enforcer-bin)
      [[ $# -ge 2 ]] || fail "--enforcer-bin requires a path"
      ENFORCER_BIN="$2"
      shift 2
      ;;
    --enforcer-oci-ref)
      [[ $# -ge 2 ]] || fail "--enforcer-oci-ref requires a ref"
      ENFORCER_OCI_REF="$2"
      shift 2
      ;;
    --vsock-bridge-bin)
      [[ $# -ge 2 ]] || fail "--vsock-bridge-bin requires a path"
      VSOCK_BRIDGE_BIN="$2"
      shift 2
      ;;
    --rootfs-size-mib)
      [[ $# -ge 2 ]] || fail "--rootfs-size-mib requires a value"
      ROOTFS_SIZE_MIB="$2"
      shift 2
      ;;
    --rootfs-oci-ref)
      [[ $# -ge 2 ]] || fail "--rootfs-oci-ref requires a value"
      ROOTFS_OCI_REF="$2"
      shift 2
      ;;
    --skip-helper-build)
      BUILD_HELPER=0
      shift
      ;;
    --keep-home)
      KEEP_HOME=1
      shift
      ;;
    --keep-agent)
      KEEP_AGENT=1
      KEEP_HOME=1
      shift
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

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

pick_free_port() {
  python3 - <<'PY'
import socket
with socket.socket() as s:
    s.bind(("127.0.0.1", 0))
    print(s.getsockname()[1])
PY
}

wait_gateway() {
  local url="$1"
  local deadline=$((SECONDS + 30))
  local code
  while (( SECONDS < deadline )); do
    code="$(curl -sS -o /dev/null -w '%{http_code}' "$url" || true)"
    if [[ "$code" == "200" || "$code" == "401" ]]; then
      return 0
    fi
    sleep 0.25
  done
  return 1
}

api_json() {
  local method="$1"
  local path="$2"
  local body="${3:-}"
  if [[ -n "$body" ]]; then
    curl -fsS -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
      -X "$method" "$(api_url "$path")" -d "$body"
  else
    curl -fsS -H "Authorization: Bearer $TOKEN" -X "$method" "$(api_url "$path")"
  fi
}

api_stream_expect() {
  local path="$1"
  local want_type="$2"
  local body="${3:-}"
  local out
  out="$(mktemp /tmp/agency-apple-vf-stream.XXXXXX)"
  if [[ -n "$body" ]]; then
    curl -fsS -H "Authorization: Bearer $TOKEN" -H 'Accept: application/x-ndjson' -H 'Content-Type: application/json' \
      -X POST "$(api_url "$path")" -d "$body" | tee "$out"
  else
    curl -fsS -H "Authorization: Bearer $TOKEN" -H 'Accept: application/x-ndjson' \
      -X POST "$(api_url "$path")" | tee "$out"
  fi
  python3 - "$out" "$want_type" <<'PY'
import json
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
want = sys.argv[2]
seen = False
errors = []
for line in path.read_text().splitlines():
    if not line.strip():
        continue
    event = json.loads(line)
    if event.get("type") == "error":
        errors.append(event.get("error", "unknown stream error"))
    if event.get("type") == want:
        seen = True
if errors:
    raise SystemExit("; ".join(errors))
if not seen:
    raise SystemExit(f"stream did not emit type={want}")
PY
  rm -f "$out"
}

assert_runtime_healthy() {
  local status_json="$1"
  python3 - "$status_json" <<'PY'
import json
import pathlib
import sys

status = json.loads(pathlib.Path(sys.argv[1]).read_text())
if status.get("backend") != "apple-vf-microvm":
    raise SystemExit(f"backend = {status.get('backend')!r}, want apple-vf-microvm")
if status.get("phase") != "running":
    raise SystemExit(f"phase = {status.get('phase')!r}, want running")
if status.get("healthy") is not True:
    raise SystemExit("runtime healthy flag is not true")
details = status.get("details") or {}
if str(details.get("body_ws_connected")).lower() != "true":
    raise SystemExit(f"body_ws_connected = {details.get('body_ws_connected')!r}, want true")
PY
}

start_comms() {
  local python_bin="$ROOT/.venv/bin/python"
  if [[ ! -x "$python_bin" ]]; then
    python_bin="$(command -v python3)"
  fi
  mkdir -p "$SMOKE_HOME/infrastructure/comms/data/channels" "$SMOKE_HOME/infrastructure/comms/data/cursors"
  PYTHONPATH="$ROOT" "$python_bin" "$ROOT/services/comms/server.py" \
    --port "$COMMS_PORT" \
    --data-dir "$SMOKE_HOME/infrastructure/comms/data" \
    --agents-dir "$SMOKE_HOME/agents" \
    >"$SMOKE_HOME/comms.log" 2>&1 &
  COMMS_PID="$!"
  local deadline=$((SECONDS + 20))
  while (( SECONDS < deadline )); do
    if curl -fsS "http://127.0.0.1:$COMMS_PORT/health" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.25
  done
  tail -n 120 "$SMOKE_HOME/comms.log" >&2 || true
  fail "comms did not become healthy"
}

start_dummy_services() {
  python3 - "$KNOWLEDGE_PORT" "$WEB_FETCH_PORT" "$EGRESS_PORT" >"$SMOKE_HOME/dummy-services.log" 2>&1 <<'PY' &
import http.server
import json
import socketserver
import sys
import threading

ports = [int(p) for p in sys.argv[1:]]

class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps({"ok": True}).encode())

    def do_POST(self):
        self.do_GET()

    def log_message(self, *_):
        pass

servers = []
for port in ports:
    server = socketserver.TCPServer(("127.0.0.1", port), Handler)
    servers.append(server)
    threading.Thread(target=server.serve_forever, daemon=True).start()

threading.Event().wait()
PY
  DUMMY_PID="$!"
  local deadline=$((SECONDS + 10))
  while (( SECONDS < deadline )); do
    if curl -fsS "http://127.0.0.1:$KNOWLEDGE_PORT/health" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.25
  done
  tail -n 120 "$SMOKE_HOME/dummy-services.log" >&2 || true
  fail "dummy host services did not become healthy"
}

patch_agent_model() {
  if grep -q '^model:' "$SMOKE_HOME/agents/$AGENT_NAME/agent.yaml"; then
    python3 - "$SMOKE_HOME/agents/$AGENT_NAME/agent.yaml" <<'PY'
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
lines = path.read_text().splitlines()
out = []
for line in lines:
    if line.startswith("model:"):
        out.append("model: apple-vf-smoke-model")
    else:
        out.append(line)
path.write_text("\n".join(out) + "\n")
PY
  else
    printf '\nmodel: apple-vf-smoke-model\n' >>"$SMOKE_HOME/agents/$AGENT_NAME/agent.yaml"
  fi
}

if [[ "$(uname -s)" != "Darwin" ]]; then
  fail "apple-vf lifecycle smoke requires macOS"
fi
if [[ "$(uname -m)" != "arm64" ]]; then
  fail "apple-vf lifecycle smoke requires Apple silicon arm64"
fi

require_cmd curl
require_cmd go
require_cmd python3

if [[ -z "$MKE2FS_PATH" ]]; then
  if command -v mke2fs >/dev/null 2>&1; then
    MKE2FS_PATH="$(command -v mke2fs)"
  elif [[ -x /opt/homebrew/opt/e2fsprogs/sbin/mke2fs ]]; then
    MKE2FS_PATH="/opt/homebrew/opt/e2fsprogs/sbin/mke2fs"
  else
    fail "mke2fs not found; install e2fsprogs or pass --mke2fs"
  fi
fi

[[ -r "$KERNEL_PATH" ]] || fail "Apple VF kernel Image is not readable at $KERNEL_PATH"
[[ -x "$MKE2FS_PATH" ]] || fail "mke2fs is not executable at $MKE2FS_PATH"
[[ -n "$ROOTFS_OCI_REF" ]] || fail "Apple VF rootfs OCI artifact is not configured; pass --rootfs-oci-ref or set AGENCY_APPLE_VF_ROOTFS_OCI_REF"

cd "$ROOT"

if [[ "$BUILD_HELPER" == "1" ]]; then
  log "Building signed Apple VF helper"
  "$ROOT/scripts/readiness/apple-vf-helper-build.sh" >/dev/null
fi
[[ -x "$HELPER_BIN" ]] || fail "Apple VF helper is not executable at $HELPER_BIN"

log "Building Agency gateway"
go build -o "$AGENCY_BIN" ./cmd/gateway
[[ -x "$AGENCY_BIN" ]] || fail "Agency build did not produce $AGENCY_BIN"

if [[ -n "$ENFORCER_OCI_REF" ]]; then
  log "Extracting host-process enforcer from OCI artifact"
  go run ./cmd/runtime-oci-artifact \
    --extract-ref "$ENFORCER_OCI_REF" \
    --extract-path /usr/local/bin/enforcer \
    --output "$ENFORCER_BIN" \
    --platform darwin/arm64
else
  log "Building host-process enforcer"
  (cd "$ROOT/images/enforcer" && go build -o "$ENFORCER_BIN" .)
fi
[[ -x "$ENFORCER_BIN" ]] || fail "host enforcer build did not produce $ENFORCER_BIN"

log "Building Linux ARM64 guest vsock bridge"
env GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o "$VSOCK_BRIDGE_BIN" ./cmd/agency-vsock-http-bridge
[[ -x "$VSOCK_BRIDGE_BIN" ]] || fail "vsock bridge build did not produce $VSOCK_BRIDGE_BIN"

if [[ -z "$SMOKE_HOME" ]]; then
  SMOKE_HOME="$(mktemp -d /tmp/agency-apple-vf-lifecycle.XXXXXX)"
else
  mkdir -p "$SMOKE_HOME"
fi
GATEWAY_PORT="$(pick_free_port)"
COMMS_PORT="$(pick_free_port)"
KNOWLEDGE_PORT="$(pick_free_port)"
WEB_FETCH_PORT="$(pick_free_port)"
EGRESS_PORT="$(pick_free_port)"
TOKEN="$(python3 - <<'PY'
import secrets
print(secrets.token_hex(32))
PY
)"
log "Using disposable Agency home: $SMOKE_HOME"
log "Using gateway port: $GATEWAY_PORT"

mkdir -p \
  "$SMOKE_HOME/agents" \
  "$SMOKE_HOME/audit" \
  "$SMOKE_HOME/infrastructure" \
  "$SMOKE_HOME/run"
chmod 700 "$SMOKE_HOME/audit"
cat >"$SMOKE_HOME/capacity.yaml" <<'EOF'
host_memory_mb: 8192
host_cpu_cores: 4
system_reserve_mb: 2048
infra_overhead_mb: 1264
runtime_backend: apple-vf-microvm
max_agents: 4
max_concurrent_meesks: 4
agent_slot_mb: 640
meeseeks_slot_mb: 640
network_pool_configured: false
EOF
cat >"$SMOKE_HOME/config.yaml" <<EOF
token: "$TOKEN"
egress_token: "$TOKEN"
gateway_addr: "127.0.0.1:$GATEWAY_PORT"
llm_provider: smoke
hub:
  deployment_backend: apple-vf-microvm
  deployment_backend_config:
    helper_binary: "$HELPER_BIN"
    kernel_path: "$KERNEL_PATH"
    mke2fs_path: "$MKE2FS_PATH"
    enforcer_binary_path: "$ENFORCER_BIN"
    vsock_bridge_binary_path: "$VSOCK_BRIDGE_BIN"
    rootfs_oci_ref: "$ROOTFS_OCI_REF"
    rootfs_size_mib: "$ROOTFS_SIZE_MIB"
EOF

log "Starting lifecycle host dependencies"
start_comms
start_dummy_services

log "Starting disposable Agency gateway"
AGENCY_HOME="$SMOKE_HOME" \
AGENCY_APPLE_VF_HELPER_BIN="$HELPER_BIN" \
AGENCY_APPLE_VF_KERNEL="$KERNEL_PATH" \
AGENCY_MKE2FS="$MKE2FS_PATH" \
AGENCY_APPLE_VF_ENFORCER_BIN="$ENFORCER_BIN" \
AGENCY_APPLE_VF_VSOCK_BRIDGE_BIN="$VSOCK_BRIDGE_BIN" \
AGENCY_GATEWAY_PORT="$GATEWAY_PORT" \
AGENCY_GATEWAY_PROXY_PORT="$COMMS_PORT" \
AGENCY_GATEWAY_PROXY_KNOWLEDGE_PORT="$KNOWLEDGE_PORT" \
AGENCY_WEB_FETCH_PORT="$WEB_FETCH_PORT" \
AGENCY_EGRESS_PROXY_PORT="$EGRESS_PORT" \
  "$AGENCY_BIN" serve --http "127.0.0.1:$GATEWAY_PORT" >"$SMOKE_HOME/gateway.log" 2>&1 &
GATEWAY_PID="$!"
if ! wait_gateway "$(api_url "/api/v1/health")"; then
  tail -n 120 "$SMOKE_HOME/gateway.log" >&2 || true
  fail "gateway did not become reachable"
fi

log "Creating disposable agent through gateway API"
api_json POST "/api/v1/agents" "{\"name\":\"$AGENT_NAME\",\"preset\":\"generalist\"}" >/dev/null
patch_agent_model

log "Starting agent through gateway lifecycle API"
api_stream_expect "/api/v1/agents/$AGENT_NAME/start" "complete"

STATUS_JSON="$(mktemp /tmp/agency-apple-vf-status.XXXXXX)"
api_json GET "/api/v1/agents/$AGENT_NAME/runtime/manifest" >/dev/null
api_json GET "/api/v1/agents/$AGENT_NAME/runtime/status" >"$STATUS_JSON"
assert_runtime_healthy "$STATUS_JSON"
api_json POST "/api/v1/agents/$AGENT_NAME/runtime/validate" '{}' >/dev/null

log "Restarting agent through gateway lifecycle API"
api_json POST "/api/v1/agents/$AGENT_NAME/restart" '{}' >/dev/null
api_json GET "/api/v1/agents/$AGENT_NAME/runtime/status" >"$STATUS_JSON"
assert_runtime_healthy "$STATUS_JSON"
api_json POST "/api/v1/agents/$AGENT_NAME/runtime/validate" '{}' >/dev/null

if [[ "$KEEP_AGENT" == "1" ]]; then
  log "Keeping disposable agent runtime: $AGENT_NAME"
else
  log "Stopping and deleting disposable agent"
  api_json POST "/api/v1/agents/$AGENT_NAME/stop" '{"type":"immediate","reason":"apple-vf lifecycle smoke"}' >/dev/null
  api_json DELETE "/api/v1/agents/$AGENT_NAME" >/dev/null
fi
rm -f "$STATUS_JSON"

log "Apple VF lifecycle smoke passed"
