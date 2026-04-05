#!/bin/sh
set -e

CERT_DIR=/tmp/certs
CERT_FILE="$CERT_DIR/cert.pem"
KEY_FILE="$CERT_DIR/key.pem"

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

exec nginx -g 'daemon off;'
