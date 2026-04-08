#!/bin/sh
# Bidirectional gateway proxy.
# Direction 1 (container→gateway): TCP:8200 → UNIX:/run/gateway.sock
# Direction 2 (gateway→container): TCP:PORT → TCP:service:8080
set -e

# Direction 1: container→gateway (existing behavior)
socat TCP-LISTEN:8200,fork,reuseaddr UNIX-CONNECT:/run/gateway.sock &

# Direction 2: gateway→services
socat TCP-LISTEN:8202,fork,reuseaddr TCP:comms:8080 &
socat TCP-LISTEN:8204,fork,reuseaddr TCP:knowledge:8080 &
socat TCP-LISTEN:8205,fork,reuseaddr TCP:intake:8080 &

echo "gateway-proxy: all bridges started"
wait -n
echo "gateway-proxy: a bridge process exited, shutting down"
exit 1
