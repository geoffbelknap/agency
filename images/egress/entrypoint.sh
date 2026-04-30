#!/bin/bash
set -e

# Fetch external blocklists before starting the proxy
python3 /app/services/egress/fetch_blocklists.py /app/config/policy.yaml /app/blocklists

# Use persistent cert directory so CA survives container restarts
# and can be shared with workspace containers for trust.
exec mitmdump --listen-port 3128 \
    --set block_global=false \
    --set confdir=/app/certs \
    --scripts /app/services/egress/addon.py
