#!/bin/sh
# Bidirectional gateway proxy.
# Direction 1 (container→gateway): TCP:8200 → gateway daemon
# Direction 2 (gateway→container): TCP:PORT → TCP:service:8080
set -e

COMMS_HOST="${AGENCY_COMMS_HOST:-comms}"
COMMS_PORT="${AGENCY_COMMS_PORT:-8080}"
KNOWLEDGE_HOST="${AGENCY_KNOWLEDGE_HOST:-knowledge}"
KNOWLEDGE_PORT="${AGENCY_KNOWLEDGE_PORT:-8080}"
INTAKE_HOST="${AGENCY_INTAKE_HOST:-intake}"
INTAKE_PORT="${AGENCY_INTAKE_PORT:-8080}"

# Determine how to reach the gateway daemon.
# On Linux: Unix socket works through bind mount (native filesystem).
# On VM-backed runtimes: Unix sockets don't work through bind mounts
# (VM boundary), so we use the configured host gateway alias.
HOST_GATEWAY_PORT="${AGENCY_HOST_GATEWAY_PORT:-8200}"
HOST_GATEWAY_HOSTS="${AGENCY_HOST_GATEWAY_HOSTS:-host.docker.internal,host.containers.internal}"
GATEWAY_TARGET=""
if [ -S /run/gateway.sock ]; then
    # Test if the socket is actually connectable; VM-backed host backends may
    # expose the mount while still blocking Unix socket forwarding.
    if socat -T1 OPEN:/dev/null UNIX-CONNECT:/run/gateway.sock 2>/dev/null; then
        GATEWAY_TARGET="UNIX-CONNECT:/run/gateway.sock"
    fi
fi
if [ -z "$GATEWAY_TARGET" ]; then
    # Socket not usable — try host aliases provided by the backend contract.
    # Name resolution is the portable capability we rely on here; ICMP reachability
    # is not guaranteed across runtimes and is not required for the TCP bridge.
    OLD_IFS="$IFS"
    IFS=","
    for host in $HOST_GATEWAY_HOSTS; do
        host=$(printf '%s' "$host" | tr -d ' ')
        if [ -n "$host" ] && getent hosts "$host" >/dev/null 2>&1; then
            GATEWAY_TARGET="TCP:${host}:${HOST_GATEWAY_PORT}"
            echo "gateway-proxy: using ${host}"
            break
        fi
    done
    IFS="$OLD_IFS"
fi
if [ -z "$GATEWAY_TARGET" ]; then
    echo "gateway-proxy: no route to gateway daemon (no socket, no reachable host alias in ${HOST_GATEWAY_HOSTS})"
    exit 1
fi
echo "gateway-proxy: target=$GATEWAY_TARGET"

# Direction 1: container→gateway
socat TCP-LISTEN:8200,fork,reuseaddr "$GATEWAY_TARGET" &

# Direction 2: gateway→services
echo "gateway-proxy: service targets comms=${COMMS_HOST}:${COMMS_PORT} knowledge=${KNOWLEDGE_HOST}:${KNOWLEDGE_PORT} intake=${INTAKE_HOST}:${INTAKE_PORT}"
socat TCP-LISTEN:8202,fork,reuseaddr TCP:${COMMS_HOST}:${COMMS_PORT} &
socat TCP-LISTEN:8204,fork,reuseaddr TCP:${KNOWLEDGE_HOST}:${KNOWLEDGE_PORT} &
socat TCP-LISTEN:8205,fork,reuseaddr TCP:${INTAKE_HOST}:${INTAKE_PORT} &

echo "gateway-proxy: all bridges started"
wait -n
echo "gateway-proxy: a bridge process exited, shutting down"
exit 1
