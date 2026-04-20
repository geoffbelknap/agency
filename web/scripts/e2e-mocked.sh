#!/usr/bin/env bash
set -euo pipefail

unset FORCE_COLOR
unset NO_COLOR

exec playwright test -c playwright.config.ts "$@"
