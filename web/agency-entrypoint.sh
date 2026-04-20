#!/bin/sh
set -e

GATEWAY_PORT="${AGENCY_GATEWAY_PORT:-8200}"
GATEWAY_HOST="${AGENCY_GATEWAY_HOST:-gateway}"
WEB_LISTEN="${AGENCY_WEB_LISTEN:-8280}"
NGINX_RUNTIME_DIR=/tmp/nginx
NGINX_RUNTIME_CONF="$NGINX_RUNTIME_DIR/nginx.conf"
NGINX_RUNTIME_CONFD="$NGINX_RUNTIME_DIR/conf.d"

mkdir -p "$NGINX_RUNTIME_CONFD"

if [ -f /etc/nginx/conf.d/agency-web.conf ]; then
  sed \
    -e "s/__AGENCY_GATEWAY_PORT__/${GATEWAY_PORT}/g" \
    -e "s/__AGENCY_GATEWAY_HOST__/${GATEWAY_HOST}/g" \
    -e "s/__AGENCY_WEB_LISTEN__/${WEB_LISTEN}/g" \
    /etc/nginx/conf.d/agency-web.conf > "$NGINX_RUNTIME_CONFD/agency-web.conf"
fi

if [ -f /etc/nginx/nginx.conf ]; then
  sed "s|include /etc/nginx/conf.d/\\*\\.conf;|include $NGINX_RUNTIME_CONFD/*.conf;|g" /etc/nginx/nginx.conf > "$NGINX_RUNTIME_CONF"
fi

exec nginx -e /dev/stderr -c "$NGINX_RUNTIME_CONF" -g 'daemon off;'
