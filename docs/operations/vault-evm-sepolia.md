# Vault EVM и Ethereum Sepolia

> **TL;DR:** EVM использует отдельный Vault plugin/mount и отдельный seed;
> preflight проверяет Sepolia RPC, PYUSD contract, Transit authorization и
> tenant-scoped custody до включения outbound policy.
>
> **Тип:** How-To
>
> **Аудитория:** DevOps, Security, Developer, QA
>
> **Связанные:** [Vault TRON production](vault-tron-production.md),
> [добавление blockchain network](../howto/add-blockchain-network.md),
> [Merchant API](../reference/merchant-public-api.md)

---

## Инварианты

- mount называется `evm` и не переиспользует TRON storage;
- `hd_seed` — отдельный 64-byte seed, созданный и переданный вне репозитория,
  shell history, CI output и application environment;
- v1 разрешает только chain ID `11155111`;
- worker не получает право читать config/seed или изменять allowlist;
- seed, private keys, Vault token и authorization credentials не сохраняются в
  PostgreSQL и не выводятся в логи;
- production topology содержит минимум два provider ID; любой wrong chain ID
  блокирует весь runtime.

## Регистрация plugin

Тестовый image уже содержит оба бинарника. Зарегистрируйте EVM plugin с
checksum именно установленного файла и включите отдельный mount:

```sh
EVM_SHA256="$(sha256sum /vault/plugins/vault-plugin-secrets-evm | awk '{print $1}')"
vault plugin register -sha256="$EVM_SHA256" secret vault-plugin-secrets-evm
vault secrets enable -path=evm -plugin-name=vault-plugin-secrets-evm plugin
```

Обновление бинарника выполняется отдельной change operation: новый checksum,
повторная регистрация, reload/rollout и readiness. Старый plugin не удаляется,
пока все уже подписанные транзакции не находятся под reconciliation.

## Tenant authorization config

Используйте тот же публичный Transit authorization key и раздельные approval
public keys, которые проверяются control plane. Private material остаётся в
Transit. Подготовьте operator-only JSON:

```json
{
  "schema_version": "evm_tenant_signing_config.v1",
  "authorization_keyring": {
    "transit/metapus-signing-auth-<tenant-id>/v1": "<Ed25519 public key>"
  },
  "policy_public_key": "<public key>",
  "intent_public_key": "<public key>",
  "risk_assessment_public_key": "<public key>",
  "policy_decision_public_key": "<public key>",
  "admin_approval_public_key": "<public key>",
  "risk_approval_public_key": "<public key>",
  "hd_seed": "<base64/hex encoded independent 64-byte seed>",
  "allowed_chain_ids": ["11155111"]
}
```

```sh
curl --fail --silent --show-error \
  -H "X-Vault-Token: $VAULT_TOKEN" -H "Content-Type: application/json" \
  --request POST --data @/secure/operator/evm-tenant-config.json \
  "$VAULT_ADDR/v1/evm/tenants/$TENANT_ID/config/authorization"
```

Все семь публичных ключей должны принадлежать разным ключам. Plugin отклоняет
дубликаты и другой chain ID.

## Derivation и sweep allowlist

Сначала выведите consolidation wallet из EVM mount:

```sh
curl --fail --silent --show-error \
  -H "X-Vault-Token: $VAULT_TOKEN" -H "Content-Type: application/json" \
  --request POST \
  --data '{"chain_id":"11155111","derivation_path":"m/44'\''/60'\''/0'\''/0/0"}' \
  "$VAULT_ADDR/v1/evm/tenants/$TENANT_ID/wallets/derive"
```

Разрешите только ранее выведенный EIP-55 address:

```json
{
  "schema_version": "evm_sweep_destinations.v1",
  "version": "2026-08-21.1",
  "destinations": {
    "11155111": ["<derived EIP-55 consolidation address>"]
  }
}
```

Запись выполняется в
`/v1/evm/tenants/<tenant-id>/config/sweep-destinations`. Readiness публикует
только наличие chain policy, а не seed или private key.

## Worker policy и preflight

Отрендерите `deployments/vault-test/worker-policy.hcl.template` с точным tenant
UUID. Wildcard запрещён. Worker получает `config/readiness`, `wallets/derive`
и `tx/sign` для mount `evm`, но не operator config paths.

Runtime config:

```env
APP_ENV=staging
VAULT_ADDR=http://vault-proxy:8100
CRYPTO_AUTH_SIGNER=transit
CRYPTO_TRANSIT_MOUNT=transit
CRYPTO_CHAIN_CONFIG_FILE=/run/config/crypto-chains.json
CRYPTO_PREFLIGHT_NETWORK_CODE=ETHEREUM-SEPOLIA
```

Запустите `cmd/crypto-preflight`. Gate проверяет chain ID, finalized tag,
`eth_feeHistory`, bytecode/name/symbol/decimals PYUSD, startup nonce
reconciliation и EVM custody readiness. Затем проверьте Admin runtime status.

## Rollout и rollback

1. Миграции до `ExpectedSchemaVersion` текущего бинарника (77), plugin и RPC config при выключенной сети.
2. Preflight inactive network.
3. Catalog network/token active, outbound policy и wallet allocation выключены.
4. Inbound ETH/PYUSD для тестовых tenants.
5. JIT и sweeps с минимальными limits, затем withdrawals/treasury.

Rollback запрещает новые allocations и outbound intents. Watcher,
confirmations, reconciliation и byte-identical resend уже подписанных
транзакций продолжают работать. При nonce divergence, deep reorg или reorg
проведённого payment сеть остаётся на hold до security/finance resolution.


## Проверка RPC ingestion

Настройки operation routing, параллельного native prefetch и отдельный read-only
`crypto-preflight -mode ingestion` описаны в [RPC ingestion runbook](rpc-ingestion.md).
Balance preflight выполняется при остановленном worker; ingestion preflight — при живом.
