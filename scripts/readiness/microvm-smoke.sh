#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BACKEND="${AGENCY_RUNTIME_BACKEND:-auto}"
ROOTFS_OCI_REF="${AGENCY_MICROVM_ROOTFS_OCI_REF:-}"
RUN_CORE=1
RUN_CONTRACT=1
RUN_WEB=0

usage() {
  cat <<'EOF'
Usage: ./scripts/readiness/microvm-smoke.sh [options]

Runs the supported microVM readiness path for the current host:
  - macOS Apple silicon: apple-vf-microvm
  - Linux/WSL: firecracker

Options:
  --backend auto|apple-vf-microvm|firecracker
  --rootfs-oci-ref REF    Versioned body/rootfs OCI artifact reference.
                          Required for apple-vf-microvm. Firecracker currently
                          uses its backend smoke's local OCI rootfs path.
  --skip-core             Skip git diff/status-check/go/web unit gates.
  --skip-contract         Skip the backend-neutral runtime contract smoke.
  --web                   Run the backend Web UI smoke after lifecycle checks.
  --skip-web              Do not run backend Web UI smoke. Default.

Environment:
  AGENCY_RUNTIME_BACKEND
  AGENCY_MICROVM_ROOTFS_OCI_REF
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
      [[ "$(uname -m)" == "arm64" ]] || fail "apple-vf-microvm requires macOS arm64"
      printf 'apple-vf-microvm\n'
      ;;
    Linux)
      printf 'firecracker\n'
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

run_apple_vf() {
  [[ -n "$ROOTFS_OCI_REF" ]] || fail "--rootfs-oci-ref is required for apple-vf-microvm"

  local agent="apple-vf-contract-$(git -C "$ROOT" rev-parse --short HEAD)"
  local home="/tmp/agency-apple-vf-contract-$(git -C "$ROOT" rev-parse --short HEAD)"

  cleanup_apple_vf() {
    if [[ -d "$home" && -x /tmp/agency-apple-vf-lifecycle ]]; then
      env AGENCY_HOME="$home" /tmp/agency-apple-vf-lifecycle stop "$agent" >/dev/null 2>&1 || true
      env AGENCY_HOME="$home" /tmp/agency-apple-vf-lifecycle delete "$agent" >/dev/null 2>&1 || true
      env AGENCY_HOME="$home" /tmp/agency-apple-vf-lifecycle serve stop >/dev/null 2>&1 || true
    fi
  }
  trap cleanup_apple_vf RETURN

  log "Verifying Apple VF artifacts"
  "$ROOT/scripts/readiness/apple-vf-artifacts.sh" --verify-existing

  log "Running Apple VF doctor"
  "$ROOT/agency" admin doctor

  log "Running Apple VF lifecycle smoke"
  "$ROOT/scripts/readiness/apple-vf-lifecycle-smoke.sh" --rootfs-oci-ref "$ROOTFS_OCI_REF"

  if [[ "$RUN_CONTRACT" == "1" ]]; then
    log "Running Apple VF lifecycle smoke with kept runtime for contract smoke"
    "$ROOT/scripts/readiness/apple-vf-lifecycle-smoke.sh" \
      --home "$home" \
      --agent "$agent" \
      --rootfs-oci-ref "$ROOTFS_OCI_REF" \
      --keep-agent

    log "Running backend-neutral runtime contract smoke"
    "$ROOT/scripts/readiness/runtime-contract-smoke.sh" \
      --agent "$agent" \
      --home "$home" \
      --start-gateway \
      --skip-tests
    cleanup_apple_vf
  fi

  if [[ "$RUN_WEB" == "1" ]]; then
    log "Running Apple VF Web UI smoke"
    "$ROOT/scripts/e2e/apple-vf-webui-smoke.sh"
  fi
}

run_firecracker() {
  log "Verifying Firecracker artifacts"
  "$ROOT/scripts/readiness/firecracker-artifacts.sh" --verify-existing
  "$ROOT/scripts/readiness/firecracker-kernel-artifacts.sh" --verify-existing

  log "Running Firecracker doctor"
  "$ROOT/agency" admin doctor

  if [[ "$RUN_CONTRACT" != "1" ]]; then
    log "Running Firecracker lifecycle smoke"
    "$ROOT/scripts/readiness/firecracker-microvm-smoke.sh"
  else
    local out
    local pid=""
    local pid_is_group=0
    local contract_cmd=""
    out="$(mktemp /tmp/agency-firecracker-keep-agent.XXXXXX.log)"
    cleanup_firecracker() {
      [[ -n "$pid" ]] || return
      kill -0 "$pid" >/dev/null 2>&1 || return

      local target="$pid"
      if [[ "$pid_is_group" == "1" ]]; then
        target="-$pid"
      fi

      kill -INT "$target" >/dev/null 2>&1 || true
      for _ in $(seq 1 30); do
        if ! kill -0 "$pid" >/dev/null 2>&1; then
          wait "$pid" >/dev/null 2>&1 || true
          return
        fi
        sleep 1
      done

      kill -TERM "$target" >/dev/null 2>&1 || true
      for _ in $(seq 1 10); do
        if ! kill -0 "$pid" >/dev/null 2>&1; then
          wait "$pid" >/dev/null 2>&1 || true
          return
        fi
        sleep 1
      done

      kill -KILL "$target" >/dev/null 2>&1 || true
      wait "$pid" >/dev/null 2>&1 || true
    }
    trap cleanup_firecracker RETURN

    log "Running Firecracker lifecycle smoke with kept runtime for contract smoke"
    if command -v setsid >/dev/null 2>&1; then
      setsid "$ROOT/scripts/readiness/firecracker-microvm-smoke.sh" --keep-agent >"$out" 2>&1 &
      pid_is_group=1
    else
      "$ROOT/scripts/readiness/firecracker-microvm-smoke.sh" --keep-agent >"$out" 2>&1 &
    fi
    pid="$!"

    for _ in $(seq 1 180); do
      if ! kill -0 "$pid" >/dev/null 2>&1; then
        cat "$out" >&2 || true
        fail "Firecracker keep-agent smoke exited before printing contract command"
      fi
      contract_cmd="$(awk -F= '/^contract_smoke_command=/ {print $2; exit}' "$out")"
      [[ -n "$contract_cmd" ]] && break
      sleep 1
    done
    [[ -n "$contract_cmd" ]] || {
      cat "$out" >&2 || true
      fail "Firecracker keep-agent smoke did not print contract command"
    }

    log "Running backend-neutral runtime contract smoke"
    (cd "$ROOT" && bash -lc "$contract_cmd")
    cleanup_firecracker
    cat "$out"
  fi

  if [[ "$RUN_WEB" == "1" ]]; then
    log "Running Firecracker Web UI smoke"
    "$ROOT/scripts/e2e/firecracker-webui-smoke.sh" all
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
  apple-vf-microvm|firecracker)
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

case "$BACKEND" in
  apple-vf-microvm)
    run_apple_vf
    ;;
  firecracker)
    run_firecracker
    ;;
esac

log "microVM smoke passed for $BACKEND"
