#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
AGENCY_BIN="${AGENCY_BIN:-$ROOT/agency}"
ROOTFS_OCI_REF="${AGENCY_MICROVM_ROOTFS_OCI_REF:-}"
ENFORCER_OCI_REF="${AGENCY_MICROVM_ENFORCER_OCI_REF:-}"
BACKEND="${AGENCY_RUNTIME_BACKEND:-microagent}"
RUN_STATIC=1
RUN_RUNTIME=1
RUN_WEB_SAFE=1
RUN_WEB_RISKY=0
SKIP_BUILD=0

DESTROY_TEST_PATTERN="(destroy|Destroy|wipe|Wipe)"

usage() {
  cat <<'EOF'
Usage: ./scripts/readiness/full-surface-smoke.sh [options]

Runs the pre-RC surface smoke for Agency:
  - CLI command registration and help text
  - MCP discovery and core tool tests
  - supported microVM runtime smoke
  - live Web UI smoke, excluding only destroy/wipe feature tests

Options:
  --backend auto|microagent
  --rootfs-oci-ref REF
  --enforcer-oci-ref REF
  --include-risky-web  Include live-risky Web UI tests, still filtering
                       destroy/wipe tests.
  --skip-static        Skip static Go, Web unit, CLI, and MCP checks.
  --skip-runtime       Skip microVM runtime smoke.
  --skip-web           Skip live Web UI smoke.
  --skip-build         Reuse the current local Agency binary and images.
  -h, --help           Show this help.

Notes:
  This gate does not validate destroy/wipe as a feature. Ordinary delete,
  remove, archive, teardown, and cleanup flows remain in scope.
EOF
}

log() {
  printf '==> %s\n' "$*"
}

fail() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

run() {
  log "$*"
  "$@"
}

ensure_agency_binary() {
  if [[ "$SKIP_BUILD" == "1" ]]; then
    [[ -x "$AGENCY_BIN" ]] || fail "Agency binary is not executable: $AGENCY_BIN"
    return
  fi

  run make -C "$ROOT" build host-enforcer
  AGENCY_BIN="$ROOT/agency"
}

agency_help() {
  local command_path="$1"

  if [[ -z "$command_path" ]]; then
    "$AGENCY_BIN" --help >/dev/null
    return
  fi

  local parts=()
  read -r -a parts <<<"$command_path"
  "$AGENCY_BIN" "${parts[@]}" --help >/dev/null
}

run_cli_surface_smoke() {
  log "Checking CLI command surface"

  local cli_home
  local previous_home="${AGENCY_HOME:-}"
  local previous_experimental="${AGENCY_EXPERIMENTAL_SURFACES:-}"
  cli_home="$(mktemp -d "${TMPDIR:-/tmp}/agency-cli-surface.XXXXXX")"
  printf '{"checked_at":"2099-01-01T00:00:00Z","latest":"0.0.0","url":""}\n' >"$cli_home/update-check.json"
  export AGENCY_HOME="$cli_home"
  export AGENCY_EXPERIMENTAL_SURFACES=1

  local commands=(
    ""
    "start"
    "stop"
    "restart"
    "send"
    "status"
    "show"
    "list"
    "log"
    "create"
    "halt"
    "resume"
    "grant"
    "revoke"
    "comms list"
    "comms read"
    "comms create"
    "comms search"
    "comms archive"
    "infra up"
    "infra down"
    "infra status"
    "infra rebuild"
    "infra reload"
    "infra capacity"
    "admin doctor"
    "admin usage"
    "admin routing suggestions"
    "admin routing approve"
    "admin routing reject"
    "admin routing stats"
    "admin trust"
    "admin audit"
    "admin egress domains"
    "admin egress why"
    "admin rebuild"
    "admin department"
    "admin knowledge"
    "context push"
    "context status"
    "policy show"
    "policy validate"
    "runtime manifest"
    "runtime status"
    "runtime validate"
    "authz resolve"
    "cap list"
    "cap show"
    "cap enable"
    "cap disable"
    "cap add"
    "team create"
    "team list"
    "team show"
    "team activity"
    "mission create"
    "mission list"
    "mission show"
    "mission health"
    "mission update"
    "mission assign"
    "mission pause"
    "mission resume"
    "mission complete"
    "mission history"
    "event list"
    "event show"
    "event subscriptions"
    "webhook create"
    "webhook list"
    "webhook show"
    "webhook rotate-secret"
    "notify list"
    "notify add"
    "notify test"
    "audit summarize"
    "creds set"
    "creds list"
    "creds show"
    "creds rotate"
    "creds test"
    "creds group create"
    "registry list"
    "registry show"
    "registry update"
    "package list"
    "instance list"
    "instance create-from-package"
    "instance show"
    "instance validate"
    "instance update"
    "instance apply"
    "instance runtime"
    "hub search"
    "hub install"
    "hub deploy"
    "hub list"
    "hub update"
    "hub check"
    "hub doctor"
    "hub outdated"
    "hub upgrade"
    "hub add-source"
    "hub list-sources"
    "hub create"
    "hub audit"
    "hub publish"
    "hub info"
    "hub show"
    "hub activate"
    "hub deactivate"
    "hub provider add"
    "hub deployment create"
    "hub deployment configure"
    "hub deployment list"
    "hub deployment show"
    "hub deployment validate"
    "hub deployment apply"
    "hub deployment export"
    "hub deployment import"
    "hub deployment claim"
    "hub deployment release"
    "intake items"
    "intake poll"
    "intake stats"
    "graph query"
    "graph who-knows"
    "graph stats"
    "graph ingest"
    "graph insight"
    "graph export"
    "graph import"
    "graph import-graphiti"
    "graph import-config"
    "graph import-status"
    "graph classification list"
    "graph review list"
    "graph principals list"
    "graph ontology types"
    "graph ontology relationships"
    "graph ontology validate"
    "mcp-server"
    "runtime-authority-serve"
    "host-web-serve"
  )

  local command_path
  local status=0
  for command_path in "${commands[@]}"; do
    if ! agency_help "$command_path"; then
      status=1
      break
    fi
  done

  if [[ -n "$previous_home" ]]; then
    export AGENCY_HOME="$previous_home"
  else
    unset AGENCY_HOME
  fi
  if [[ -n "$previous_experimental" ]]; then
    export AGENCY_EXPERIMENTAL_SURFACES="$previous_experimental"
  else
    unset AGENCY_EXPERIMENTAL_SURFACES
  fi
  rm -rf "$cli_home"
  return "$status"
}

run_static_surface_smoke() {
  log "Running static CLI, MCP, API, and Web checks"
  run git -C "$ROOT" diff --check
  run go test ./...
  run npm --prefix "$ROOT/web" test
  run "$ROOT/scripts/readiness/web-ui-shell-guard.sh"
  run_cli_surface_smoke
}

ensure_web_dist() {
  if [[ "$RUN_WEB_SAFE" != "1" && "$RUN_STATIC" != "1" ]]; then
    return
  fi

  if [[ "$SKIP_BUILD" == "1" ]]; then
    run "$ROOT/scripts/readiness/web-ui-shell-guard.sh"
    return
  fi

  run npm --prefix "$ROOT/web" run build
  run "$ROOT/scripts/readiness/web-ui-shell-guard.sh"
}

run_runtime_smoke() {
  [[ -n "$ROOTFS_OCI_REF" ]] || fail "--rootfs-oci-ref is required unless --skip-runtime is set"

  local args=(
    --backend "$BACKEND"
    --rootfs-oci-ref "$ROOTFS_OCI_REF"
    --skip-core
  )
  if [[ -n "$ENFORCER_OCI_REF" ]]; then
    args+=(--enforcer-oci-ref "$ENFORCER_OCI_REF")
  fi

  run "$ROOT/scripts/readiness/microvm-smoke.sh" "${args[@]}"
}

run_web_smoke() {
  local common_args=(
    --skip-build
    --backend "$BACKEND"
    --mock-llm
    --
    --grep-invert "$DESTROY_TEST_PATTERN"
  )
  if [[ -n "$ROOTFS_OCI_REF" ]]; then
    common_args=(--rootfs-oci-ref "$ROOTFS_OCI_REF" "${common_args[@]}")
  fi
  if [[ -n "$ENFORCER_OCI_REF" ]]; then
    common_args=(--enforcer-oci-ref "$ENFORCER_OCI_REF" "${common_args[@]}")
  fi

  run "$ROOT/scripts/e2e/e2e-live-disposable.sh" "${common_args[@]}"

  if [[ "$RUN_WEB_RISKY" == "1" ]]; then
    run "$ROOT/scripts/e2e/e2e-live-disposable.sh" --risky "${common_args[@]}"
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
    --include-risky-web)
      RUN_WEB_RISKY=1
      shift
      ;;
    --skip-static)
      RUN_STATIC=0
      shift
      ;;
    --skip-runtime)
      RUN_RUNTIME=0
      shift
      ;;
    --skip-web)
      RUN_WEB_SAFE=0
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
    *)
      fail "unknown argument: $1"
      ;;
  esac
done

case "$BACKEND" in
  auto|microagent)
    ;;
  *)
    fail "unsupported backend: $BACKEND"
    ;;
esac

cd "$ROOT"
ensure_agency_binary
ensure_web_dist

if [[ "$RUN_STATIC" == "1" ]]; then
  run_static_surface_smoke
fi

if [[ "$RUN_RUNTIME" == "1" ]]; then
  run_runtime_smoke
fi

if [[ "$RUN_WEB_SAFE" == "1" ]]; then
  run_web_smoke
fi

log "full surface smoke passed"
