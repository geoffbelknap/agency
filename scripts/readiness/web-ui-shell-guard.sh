#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WEB_DIR="$ROOT/web"
DIST_INDEX="$WEB_DIR/dist/index.html"

fail() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

if [[ -f "$WEB_DIR/src/app/lib/contract-surface.ts" ]]; then
  fail "abandoned contract-shell source is present: web/src/app/lib/contract-surface.ts"
fi

if [[ -f "$WEB_DIR/components.json" ]]; then
  fail "abandoned contract-shell shadcn config is present: web/components.json"
fi

if rg -n "agency-contract-detail|contractModules|findModuleForSurface|moduleVisibleSurfaces" "$WEB_DIR/src" >/dev/null; then
  rg -n "agency-contract-detail|contractModules|findModuleForSurface|moduleVisibleSurfaces" "$WEB_DIR/src" >&2
  fail "abandoned contract-shell references are present in Web source"
fi

if rg -n "label:[[:space:]]*['\"](Operate|Govern|Extend)['\"]" "$WEB_DIR/src" >/dev/null; then
  rg -n "label:[[:space:]]*['\"](Operate|Govern|Extend)['\"]" "$WEB_DIR/src" >&2
  fail "abandoned contract-shell nav labels are present in Web source"
fi

if [[ ! -f "$DIST_INDEX" ]]; then
  fail "web/dist is missing; run npm --prefix web run build"
fi

newer_source="$(
  find \
    "$WEB_DIR/src" \
    "$WEB_DIR/package.json" \
    "$WEB_DIR/package-lock.json" \
    "$WEB_DIR/index.html" \
    "$WEB_DIR/vite.config.ts" \
    "$WEB_DIR/tsconfig.json" \
    -type f -newer "$DIST_INDEX" -print -quit
)"

if [[ -n "$newer_source" ]]; then
  fail "web/dist is stale; $newer_source is newer than web/dist/index.html"
fi

if rg -n "agency-contract-detail|contractModules|findModuleForSurface|moduleVisibleSurfaces" "$WEB_DIR/dist" >/dev/null; then
  rg -n "agency-contract-detail|contractModules|findModuleForSurface|moduleVisibleSurfaces" "$WEB_DIR/dist" >&2
  fail "abandoned contract-shell references are present in built Web assets"
fi

printf 'web UI shell guard passed\n'
