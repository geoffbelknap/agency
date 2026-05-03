#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BACKEND="${AGENCY_RUNTIME_BACKEND:-auto}"
ROOTFS_OCI_REF="${AGENCY_MICROVM_ROOTFS_OCI_REF:-}"
ENFORCER_OCI_REF="${AGENCY_MICROVM_ENFORCER_OCI_REF:-}"
RUN_CORE=1
RUN_CONTRACT=1
RUN_WEB=0

usage() {
  cat <<'EOF'
Usage: ./scripts/readiness/microvm-smoke.sh [options]

Runs the supported microVM readiness path for the current host:
  - Agency validates the microagent backend contract.
  - microagent owns host runner coverage such as Apple VF and Firecracker.

Options:
  --backend auto|microagent
  --rootfs-oci-ref REF    Versioned body/rootfs OCI artifact reference.
                          Required for microagent release validation.
  --enforcer-oci-ref REF  Versioned enforcer OCI artifact reference.
                          Extracts the host-process enforcer when supplied.
  --skip-core             Skip git diff/status-check/go/web unit gates.
  --skip-contract         Skip the backend-neutral runtime contract smoke.
  --web                   Run the backend Web UI smoke after lifecycle checks.
  --skip-web              Do not run backend Web UI smoke. Default.

Environment:
  AGENCY_RUNTIME_BACKEND
  AGENCY_MICROVM_ROOTFS_OCI_REF
  AGENCY_MICROVM_ENFORCER_OCI_REF
EOF
}

log() {
  printf '==> %s\n' "$*"
}

fail() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

detect_backend() {
  case "$(uname -s)" in
    Darwin)
      [[ "$(uname -m)" == "arm64" ]] || fail "microagent readiness requires macOS arm64"
      printf 'microagent\n'
      ;;
    Linux)
      printf 'microagent\n'
      ;;
    *)
      fail "unsupported host OS for microVM smoke: $(uname -s)"
      ;;
  esac
}

run_core_gates() {
  log "Running core readiness gates"
  git -C "$ROOT" diff --check
  "$ROOT/scripts/ci/verify-required-status-checks.sh"
  (cd "$ROOT" && go vet ./...)
  (cd "$ROOT" && go test ./...)
  (cd "$ROOT/web" && npm test -- Infrastructure.test.tsx)
}

run_microagent() {
  [[ -n "$ROOTFS_OCI_REF" ]] || fail "--rootfs-oci-ref is required for microagent"

  log "Running microagent doctor"
  microagent doctor

  log "Running microagent lifecycle smoke"
  local args=(--rootfs-oci-ref "$ROOTFS_OCI_REF")
  if [[ -n "$ENFORCER_OCI_REF" ]]; then
    args+=(--enforcer-oci-ref "$ENFORCER_OCI_REF")
  fi
  if [[ "$RUN_CONTRACT" == "1" ]]; then
    args+=(--contract-smoke)
  fi
  "$ROOT/scripts/readiness/microagent-lifecycle-smoke.sh" "${args[@]}"

  if [[ "$RUN_WEB" == "1" ]]; then
    log "Running microagent Web UI smoke"
    local web_args=(
      --risky
      --backend microagent
      --rootfs-oci-ref "$ROOTFS_OCI_REF"
      --mock-llm
      --skip-build
      --
      --grep "microagent backend"
      --grep-invert "(destroy|Destroy|wipe|Wipe)"
    )
    if [[ -n "$ENFORCER_OCI_REF" ]]; then
      web_args=(--enforcer-oci-ref "$ENFORCER_OCI_REF" "${web_args[@]}")
    fi
    "$ROOT/scripts/e2e/e2e-live-disposable.sh" "${web_args[@]}"
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --backend)
      BACKEND="${2:-}"
      shift 2
      ;;
    --rootfs-oci-ref)
      ROOTFS_OCI_REF="${2:-}"
      shift 2
      ;;
    --enforcer-oci-ref)
      ENFORCER_OCI_REF="${2:-}"
      shift 2
      ;;
    --skip-core)
      RUN_CORE=0
      shift
      ;;
    --skip-contract)
      RUN_CONTRACT=0
      shift
      ;;
    --web)
      RUN_WEB=1
      shift
      ;;
    --skip-web)
      RUN_WEB=0
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

case "$BACKEND" in
  auto)
    BACKEND="$(detect_backend)"
    ;;
  microagent)
    ;;
  *)
    fail "unsupported backend: $BACKEND"
    ;;
esac

cd "$ROOT"
log "Selected backend: $BACKEND"
if [[ "$RUN_CORE" == "1" ]]; then
  run_core_gates
fi

run_microagent

log "microVM smoke passed for $BACKEND"
