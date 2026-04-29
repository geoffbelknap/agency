#!/usr/bin/env bash

set -euo pipefail

AGENCY_BIN="${AGENCY_BIN:-agency}"

exec env AGENCY_EXPERIMENTAL_SURFACES=1 "$AGENCY_BIN" admin doctor "$@"
