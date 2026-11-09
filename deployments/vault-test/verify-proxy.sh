#!/usr/bin/env sh
set -eu

compose_file="${1:?usage: verify-proxy.sh <compose-file> <tenant-uuid> <active-kid> <network>}"
tenant_id="${2:?tenant UUID is required}"
active_kid="${3:?active Transit kid is required}"
network="${4:?network is required}"
other_tenant="00000000-0000-0000-0000-000000000000"
if [ "$other_tenant" = "$tenant_id" ]; then
  other_tenant="00000000-0000-0000-0000-000000000002"
fi

compose() {
  docker compose -f "$compose_file" --profile vault-test "$@"
}

proxy_vault() {
  compose exec -T \
    -e VAULT_ADDR=http://127.0.0.1:8100 \
    -e VAULT_TOKEN=client-token-must-be-ignored \
    vault-proxy vault "$@"
}

compose exec -T vault-proxy test ! -e /run/metapus/vault-bootstrap/wrapped-secret-id

proxy_vault read "transit/keys/metapus-signing-auth-$tenant_id" >/dev/null

readiness="$(proxy_vault read -format=json "tron/tenants/$tenant_id/config/readiness")"
printf '%s' "$readiness" | grep -Fq "$active_kid"
printf '%s' "$readiness" | grep -Fq "$network"

if proxy_vault read "tron/tenants/$tenant_id/config/authorization" >/dev/null 2>&1; then
  printf '%s\n' "worker policy unexpectedly permits TRON configuration reads" >&2
  exit 1
fi

if proxy_vault read "transit/keys/metapus-signing-auth-$other_tenant" >/dev/null 2>&1; then
  printf '%s\n' "worker policy unexpectedly permits a different tenant" >&2
  exit 1
fi

printf '%s\n' "Vault Proxy bootstrap, forced token, readiness, and tenant ACL checks passed."
