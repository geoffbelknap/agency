#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
AGENCY_BIN="$ROOT/agency"
VERSION=""
BODY_REF=""
ENFORCER_REF=""
FIXTURES=()
SOURCE_HOME="$HOME/.agency"
HOME_DIR=""
HOST_ENFORCER=""
IMAGE_PREFIX="ghcr.io/geoffbelknap"
PORT="8299"
COMMS_PORT="8302"
KNOWLEDGE_PORT="8304"
WEB_FETCH_PORT="8306"
WEB_PORT="8380"
EGRESS_PORT="8412"
RESPONSE_TIMEOUT="120"
START_TIMEOUT="180"

usage() {
  cat <<'EOF'
Usage:
  scripts/dev/agent-loop-live-gate.sh --version VERSION [options]
  scripts/dev/agent-loop-live-gate.sh --body-ref REF --enforcer-ref REF [options]

Runs live agent-loop eval fixtures against disposable Agency homes using
published runtime OCI artifacts. Run this from a normal terminal on macOS; do
not run Apple Virtualization live work from the Codex sandbox.

Options:
  --version VERSION        Runtime artifact version, for example 0.3.19-dev-7a7fa33.
  --body-ref REF           Full body OCI ref. Overrides --version body ref.
  --enforcer-ref REF       Full enforcer OCI ref. Overrides --version enforcer ref.
  --fixture ID             Fixture to run. May be repeated. Default: basic_dm_alive.
  --agency-bin PATH        Agency binary to use. Default: repo ./agency.
  --source-home PATH       Source Agency home for credentials/routing. Default: ~/.agency.
  --home-dir PATH          Disposable Agency home. Default: short per-fixture path under /private/tmp.
  --image-prefix PREFIX    OCI image prefix for --version refs. Default: ghcr.io/geoffbelknap.
  --response-timeout SEC   Live eval response timeout. Default: 120.
  --start-timeout SEC      Agent start timeout. Default: 180.
  -h, --help               Show this help.
EOF
}

die() {
  echo "error: $*" >&2
  exit 1
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      [[ $# -ge 2 ]] || die "--version requires a value"
      VERSION="$2"
      shift 2
      ;;
    --body-ref)
      [[ $# -ge 2 ]] || die "--body-ref requires a value"
      BODY_REF="$2"
      shift 2
      ;;
    --enforcer-ref)
      [[ $# -ge 2 ]] || die "--enforcer-ref requires a value"
      ENFORCER_REF="$2"
      shift 2
      ;;
    --fixture)
      [[ $# -ge 2 ]] || die "--fixture requires a value"
      FIXTURES+=("$2")
      shift 2
      ;;
    --agency-bin)
      [[ $# -ge 2 ]] || die "--agency-bin requires a value"
      AGENCY_BIN="$2"
      shift 2
      ;;
    --source-home)
      [[ $# -ge 2 ]] || die "--source-home requires a value"
      SOURCE_HOME="$2"
      shift 2
      ;;
    --home-dir)
      [[ $# -ge 2 ]] || die "--home-dir requires a value"
      HOME_DIR="$2"
      shift 2
      ;;
    --image-prefix)
      [[ $# -ge 2 ]] || die "--image-prefix requires a value"
      IMAGE_PREFIX="$2"
      shift 2
      ;;
    --response-timeout)
      [[ $# -ge 2 ]] || die "--response-timeout requires a value"
      RESPONSE_TIMEOUT="$2"
      shift 2
      ;;
    --start-timeout)
      [[ $# -ge 2 ]] || die "--start-timeout requires a value"
      START_TIMEOUT="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

if [[ -n "$VERSION" ]]; then
  BODY_REF="${BODY_REF:-$IMAGE_PREFIX/agency-runtime-body:v$VERSION}"
  ENFORCER_REF="${ENFORCER_REF:-$IMAGE_PREFIX/agency-runtime-enforcer:v$VERSION}"
fi

[[ -n "$BODY_REF" ]] || die "missing --version or --body-ref"
[[ -n "$ENFORCER_REF" ]] || die "missing --version or --enforcer-ref"
[[ -x "$AGENCY_BIN" ]] || die "agency binary is not executable: $AGENCY_BIN"
[[ -f "$SOURCE_HOME/credentials/.key" ]] || die "missing source credential key: $SOURCE_HOME/credentials/.key"
[[ -f "$SOURCE_HOME/credentials/store.enc" ]] || die "missing source credential store: $SOURCE_HOME/credentials/store.enc"
[[ -f "$SOURCE_HOME/infrastructure/routing.yaml" ]] || die "missing source routing file: $SOURCE_HOME/infrastructure/routing.yaml"
command -v openssl >/dev/null || die "openssl is required"

if [[ ${#FIXTURES[@]} -eq 0 ]]; then
  FIXTURES=("basic_dm_alive")
fi

safe_id="${VERSION:-$(basename "$BODY_REF")}"
safe_id="$(printf '%s' "$safe_id" | tr -c 'A-Za-z0-9._-' '-')"
HOST_ENFORCER="${HOST_ENFORCER:-/private/tmp/agency-enforcer-host-${safe_id}}"

cleanup() {
  for port in "$PORT" "$COMMS_PORT" "$KNOWLEDGE_PORT" "$WEB_FETCH_PORT" "$WEB_PORT" "$EGRESS_PORT"; do
    if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/tmp/agency-live-gate-lsof.out 2>/dev/null; then
      awk 'NR > 1 {print $2}' /tmp/agency-live-gate-lsof.out | while read -r pid; do
        [[ -n "$pid" ]] && kill "$pid" 2>/dev/null || true
      done
    fi
  done
}
trap cleanup EXIT

cd "$ROOT"

if [[ ! -x "$HOST_ENFORCER" ]]; then
  go run ./cmd/runtime-oci-artifact \
    --extract-ref "$ENFORCER_REF" \
    --platform darwin/arm64 \
    --extract-path /usr/local/bin/enforcer \
    --output "$HOST_ENFORCER"
  chmod +x "$HOST_ENFORCER"
fi

prepare_home() {
  local fixture="$1"
  local fixture_home="$2"

  rm -rf "$fixture_home"
  mkdir -p "$fixture_home/credentials" "$fixture_home/infrastructure" "$fixture_home/infrastructure/egress/certs"

  openssl req \
    -x509 \
    -newkey rsa:2048 \
    -nodes \
    -keyout "$fixture_home/infrastructure/egress/certs/mitmproxy-ca-key.pem" \
    -out "$fixture_home/infrastructure/egress/certs/mitmproxy-ca-cert.pem" \
    -days 3650 \
    -subj "/CN=mitmproxy/O=mitmproxy" >/tmp/agency-live-gate-openssl.log 2>&1
  cat \
    "$fixture_home/infrastructure/egress/certs/mitmproxy-ca-key.pem" \
    "$fixture_home/infrastructure/egress/certs/mitmproxy-ca-cert.pem" \
    >"$fixture_home/infrastructure/egress/certs/mitmproxy-ca.pem"
  chmod 600 "$fixture_home/infrastructure/egress/certs/mitmproxy-ca.pem"
  chmod 644 "$fixture_home/infrastructure/egress/certs/mitmproxy-ca-cert.pem"

  cat >"$fixture_home/config.yaml" <<EOF
gateway_addr: 127.0.0.1:${PORT}
token: agency-loop-eval-token
llm_provider: anthropic
hub:
  deployment_backend: microagent
  deployment_backend_config:
    binary_path: microagent
    enforcer_binary_path: ${HOST_ENFORCER}
    entrypoint: /app/entrypoint.sh
    mke2fs_path: /opt/homebrew/opt/e2fsprogs/sbin/mke2fs
    rootfs_oci_ref: ${BODY_REF}
    state_dir: ${fixture_home}/runtime/microagent
EOF

  cat >"$fixture_home/capacity.yaml" <<'EOF'
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

  cp "$SOURCE_HOME/credentials/.key" "$SOURCE_HOME/credentials/store.enc" "$fixture_home/credentials/"
  cp "$SOURCE_HOME/infrastructure/routing.yaml" "$fixture_home/infrastructure/routing.yaml"
  if [[ -f "$SOURCE_HOME/infrastructure/credential-swaps.yaml" ]]; then
    cp "$SOURCE_HOME/infrastructure/credential-swaps.yaml" "$fixture_home/infrastructure/credential-swaps.yaml"
  fi
  if [[ -f "$SOURCE_HOME/infrastructure/credential-swaps.local.yaml" ]]; then
    cp "$SOURCE_HOME/infrastructure/credential-swaps.local.yaml" "$fixture_home/infrastructure/credential-swaps.local.yaml"
  fi
}

run_fixture() {
  local fixture="$1"
  local fixture_home="$2"

  prepare_home "$fixture" "$fixture_home"
  cleanup

  export AGENCY_HOME="$fixture_home"
  export AGENCY_GATEWAY_PROXY_PORT="$COMMS_PORT"
  export AGENCY_GATEWAY_PROXY_KNOWLEDGE_PORT="$KNOWLEDGE_PORT"
  export AGENCY_WEB_FETCH_PORT="$WEB_FETCH_PORT"
  export AGENCY_WEB_PORT="$WEB_PORT"
  export AGENCY_EGRESS_PROXY_PORT="$EGRESS_PORT"
  "$AGENCY_BIN" serve >/tmp/agency-live-gate-daemon.log 2>&1 &

  for _ in $(seq 1 40); do
    if "$AGENCY_BIN" serve status >/tmp/agency-live-gate-status.log 2>&1; then
      break
    fi
    sleep 0.25
  done

  "$ROOT/scripts/dev/dev-agent-loop-eval.sh" \
    --mode live \
    --fixture "$fixture" \
    --agency-bin "$AGENCY_BIN" \
    --response-timeout "$RESPONSE_TIMEOUT" \
    --start-timeout "$START_TIMEOUT"
}

failures=0
fixture_index=0
for fixture in "${FIXTURES[@]}"; do
  fixture_index=$((fixture_index + 1))
  fixture_home="${HOME_DIR:-/private/tmp/agl-${safe_id}-${fixture_index}}"
  if [[ ${#FIXTURES[@]} -gt 1 ]]; then
    printf '\n==> live fixture: %s\n' "$fixture"
  fi
  if ! run_fixture "$fixture" "$fixture_home"; then
    failures=$((failures + 1))
  fi
  cleanup
done

if [[ $failures -gt 0 ]]; then
  die "$failures live fixture(s) failed"
fi
