#!/bin/sh
# Bidirectional gateway proxy.
# Direction 1 (container→gateway): TCP:8200 → gateway daemon
# Direction 2 (gateway→container): TCP:PORT → TCP:service:8080
set -e

# Determine how to reach the gateway daemon.
# On Linux: Unix socket works through bind mount (native filesystem).
# On macOS Docker Desktop: Unix sockets don't work through bind mounts
# (VM boundary), so we use host.docker.internal:8200 instead.
GATEWAY_TARGET=""
if [ -S /run/gateway.sock ]; then
    # Test if the socket is actually connectable (fails on macOS Docker Desktop)
    if socat -T1 OPEN:/dev/null UNIX-CONNECT:/run/gateway.sock 2>/dev/null; then
        GATEWAY_TARGET="UNIX-CONNECT:/run/gateway.sock"
    fi
fi
if [ -z "$GATEWAY_TARGET" ]; then
    # Socket not usable — try host.docker.internal (macOS Docker Desktop)
    if ping -c1 -W1 host.docker.internal >/dev/null 2>&1; then
        GATEWAY_TARGET="TCP:host.docker.internal:8200"
        echo "gateway-proxy: using host.docker.internal (macOS Docker Desktop)"
    fi
fi
if [ -z "$GATEWAY_TARGET" ]; then
    echo "gateway-proxy: no route to gateway daemon (no socket, no host.docker.internal)"
    exit 1
fi
echo "gateway-proxy: target=$GATEWAY_TARGET"

# Direction 1: container→gateway
socat TCP-LISTEN:8200,fork,reuseaddr "$GATEWAY_TARGET" &

# Direction 2: gateway→services
socat TCP-LISTEN:8202,fork,reuseaddr TCP:comms:8080 &
socat TCP-LISTEN:8204,fork,reuseaddr TCP:knowledge:8080 &
socat TCP-LISTEN:8205,fork,reuseaddr TCP:intake:8080 &

echo "gateway-proxy: all bridges started"
wait -n
echo "gateway-proxy: a bridge process exited, shutting down"
exit 1
