#!/usr/bin/env bash
# Generate a self-signed cert for local-only use (127.0.0.1).
# For public access, use a real cert (Caddy auto-TLS / certbot) on the reverse proxy.

set -euo pipefail

OUT_DIR=${1:-$HOME/.ops-panel}
mkdir -p "$OUT_DIR"
chmod 700 "$OUT_DIR"

CERT="$OUT_DIR/cert.pem"
KEY="$OUT_DIR/key.pem"

if [ -f "$CERT" ] && [ -f "$KEY" ]; then
  echo "cert already exists at $CERT — remove it first if you want to regenerate."
  exit 0
fi

openssl req -x509 -newkey rsa:4096 -sha256 -days 825 -nodes \
  -keyout "$KEY" -out "$CERT" \
  -subj "/CN=ops-panel-local" \
  -addext "subjectAltName=DNS:localhost,IP:127.0.0.1,IP:::1"

chmod 600 "$KEY"
chmod 644 "$CERT"

echo "wrote:"
echo "  $CERT"
echo "  $KEY"
echo
echo "put these in your config.json under tls_cert_file / tls_key_file"
