#!/bin/bash
# Build all Agency container images.
# Usage: ./scripts/build_images.sh [service...]
# Examples:
#   ./scripts/build_images.sh          # build all
#   ./scripts/build_images.sh comms    # build just comms

set -e
cd "$(dirname "$0")/.."

# Services that need agency_core/ as build context (they import agency_core.models or agency_core.images.*)
REPO_CONTEXT_SERVICES="comms knowledge intake"

# Self-contained services (build context is their own directory)
LOCAL_CONTEXT_SERVICES="enforcer egress mesh"

ALL_SERVICES="$REPO_CONTEXT_SERVICES $LOCAL_CONTEXT_SERVICES"

if [ $# -gt 0 ]; then
    ALL_SERVICES="$*"
fi

for svc in $ALL_SERVICES; do
    echo "=== Building agency-$svc ==="
    if echo "$REPO_CONTEXT_SERVICES" | grep -qw "$svc"; then
        # Build with agency_core/ as context so Dockerfile can COPY models/, exceptions.py
        docker build -t "agency-$svc:latest" -f "agency_core/images/$svc/Dockerfile" "agency_core/"
    else
        # Self-contained — build context is the image directory
        docker build -t "agency-$svc:latest" "agency_core/images/$svc/"
    fi
    echo ""
done

echo "Done. Built: $ALL_SERVICES"
