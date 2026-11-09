# AML: проверка совместимости перед переключением

`crypto-preflight -mode aml` проверяет конфигурацию AML в одной tenant-базе и совместимость установленных Vault signer-плагинов. Команда выполняет SQL SELECT в read-only repeatable snapshot, затем GET readiness для control/family signer. Она не применяет миграции, не включает policy, не регистрирует owner, не запускает jobs, не подписывает транзакции и не вызывает blockchain/AML-провайдера. `-apply` в этом режиме запрещён.

## Контракт signer

Оба family plugins публикуют в существующем `config/readiness` три признака бинарного контракта:

```json
{
  "aml_signing_schema": "aml_signing.v1",
  "aml_intent_schema": "money_movement_intent.v2",
  "aml_source_line_basis": "CryptoAMLWithdrawalLine"
}
```

`aml_signing.v1` означает проверку signed intent V2, explicit high/unknown acceptance, уникальных source lines и их общей суммы, точного membership source/key/amount и owner. Каждая line потребляется один раз независимо от изменения approval; для неё допустимы только attempt/generation 1. Это версия поведения бинарника, а не настройка, которую можно включить у старого плагина.

При наличии любого enabled AML scope в сети worker требует этот контракт дополнительно к обычному signer readiness и независимому merchant custody registry. Старая версия, неполный ответ или недоступный readiness запрещают новую подпись и повторный broadcast. Уже известный final chain outcome остаётся доступным для receipt recovery и учёта без новой отправки. До включения AML scopes прежний custody-контракт сохраняет совместимость.

Сначала обновляются оба signer-плагина, затем worker/API; network policy при подготовке остаётся выключенной. Наличие только `merchant_custody_supported=true` не доказывает поддержку AML source lines. Новые withdrawal intents нельзя направлять в плагин, который ещё не публикует AML-контракт, даже если он уже содержит часть V2-реализации.

## Запуск

Используются обычные параметры crypto-preflight: tenant `DATABASE_URL`/`DATABASE_URL_FILE`, `TENANT_ID`, tokenless `VAULT_ADDR`, `CRYPTO_AUTH_SIGNER=transit` и transit mount. Нужны права чтения readiness family mounts и метаданных control signer; права derive/sign/enroll для этого режима не нужны. Режим не создаёт полный CryptoProcessor: approval verification keys и `CRYPTO_CHAIN_CONFIG_FILE` ему не требуются. Для production control signer читаются только Transit metadata; private key не извлекается. Production-ограничения на статический Vault token и local private key сохраняются и в диагностическом сервисе.

```sh
go run ./cmd/crypto-preflight -mode aml
```

Без `CRYPTO_PREFLIGHT_NETWORK_CODE` проверяются все сети с enabled AML scopes. Для отдельного этапа можно указать `CRYPTO_PREFLIGHT_NETWORK_CODE=TRON-NILE` или `ETHEREUM-SEPOLIA`. Выбранная сеть без AML scopes возвращает блокирующую причину. При несовместимой схеме команда останавливает проверку до чтения новых AML relations.

Ответ — JSON с `observedAt`, `databaseSchema`, `expectedSchema`, `namespaceEnrolled`, `ready`, общими `blockers` и массивом `networks`. Внутренние URL Vault, credentials и текст transport errors в отчёт не входят. Exit code равен 0 только при `ready=true`.

`dispatchEnabled=false` допустим для готовой конфигурации: preflight используется до включения network policy. Сам результат не является разрешением на отправку и не имеет срока действия dispatch permit.

Поле `emulatedScopes` показывает scopes отдельного testnet-провайдера. Их наличие не подтверждает реальный vendor contract для mainnet.

## Блокирующие причины

| Код | Что требуется |
|---|---|
| `schema_incompatible` | Привести tenant schema к версии текущего release; preflight ничего не мигрирует |
| `tenant_namespace_invalid` / `vault_namespace_not_enrolled` | Проверить tenant UUID и trusted enrollment именно этой базы в namespace |
| `no_aml_networks_found` / `aml_scopes_not_enabled` | Указать подготовленную сеть и явно настроить её scopes |
| `network_inactive` / `network_hold_open` | Разобрать отключение или действующий network hold |
| `custody_not_enforced` | Завершить merchant custody enrollment и включение независимого owner registry |
| `native_asset_missing` / `native_scopes_missing` | Подготовить native asset и native AML scope для каждого участвующего мерчанта; gas не покрывается principal |
| `legacy_principal_inventory` | Разобрать ненулевые shared/unowned principal wallets затронутых активов |
| `legacy_withdrawals_pending` | Классифицировать незавершённые classless заявки затронутых merchant/token scopes |
| `automatic_wallet_baselines_missing` | Дождаться независимых baselines автоматических адресов; проверяются и адреса, для которых job ещё не создана |
| `baseline_pending` / `baseline_needs_attention` | Дождаться финального checkpoint либо разобрать evidence/hold; не удалять историю для получения зелёного отчёта |
| `signer_not_configured` / `family_signer_unavailable` | Настроить control signer и family mount в этом tenant |
| `authorization_signer_not_ready` / `family_signer_not_ready` | Проверить keyring, public keys, seed и network policy signer-плагина |
| `custody_signer_unavailable` / `custody_signer_not_ready` | Обновить/настроить независимый owner registry family signer |
| `aml_signer_contract_unavailable` / `aml_signer_contract_incompatible` | Установить signer с точным AML-контрактом и повторить проверку |

## После preflight

Этот режим проверяет конфигурацию и версии, а не весь go/no-go. Далее нужны [ingestion preflight](rpc-ingestion.md), фактическая сверка source inventory, собственный operating gas budget, принятые vendor fixtures, backup/restore и Nile/Sepolia rehearsal. Каждый authorize/sign/broadcast заново проходит [durable AML gate](crypto-aml-outbound.md), независимо от сохранённого отчёта.

Реальные binary digests, deployment, публичные testnet transactions и production activation проверяются оператором отдельно. Локальные тесты используют synthetic Vault/RPC/AML responses и одноразовые PostgreSQL базы.


## Локальная проверка реализации

- Полный `go run ./cmd/integration-test` с `-race` на создаваемых командой disposable базах: успешно, 110 пакетов с тестами.
- Оба отдельных Vault plugin modules: `go test ./... -count=1` успешно; readiness публикует точный AML-контракт.
- Сборка, golangci-lint и 428 HTTP contracts: успешно. Сам read-only preflight не менял schema 73 и Merchant/Portal HTTP-контракт; testnet emulator добавлен отдельно в migration 00074.
- Проверены custody-only plugin до/после AML enablement, неподдерживаемые schemas, недоступный readiness, отсутствие SQL transaction при Vault reads, отсутствие signing/jobs и сокрытие transport error details.
- SQL snapshot проверяет namespace/network filter, остановку на старой schema и автоматический адрес без baseline ещё до появления job.
- CLI smoke без Vault/RPC/approval keys возвращает JSON с `ready=false` и конкретными blockers на неподготовленной базе; `-apply` отклоняется до DB connect. Несуществующие approval-key/RPC config files этот режим не читает.


Readiness snapshot проверяет наличие начального baseline у автоматических адресов. Коллектор с ненулевым текущим inventory не становится «unbaselined»: его текущую spendability проверяют risk, inventory, allocation и dispatch gates. При конкурентном изменении EVM cursor во время startup RPC сверка возвращает retryable ошибку; такой снимок не должен создавать nonce divergence hold. Не запускать старую диагностическую сборку параллельно новому воркеру.
