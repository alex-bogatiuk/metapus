#!/usr/bin/env sh
set -eu

case "${PUBLIC_IP:-}" in
  ""|*[!0-9.]*)
    printf '%s\n' "PUBLIC_IP must be a literal IPv4 address" >&2
    exit 1
    ;;
esac

while :; do
  if certbot renew \
    --non-interactive \
    --quiet \
    --cert-name "$PUBLIC_IP" \
    --preferred-profile shortlived \
    --webroot \
    --webroot-path /var/www/certbot \
    --deploy-hook "touch /etc/letsencrypt/.metapus-reload"; then
    printf '%s\n' "Certbot renewal check completed."
  else
    printf '%s\n' "Certbot renewal check failed; retrying in 12 hours." >&2
  fi
  sleep 43200 &
  wait $!
done
