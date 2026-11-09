#!/usr/bin/env sh
set -eu

case "${PUBLIC_IP:-}" in
  ""|*[!0-9.]*)
    printf '%s\n' "PUBLIC_IP must be a literal IPv4 address" >&2
    exit 1
    ;;
esac

case "${NGINX_MODE:-https}" in
  acme)
    template="/opt/metapus/acme-only.conf.template"
    ;;
  https)
    template="/opt/metapus/https.conf.template"
    ;;
  *)
    printf '%s\n' "NGINX_MODE must be acme or https" >&2
    exit 1
    ;;
esac

sed "s/__PUBLIC_IP__/${PUBLIC_IP}/g" "$template" >/etc/nginx/conf.d/default.conf
nginx -t
nginx

stop_nginx() {
  nginx -s quit 2>/dev/null || true
}
trap stop_nginx INT TERM EXIT

marker="/etc/letsencrypt/.metapus-reload"
last_marker=""
while [ -f /var/run/nginx.pid ] && kill -0 "$(cat /var/run/nginx.pid)" 2>/dev/null; do
  sleep 30 &
  wait $!
  current_marker="$(stat -c %Y "$marker" 2>/dev/null || true)"
  if [ -n "$current_marker" ] && [ -n "$last_marker" ] && [ "$current_marker" != "$last_marker" ]; then
    nginx -t
    nginx -s reload
  fi
  last_marker="$current_marker"
done

wait "$(cat /var/run/nginx.pid 2>/dev/null || printf '0')" 2>/dev/null || true
