#!/usr/bin/env sh
set -eu

output_dir="${1:-./vault-test/tls}"
mkdir -p "$output_dir"
umask 077

work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT

openssl req -x509 -newkey rsa:3072 -sha256 -nodes -days 30 \
  -subj "/CN=Metapus Vault Test CA" \
  -keyout "$work_dir/ca.key" -out "$output_dir/ca.crt"

openssl req -newkey rsa:3072 -nodes -subj "/CN=vault-test" \
  -keyout "$output_dir/server.key" -out "$output_dir/server.csr"

cat > "$work_dir/server.ext" <<'EOF'
subjectAltName=DNS:vault-test,DNS:localhost,IP:127.0.0.1
extendedKeyUsage=serverAuth
EOF

openssl x509 -req -sha256 -days 30 \
  -in "$output_dir/server.csr" \
  -CA "$output_dir/ca.crt" -CAkey "$work_dir/ca.key" -CAcreateserial \
  -extfile "$work_dir/server.ext" -out "$output_dir/server.crt"

rm -f "$output_dir/server.csr" "$output_dir/ca.srl"
chmod 0400 "$output_dir/server.key"
chmod 0444 "$output_dir/ca.crt" "$output_dir/server.crt"
printf '%s\n' "Test TLS material created in $output_dir (expires in 30 days; CA private key was destroyed)."
