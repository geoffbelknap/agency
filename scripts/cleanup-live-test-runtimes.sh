#!/usr/bin/env bash
set -euo pipefail

APPLY=0
QUIET=0

usage() {
  cat <<'EOF'
Usage: ./scripts/cleanup-live-test-runtimes.sh [--apply] [-q|--quiet]

Find and optionally stop Agency live-test runtimes that are attached to
temporary homes such as agency-live-home.*, agency-danger-home.*,
agency-oci-home.*, agency-operator-oci-home.*, or agency-setup-home.*.

By default this is a dry run. Pass --apply to terminate only matched test
runtime processes and remove scoped disposable infra/agent containers and
networks.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --apply)
      APPLY=1
      shift
      ;;
    -q|--quiet)
      QUIET=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

log() {
  if [ "$QUIET" != "1" ]; then
    printf '%s\n' "$*"
  fi
}

is_test_runtime() {
  local pid="$1"
  lsof -nP -p "$pid" 2>/dev/null | grep -Eq '/agency-((live|danger|oci|operator-oci)-home|setup-home)\.'
}

stop_pid() {
  local pid="$1"
  local waited=0

  if ! kill -0 "$pid" 2>/dev/null; then
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

matched_pids=()
while IFS= read -r line; do
  pid="$(printf '%s' "$line" | awk '{print $1}')"
  if [ -z "$pid" ]; then
    continue
  fi
  command="$(printf '%s' "$line" | sed -E 's/^[[:space:]]*[0-9]+[[:space:]]+//')"

  case "$command" in
    *"agency serve"*|*"gateway serve"*|*"/exe/gateway serve"*)
      if is_test_runtime "$pid"; then
        matched_pids+=("$pid")
        log "matched test runtime pid=$pid command=$command"
      fi
      ;;
  esac
done < <(ps -axo pid=,command= 2>/dev/null || true)

if [ "${#matched_pids[@]}" -eq 0 ]; then
  log "No leaked Agency live-test runtime processes found."
else
  if [ "$APPLY" = "1" ]; then
    for pid in "${matched_pids[@]}"; do
      log "stopping pid=$pid"
      stop_pid "$pid"
    done
  else
    log "Dry run only. Re-run with --apply to stop matched processes."
  fi
fi

if command -v docker >/dev/null 2>&1; then
  disposable_containers=()
  while IFS= read -r name; do
    disposable_containers+=("$name")
  done < <(
    docker ps -a --format '{{.Names}}' 2>/dev/null |
      grep -E '^agency-infra-(egress|comms|knowledge|intake|web-fetch|web|embeddings)-(agency-(live|danger|oci|operator-oci)-home-|agency-setup-home-)' || true
    docker ps -a --format '{{.Names}}' 2>/dev/null |
      grep -E '^agency-(alpha-(setup|readiness)-[0-9]+|playwright-agent-[0-9]+|e2e-test-agent)-(workspace|enforcer)$' || true
  )

  if [ "${#disposable_containers[@]}" -gt 0 ]; then
    if [ "$APPLY" = "1" ]; then
      log "removing disposable infra containers: ${disposable_containers[*]}"
      docker rm -f "${disposable_containers[@]}" >/dev/null 2>&1 || true
    else
      log "matched disposable infra containers: ${disposable_containers[*]}"
      log "Dry run only. Re-run with --apply to remove matched containers."
    fi
  fi

  disposable_networks=()
  while IFS= read -r name; do
    disposable_networks+=("$name")
  done < <(
    docker network ls --format '{{.Name}}' 2>/dev/null |
      grep -E '^agency-(gateway|egress-int|egress-ext|operator)-(agency-(live|danger|oci|operator-oci)-home-|agency-setup-home-)' || true
    docker network ls --format '{{.Name}}' 2>/dev/null |
      grep -E '^agency-(alpha-(setup|readiness)-[0-9]+|playwright-agent-[0-9]+|e2e-test-agent)-internal$' || true
  )

  if [ "${#disposable_networks[@]}" -gt 0 ]; then
    if [ "$APPLY" = "1" ]; then
      log "removing disposable infra networks: ${disposable_networks[*]}"
      docker network rm "${disposable_networks[@]}" >/dev/null 2>&1 || true
    else
      log "matched disposable infra networks: ${disposable_networks[*]}"
      log "Dry run only. Re-run with --apply to remove matched networks."
    fi
  fi
fi
