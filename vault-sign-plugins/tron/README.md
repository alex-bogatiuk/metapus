# TRON Vault plugin для Metapus

Plugin является policy-aware границей подписи, а не универсальным API доступа
к private keys. Он выводит tenant-scoped TRON wallets из seal-wrapped HD seed и
подписывает только транзакции, совпадающие с проверенной авторизацией Metapus.

## Пути API

| Метод | Путь | Назначение |
|---|---|---|
| `POST` | `tenants/{tenant}/config/authorization` | Только оператор: keyring v3, approval keys и HD seed |
| `GET` | `tenants/{tenant}/config/authorization` | Только оператор: публичный статус конфигурации без HD seed |
| `POST` | `tenants/{tenant}/config/sweep-destinations` | Только оператор: allowlist выведенных consolidation addresses |
| `GET` | `tenants/{tenant}/config/sweep-destinations` | Только оператор: чтение allowlist |
| `GET` | `tenants/{tenant}/config/readiness` | Несекретный readiness-статус для worker |
| `POST` | `tenants/{tenant}/wallets/derive` | Вывод wallet для operator/address-service |
| `POST` | `tenants/{tenant}/tx/sign` | Подпись транзакции для worker |

## Конфигурация авторизации

Конфигурация использует `tron_tenant_signing_config.v3`.
`authorization_keyring` связывает versioned Transit key ID с Ed25519 public key:

```json
{
  "schema_version": "tron_tenant_signing_config.v3",
  "authorization_keyring": {
    "transit/metapus-signing-auth-00000000-0000-0000-0000-000000000001/v1": "<Ed25519 public key>"
  },
  "policy_public_key": "...",
  "intent_public_key": "...",
  "risk_assessment_public_key": "...",
  "policy_decision_public_key": "...",
  "admin_approval_public_key": "...",
  "risk_approval_public_key": "...",
  "hd_seed": "<encoded 64-byte BIP-39 seed>"
}
```

Authorization key и все approval-role keys должны различаться. Неизвестный или
удалённый `kid` приводит к fail-closed отказу.

## Инварианты подписи

- tenant/path, source wallet, network, `vault_key_id`, destination, token,
  amount, fee limit, contract, raw transaction, txID, expiration и business
  basis должны совпадать с authorization;
- withdrawal и treasury withdrawal требуют полный подписанный approval bundle;
- digest точного payload `IntentJWS` можно употребить только один раз, поэтому
  новый authorization ID или attempt не позволяют повторно использовать
  approval для другой транзакции;
- exact retry возвращает сохранённую подписанную транзакцию;
- sweep не содержит approval bundle и может идти только на адрес из allowlist,
  ранее выведенный для того же tenant и network;
- sweep и activation идемпотентны по business basis.

Приватные wallet keys, HD seed, записи об употреблении intent и подписанные
транзакции остаются seal-wrapped в storage plugin.

## Сборка и эксплуатация

Зафиксированный образ Vault с plugin находится в
`deployments/vault-test/Dockerfile`. Production provisioning, bootstrap
Proxy/AppRole, policies, rotation и перенос во внешний Vault описаны в
[`docs/operations/vault-tron-production.md`](../../docs/operations/vault-tron-production.md).

Запуск тестов из этого каталога:

```sh
go test ./...
```


## AML signing contract

`GET tenants/<tenant UUID>/config/readiness` advertises binary support using
`aml_signing_schema=aml_signing.v1`, `aml_intent_schema=money_movement_intent.v2`
and `aml_source_line_basis=CryptoAMLWithdrawalLine`. The contract binds explicit
risk acceptance and the complete immutable source plan; each source line is
consumed once across approval changes and allows attempt/generation 1 only.

Custody support alone does not satisfy AML readiness. Workers check this
contract before new signing or broadcast when the network has enabled AML
scopes. Deploy compatible plugins before activating those scopes, and run the
[read-only AML configuration preflight](../../docs/operations/crypto-aml-preflight.md).
No signing or enrollment is performed by that check.
