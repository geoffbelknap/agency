#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
AGENCY_BIN="${AGENCY_BIN:-$ROOT/agency}"
AGENCY_HOME_DIR="${AGENCY_HOME:-}"
BUILD=1
KEEP_RUNNING=0
API_KEY="${OPENAI_API_KEY:-placeholder}"

usage() {
  cat <<'EOF'
Usage: ./scripts/apple-container-smoke.sh [--home PATH] [--skip-build] [--keep-running]

Runs a disposable Apple container backend setup smoke and prints diagnostics for
the current failure point. This script expects the Apple container service to
already be running (`container system start --enable-kernel-install`).

Options:
  --home PATH     Use this AGENCY_HOME. Defaults to a fresh /tmp directory.
  --skip-build    Reuse the existing ./agency binary.
  --keep-running  Leave the daemon running after diagnostics.
EOF
}

log() {
  printf '\n==> %s\n' "$1"
}

run_diag() {
  printf '\n$ %s\n' "$*"
  "$@" || true
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --home)
      [[ $# -ge 2 ]] || { echo "--home requires a path" >&2; exit 2; }
      AGENCY_HOME_DIR="$2"
      shift 2
      ;;
    --skip-build)
      BUILD=0
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
fi

cd "$ROOT"

log "environment"
printf 'repo=%s\n' "$ROOT"
printf 'agency_home=%s\n' "$AGENCY_HOME_DIR"
printf 'agency_bin=%s\n' "$AGENCY_BIN"

if [[ "$BUILD" == "1" ]]; then
  log "build"
  go build -o "$AGENCY_BIN" ./cmd/gateway
fi

log "container service"
run_diag container system status

log "port preflight"
if lsof -nP -iTCP:8200 -sTCP:LISTEN; then
  echo "port 8200 is already in use; stop the existing daemon before running this smoke" >&2
  exit 1
fi

log "setup"
set +e
AGENCY_HOME="$AGENCY_HOME_DIR" "$AGENCY_BIN" setup \
  --backend apple-container \
  --provider openai \
  --api-key "$API_KEY" \
  --no-browser
setup_status=$?
set -e
printf 'setup_exit=%s\n' "$setup_status"

log "gateway"
run_diag env AGENCY_HOME="$AGENCY_HOME_DIR" "$AGENCY_BIN" serve status
run_diag lsof -nP -iTCP:8200 -sTCP:LISTEN
run_diag curl -fsS "http://192.168.128.1:8200/api/v1/health"

log "host reverse bridge"
run_diag curl -v --max-time 5 "http://127.0.0.1:8202/channels"

log "containers"
run_diag container list --all --format json

log "gateway-proxy logs"
run_diag container logs -n 160 agency-infra-gateway-proxy

log "comms logs"
run_diag container logs -n 160 agency-infra-comms

log "comms image import"
run_diag container run --rm agency-comms:latest /bin/sh -c 'find /app/images/models -type f | sort; python - <<PY
import images.models.comms
print("images.models.comms ok")
PY'

log "gateway log tail"
if [[ -f "$AGENCY_HOME_DIR/gateway.log" ]]; then
  tail -n 160 "$AGENCY_HOME_DIR/gateway.log" || true
fi

if [[ "$KEEP_RUNNING" != "1" ]]; then
  log "cleanup daemon"
  env AGENCY_HOME="$AGENCY_HOME_DIR" "$AGENCY_BIN" serve stop || true
fi

exit "$setup_status"
