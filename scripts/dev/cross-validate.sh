#!/usr/bin/env bash
# scripts/dev/cross-validate.sh — Run both Go and Python against shared fixtures.
# Usage: ./scripts/dev/cross-validate.sh <model_name>
# Example: ./scripts/dev/cross-validate.sh org

set -euo pipefail

MODEL="${1:?Usage: cross-validate.sh <model_name>}"
FIXTURES_DIR="$(dirname "$0")/../testdata/models/${MODEL}"
PASS=0
FAIL=0

if [ ! -d "$FIXTURES_DIR" ]; then
    echo "ERROR: No fixtures at $FIXTURES_DIR"
    exit 1
fi

for fixture in "$FIXTURES_DIR"/*.yaml; do
    base=$(basename "$fixture")
    expect_valid=true
    if [[ "$base" == invalid_* ]]; then
        expect_valid=false
    fi

    # Go validation
    go_result=$(cd "$(dirname "$0")/../.." && go run ./cmd/validate/ "$fixture" 2>&1 && echo "PASS" || echo "FAIL")
    go_pass=$([[ "$go_result" == *PASS* ]] && echo true || echo false)

    # Python validation
    py_result=$(python3 -c "
from agency.models import validate_file
try:
    validate_file('$fixture')
    print('PASS')
except Exception as e:
    print('FAIL')
" 2>&1)
    py_pass=$([[ "$py_result" == *PASS* ]] && echo true || echo false)

    if [ "$go_pass" != "$py_pass" ]; then
        echo "DIVERGENCE: $base — Go=$go_pass Python=$py_pass"
        FAIL=$((FAIL + 1))
    else
        PASS=$((PASS + 1))
    fi
done

echo "Results: $PASS agree, $FAIL diverge"
[ "$FAIL" -eq 0 ] || exit 1
