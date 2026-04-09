#!/bin/sh
set -e

CERT_DIR=/tmp/certs
CERT_FILE="$CERT_DIR/cert.pem"
KEY_FILE="$CERT_DIR/key.pem"
GATEWAY_PORT="${AGENCY_GATEWAY_PORT:-8200}"
NGINX_RUNTIME_DIR=/tmp/nginx
NGINX_RUNTIME_CONF="$NGINX_RUNTIME_DIR/nginx.conf"
NGINX_RUNTIME_CONFD="$NGINX_RUNTIME_DIR/conf.d"

# Generate a self-signed cert if none was mounted at the nginx cert path
if [ -f /etc/nginx/certs/cert.pem ] && [ -f /etc/nginx/certs/key.pem ]; then
  # User-provided certs — symlink so nginx finds them in the expected location
  mkdir -p "$CERT_DIR"
  ln -sf /etc/nginx/certs/cert.pem "$CERT_FILE"
  ln -sf /etc/nginx/certs/key.pem "$KEY_FILE"
else
  echo "No TLS certs found — generating self-signed certificate..."
  mkdir -p "$CERT_DIR"
  openssl req -x509 -nodes -days 365 \
    -newkey rsa:2048 \
    -keyout "$KEY_FILE" \
    -out "$CERT_FILE" \
    -subj "/CN=localhost" \
    -addext "subjectAltName=DNS:localhost,IP:127.0.0.1,IP:::1" \
    2>/dev/null
  echo "Self-signed TLS certificate generated."
fi

mkdir -p "$NGINX_RUNTIME_CONFD"

if [ -f /etc/nginx/conf.d/agency-web.conf ]; then
  sed "s/__AGENCY_GATEWAY_PORT__/${GATEWAY_PORT}/g" /etc/nginx/conf.d/agency-web.conf > "$NGINX_RUNTIME_CONFD/agency-web.conf"
fi

if [ -f /etc/nginx/nginx.conf ]; then
  sed "s|include /etc/nginx/conf.d/\\*\\.conf;|include $NGINX_RUNTIME_CONFD/*.conf;|g" /etc/nginx/nginx.conf > "$NGINX_RUNTIME_CONF"
fi

exec nginx -c "$NGINX_RUNTIME_CONF" -g 'daemon off;'
