#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

export AGENCY_TEST_OCI_LIVE=1

if ! command -v cosign >/dev/null 2>&1; then
  echo "cosign not found; OCI update/search will run, install signature-verification subtest will skip." >&2
fi

go test ./internal/hub -run 'TestOCILive(PullCatalogIndex|HubUpdateSearchInstallFlow)' -count=1 -v
