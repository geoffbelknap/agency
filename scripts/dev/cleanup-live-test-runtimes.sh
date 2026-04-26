#!/usr/bin/env bash
set -euo pipefail

APPLY=0
QUIET=0
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

if [ -n "${AGENCY_BIN:-}" ]; then
  AGENCY_CLI="$AGENCY_BIN"
elif [ -x "$ROOT_DIR/agency" ]; then
  AGENCY_CLI="$ROOT_DIR/agency"
else
  AGENCY_CLI="agency"
fi

usage() {
  cat <<'EOF'
Usage: ./scripts/dev/cleanup-live-test-runtimes.sh [--apply] [-q|--quiet]

Find and optionally stop Agency live-test runtimes that are attached to
temporary homes such as agency-live-home.*, agency-danger-home.*,
agency-oci-home.*, agency-operator-oci-home.*, or agency-setup-home.*.

By default this is a dry run. Pass --apply to terminate only matched test
runtime processes and remove scoped disposable infra/agent containers and
networks. When a local Agency gateway is available, it also archives matched
disposable direct-message channels and deletes matching disposable agent
records.
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

is_disposable_agent_name() {
  printf '%s\n' "$1" | grep -Eq '^(alpha-(setup|readiness)-[0-9]+|playwright-agent-[0-9]+|e2e-test-agent)$'
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

container_clis=()
for cli in docker podman; do
  if command -v "$cli" >/dev/null 2>&1; then
    container_clis+=("$cli")
  fi
done

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

for cli in "${container_clis[@]}"; do
  disposable_containers=()
  while IFS= read -r name; do
    [ -n "$name" ] || continue
    disposable_containers+=("$name")
  done < <(
    "$cli" ps -a --format '{{.Names}}' 2>/dev/null |
      grep -E '^agency-infra-(gateway-proxy|egress|comms|knowledge|intake|web-fetch|web|embeddings)-(agency-(live|danger|oci|operator-oci)-home-|agency-setup-home-)' || true
    "$cli" ps -a --format '{{.Names}}' 2>/dev/null |
      grep -E '^agency-(alpha-(setup|readiness)-[0-9]+|playwright-agent-[0-9]+|e2e-test-agent)-(workspace|enforcer)$' || true
  )

  if [ "${#disposable_containers[@]}" -gt 0 ]; then
    if [ "$APPLY" = "1" ]; then
      log "removing disposable infra containers via $cli: ${disposable_containers[*]}"
      "$cli" rm -f "${disposable_containers[@]}" >/dev/null 2>&1 || true
    else
      log "matched disposable infra containers via $cli: ${disposable_containers[*]}"
      log "Dry run only. Re-run with --apply to remove matched containers."
    fi
  fi

  disposable_networks=()
  while IFS= read -r name; do
    [ -n "$name" ] || continue
    disposable_networks+=("$name")
  done < <(
    "$cli" network ls --format '{{.Name}}' 2>/dev/null |
      grep -E '^agency-(gateway|egress-int|egress-ext|operator)-(agency-(live|danger|oci|operator-oci)-home-|agency-setup-home-)' || true
    "$cli" network ls --format '{{.Name}}' 2>/dev/null |
      grep -E '^agency-(alpha-(setup|readiness)-[0-9]+|playwright-agent-[0-9]+|e2e-test-agent)-internal$' || true
  )

  if [ "${#disposable_networks[@]}" -gt 0 ]; then
    if [ "$APPLY" = "1" ]; then
      log "removing disposable infra networks via $cli: ${disposable_networks[*]}"
      "$cli" network rm "${disposable_networks[@]}" >/dev/null 2>&1 || true
    else
      log "matched disposable infra networks via $cli: ${disposable_networks[*]}"
      log "Dry run only. Re-run with --apply to remove matched networks."
    fi
  fi
done

disposable_agents=()

add_disposable_agent() {
  local value="$1"
  local existing
  if [ "${#disposable_agents[@]}" -gt 0 ]; then
    for existing in "${disposable_agents[@]}"; do
      if [ "$existing" = "$value" ]; then
        return 0
      fi
    done
  fi
  disposable_agents+=("$value")
}

for cli in "${container_clis[@]}"; do
  while IFS= read -r name; do
    agent="$(printf '%s\n' "$name" | sed -E 's/^agency-//; s/-(workspace|enforcer)$//')"
    if is_disposable_agent_name "$agent"; then
      add_disposable_agent "$agent"
    fi
  done < <(
    "$cli" ps -a --format '{{.Names}}' 2>/dev/null |
      grep -E '^agency-(alpha-(setup|readiness)-[0-9]+|playwright-agent-[0-9]+|e2e-test-agent)-(workspace|enforcer)$' || true
  )
done

if command -v "$AGENCY_CLI" >/dev/null 2>&1; then
  while IFS= read -r channel; do
    agent="${channel#dm-}"
    if is_disposable_agent_name "$agent"; then
      add_disposable_agent "$agent"
    fi
  done < <(
    "$AGENCY_CLI" -q comms list --include-inactive |
      awk '/^[[:space:]]/{print $1}' |
      grep -E '^dm-(alpha-(setup|readiness)-[0-9]+|playwright-agent-[0-9]+|e2e-test-agent)$' || true
  )
fi

if [ "${#disposable_agents[@]}" -gt 0 ]; then
  if [ "$APPLY" = "1" ]; then
    if command -v "$AGENCY_CLI" >/dev/null 2>&1; then
      for agent in "${disposable_agents[@]}"; do
        log "deleting disposable agent state: $agent"
        "$AGENCY_CLI" -q delete "$agent" >/dev/null 2>&1 || true
        log "archiving disposable DM channel: dm-$agent"
        "$AGENCY_CLI" -q comms archive "dm-$agent" >/dev/null 2>&1 || true
      done
    else
      log "agency binary not found; cannot clean Agency agent/channel state"
    fi
  else
    log "matched disposable Agency agents/DMs: ${disposable_agents[*]}"
    log "Dry run only. Re-run with --apply to delete matched agent records and archive matched DMs."
  fi
fi
