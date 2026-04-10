#!/usr/bin/env bash
set -euo pipefail

AGENCY_BIN="${AGENCY_BIN:-agency}"
AGENCY_HOME_DIR="${AGENCY_HOME:-$HOME/.agency}"
EXPECTED_REGISTRY="ghcr.io/geoffbelknap/agency-hub"
PROVIDER_NAME="${AGENCY_HUB_READINESS_PROVIDER:-gemini}"

log() {
  printf '==> %s\n' "$*"
}

fail() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

run_agency() {
  "$AGENCY_BIN" -q "$@"
}

log "Checking configured hub sources"
sources="$(run_agency hub list-sources)"
printf '%s\n' "$sources" | grep -q 'oci' ||
  fail "No OCI hub source is listed"
printf '%s\n' "$sources" | grep -q "$EXPECTED_REGISTRY" ||
  fail "Expected hub registry $EXPECTED_REGISTRY is not listed"

log "Syncing hub catalog from OCI"
run_agency hub update >/dev/null

log "Checking provider catalog entry: $PROVIDER_NAME"
run_agency hub search "$PROVIDER_NAME" | grep -q "$PROVIDER_NAME" ||
  fail "Provider $PROVIDER_NAME was not found in hub search"

provider_info="$(run_agency hub info "$PROVIDER_NAME")"
printf '%s\n' "$provider_info" | grep -q '"_kind": "provider"' ||
  fail "Hub info for $PROVIDER_NAME is not a provider"
printf '%s\n' "$provider_info" | grep -q "\"_source\":" ||
  fail "Hub info for $PROVIDER_NAME does not include source metadata"

source_name="$(printf '%s\n' "$provider_info" | awk -F'"' '/"_source":/ { print $4; exit }')"
if [ -z "$source_name" ]; then
  fail "Could not determine source name for $PROVIDER_NAME"
fi

source_cache="$AGENCY_HOME_DIR/hub-cache/$source_name"
if [ -d "$source_cache/.git" ]; then
  fail "OCI source cache still contains legacy git metadata at $source_cache/.git"
fi

if [ "$source_name" != "official" ] && [ -d "$AGENCY_HOME_DIR/hub-cache/official/.git" ]; then
  fail "Unconfigured legacy official git cache still exists"
fi

if ! run_agency hub show "$PROVIDER_NAME" >/dev/null 2>&1; then
  fail "Provider $PROVIDER_NAME is available in the catalog but not installed"
fi

log "Hub OCI readiness check passed"
