#!/usr/bin/env sh
set -eu

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
  printf '%s\n' "usage: bootstrap-tls.sh <public-env-file> [public-ip]" >&2
  exit 2
fi

env_file="$1"
public_ip="${2:-176.215.182.82}"
script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
deploy_dir="$(dirname "$script_dir")"

if [ ! -f "$env_file" ]; then
  printf '%s\n' "Public test env file does not exist: $env_file" >&2
  exit 1
fi
env_file="$(CDPATH= cd -- "$(dirname -- "$env_file")" && pwd)/$(basename -- "$env_file")"

compose() {
  docker compose \
    --env-file "$env_file" \
    --project-name metapus-public-test \
    -f "$deploy_dir/docker-compose.yml" \
    -f "$deploy_dir/docker-compose.public-test.yml" \
    "$@"
}

set_env_file_value() {
  name="$1"
  value="$2"
  tmp_file="$(mktemp "${TMPDIR:-/tmp}/metapus-public-env.XXXXXX")"
  awk -v name="$name" -v value="$value" '
    BEGIN { found = 0 }
    index($0, name "=") == 1 { print name "=" value; found = 1; next }
    { print }
    END { if (!found) print name "=" value }
  ' "$env_file" >"$tmp_file"
  chmod 0600 "$tmp_file"
  mv "$tmp_file" "$env_file"
}

export PUBLIC_IP="$public_ip"
export NGINX_MODE=acme

certbot_version="$(compose --profile tls-tools run --rm certbot --version)"
version_number="$(printf '%s\n' "$certbot_version" | sed -n 's/.*certbot \([0-9][0-9]*\.[0-9][0-9]*\).*/\1/p')"
major="$(printf '%s' "$version_number" | cut -d. -f1)"
minor="$(printf '%s' "$version_number" | cut -d. -f2)"
if [ -z "$major" ] || [ "$major" -lt 5 ] || { [ "$major" -eq 5 ] && [ "$minor" -lt 4 ]; }; then
  printf '%s\n' "Certbot 5.4 or newer is required, got: $certbot_version" >&2
  exit 1
fi

compose up -d reverse-proxy

compose --profile tls-tools run --rm certbot certonly \
  --staging \
  --non-interactive \
  --agree-tos \
  --register-unsafely-without-email \
  --preferred-profile shortlived \
  --webroot \
  --webroot-path /var/www/certbot \
  --ip-address "$public_ip" \
  --cert-name "$public_ip-staging"

compose --profile tls-tools run --rm certbot certonly \
  --non-interactive \
  --agree-tos \
  --register-unsafely-without-email \
  --preferred-profile shortlived \
  --webroot \
  --webroot-path /var/www/certbot \
  --ip-address "$public_ip" \
  --cert-name "$public_ip"

export NGINX_MODE=https
set_env_file_value NGINX_MODE https
compose up -d --force-recreate reverse-proxy

compose --profile tls-tools run --rm certbot renew \
  --dry-run \
  --no-random-sleep-on-renew \
  --run-deploy-hooks \
  --cert-name "$public_ip" \
  --preferred-profile shortlived \
  --webroot \
  --webroot-path /var/www/certbot \
  --deploy-hook "touch /etc/letsencrypt/.metapus-reload"

compose --profile tls up -d certbot-renew

curl --fail --silent --show-error \
  --resolve "$public_ip:443:127.0.0.1" \
  "https://$public_ip/health/live"

compose --profile tls-tools run --rm certbot certificates --cert-name "$public_ip"
printf '%s\n' "Trusted IP certificate, HTTPS proxy, renewal dry-run, and 12-hour renewal loop are ready."
