#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
WEB_DIR="${AGENCY_WEB_DIR:-$ROOT_DIR/web}"

log() {
  printf '==> %s\n' "$*"
}

fail() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "Missing required command: $1"
}

run_root() {
  (cd "$ROOT_DIR" && "$@")
}

run_enforcer() {
  (cd "$ROOT_DIR/images/enforcer" && "$@")
}

run_web() {
  (cd "$WEB_DIR" && "$@")
}

require_cmd go
require_cmd npm

[ -d "$WEB_DIR" ] || fail "Web directory not found: $WEB_DIR"

log "Checking provider-tool catalog, routing, and infra API"
run_root go test ./internal/providercatalog ./internal/models ./internal/api/infra

log "Checking enforcer provider-tool mediation"
run_enforcer go test ./...

log "Checking provider-tool admin and usage UI"
run_web npm test -- AdminProviderTools Usage

log "Provider tools readiness check passed"
