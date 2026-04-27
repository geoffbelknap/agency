#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HELPER_BIN="${AGENCY_APPLE_CONTAINER_WAIT_HELPER_BIN:-/tmp/agency-apple-container-wait-helper}"
CONTAINER_NAME="agency-wait-helper-smoke-$(date +%s)"

cleanup() {
  container delete --force "$CONTAINER_NAME" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM HUP

log() {
  printf '\n==> %s\n' "$1"
}

require_cmd() {
  printf '\n$ %s\n' "$*"
  "$@"
}

if [[ ! -x "$HELPER_BIN" ]]; then
  log "build wait helper"
  "$ROOT/scripts/readiness/apple-container-wait-helper-build.sh" "$HELPER_BIN" >/dev/null
fi

log "create stopped container"
require_cmd container create \
  --name "$CONTAINER_NAME" \
  --label agency.managed=true \
  --label agency.backend=apple-container \
  --label agency.home=manual-wait-helper-smoke \
  --label agency.agent=wait-helper-smoke \
  --label agency.type=workspace \
  docker.io/library/alpine:latest \
  /bin/sh -c "exit 7"

log "start and wait"
EVENTS="$("$HELPER_BIN" start-wait "$CONTAINER_NAME")"
printf '%s\n' "$EVENTS"

printf '%s\n' "$EVENTS" | ruby -rjson -e '
events = STDIN.each_line.map { |line| JSON.parse(line) }
types = events.map { |event| event["event_type"] }
abort("missing started event") unless types.include?("runtime.container.started")
exited = events.find { |event| event["event_type"] == "runtime.container.exited" }
abort("missing exited event") unless exited
abort("wrong exit code: #{exited.dig("data", "exit_code").inspect}") unless exited.dig("data", "exit_code") == 7
'

log "cleanup"
cleanup
