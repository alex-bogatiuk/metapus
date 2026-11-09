# Автопополняемый пул криптокошельков

В сетях с enforced merchant custody shared warm-pool refills остановлены. Для новых owned адресов используется отдельный [trusted merchant provisioning](crypto-aml-batches.md); для [автоматических merchant watermarks](crypto-aml-merchant-pools.md) используются отдельные конечные политики и control-plane consumer.

## Назначение

`WalletPoolController` поддерживает заранее подготовленный запас
`active + free` transient pool wallets. Создание инвойса выполняет только
атомарную аренду готового адреса и не обращается к Vault или blockchain RPC.

Если готового адреса нет, API возвращает `503 WALLET_POOL_EXHAUSTED` с
`Retry-After`. Повтор с тем же `orderId` или idempotency key безопасен:
неуспешная попытка не создаёт инвойс и не фиксирует lease.

## Жизненный цикл

1. Controller запускается при старте worker и затем примерно каждые 15 секунд
   с jitter.
2. При `ready <= lowWatermark` транзакционный planner блокирует policy-row,
   считает wallets и незавершённые jobs и создаёт только текущий deficit.
3. В той же транзакции planner резервирует монотонные HD-index в
   `sys_crypto_wallet_derivation_cursors`. Каждый path сначала публикуется в
   append-only `sys_crypto_wallet_derivations` и после этого не
   переиспользуется.
4. Provisioning worker забирает jobs через `FOR UPDATE SKIP LOCKED`, повторно
   проверяет capability runtime driver и вызывает идемпотентный Vault derive
   endpoint. Режим `derived` создаёт wallet с chain-статусом `active`, режим
   `derived_with_activation` — `unknown`; оба получают hold
   `chain_balance_check_pending`.
5. Targeted reconciliation сравнивает chain balance с ledger и снимает hold
   только после успешного чтения всех токенов сети.
6. Только для `derived_with_activation` Activation executor резервирует бюджет
   и hot-wallet balance в tenant DB до создания и подписи транзакции. После
   accepted/already-known broadcast создаётся `CryptoNetworkOperation`; после
   finality документ проводит фактическую комиссию и завершает резерв.
7. Wallet становится allocatable только в состоянии `active + free` без hold.

`reg_crypto_account_activations` хранит только queue/retry/lease и recovery
state со ссылкой `network_operation_id`. TX, settlement и фактическая стоимость
принадлежат документу. Если address уже активен до подписи или broadcast
отклонён сетью, документ не создаётся.

Jobs `pending`, `processing`, `retry_wait`, `held`, неактивированные wallets и
wallets на первичном `chain_balance_check_pending` учитываются как `inFlight`.
Recoverable activation hold
не должен порождать новые derivation paths. Это ограничивает генерацию при
недоступности Vault или TRON. После пяти последовательных ошибок circuit
breaker открывается минимум на 30 секунд; jobs используют bounded exponential
backoff с jitter. Claim каждой provisioning job имеет fencing token: worker с
истёкшим lease не может материализовать адрес после повторного claim другим
worker.

## Инварианты policy

- `0 <= lowWatermark < targetReady <= maxTotalWallets`;
- `maxProvisionBatch` и `retryAfterSeconds` положительны;
- family adapter объявляет provisioning mode; `activation`-секция допустима
  только для `derived_with_activation`, и тогда `activation.maxConcurrent`
  положителен;
- policy-суммы хранятся как `NUMERIC(78,0)` raw units, в Go представлены
  `CryptoAmount`, а в JSON передаются каноническими десятичными строками;
- обновление требует актуальный `version`;
- policy с `needsReview=true` нельзя включить;
- `(network_id, address)` и `(network_id, derivation_path)` уникальны;
- `addressReuseCooldownSeconds >= 0`; если значение > 0, `maxWatcherLagSeconds > 0`;
- для TRON при включённом cooldown `maxWatcherLagSeconds >= 180`;
- готовый адрес — `free + active` без hold, без открытого recon-инцидента и с `cooldown_until IS NULL OR cooldown_until < NOW()`;
- `lease_blocked_reason='watcher_lag'` останавливает только выдачу; refill при блоке не создаёт новые TRON-адреса; sweep/withdrawal продолжаются.
- опубликованный derivation path сохраняется в append-only registry даже для
  inactive/soft-deleted wallets и больше не выдаётся Vault повторно;
- наличие activation reserve определяется `activation_cost_reserved_at`, а не
  положительной суммой: policy с нулевой стоимостью не обходит concurrency
  gate.

Изменение policy требует permission `crypto_wallet_pool:update` и MFA step-up.
Агрегированное состояние доступно с `crypto_wallet_pool:read`; API никогда не
возвращает адреса.

## Capacity planning

Минимальный target:

```text
targetReady >= peak invoice rate × P99 refill lead time + safety margin
```

Refill lead time включает Vault derive, targeted reconciliation, попадание
activation transaction в блок и требуемые подтверждения, если family требует
activation. Автогенерация не
заменяет capacity planning: burst, который исчерпал `targetReady` быстрее
blockchain-активации, корректно получит `503`.

## Nile preset

Публичный bootstrap задаёт:

- `low=2`, `targetReady=5`, `maxTotal=20`;
- `maxProvisionBatch=5`, `activation.maxConcurrent=1`;
- activation reserve `2_000_000` sun;
- минимальный hot reserve `2_000_000` sun;
- дневной UTC-бюджет `10_000_000` sun;
- `Retry-After=30`;
- `addressReuseCooldownSeconds=0`, `maxWatcherLagSeconds=0` (карантин выключен, пока оператор не PATCH).

Перед включением карантина пересчитайте ёмкость: occupancy ≈ TTL + cooldown + sweep. При Nile `targetReady=5` / `maxTotal=20` и cooldown 1800 с без ресайза пул даст `503 WALLET_POOL_EXHAUSTED`.

Рекомендуемые значения PATCH (не migration DEFAULT):

| Сеть | cooldown, с | max watcher lag, с |
|------|-------------|--------------------|
| Nile (TRON) | 1800 | 900 |
| Sepolia (EVM) | 1200 | 600 |

Bootstrap оставляет policy выключенной, пока отсутствует подтверждённый
native-funding receipt. On-chain balance сам по себе не считается ledger
funding. Оператор сначала регистрирует подтверждённую транзакцию:

```bash
./crypto-record-wallet-funding \
  -wallet-code HOT-TRON-001 \
  -token-code TRX-NILE \
  -tx-hash <confirmed-native-trx-hash>
```

После этого bootstrap или MFA-защищённый Admin UI может включить policy.
Mainnet и другие сети остаются выключенными до отдельного подтверждения.

Worker ACL должен иметь только tenant-scoped `update` к
`tron/tenants/<tenant>/wallets/derive`. После bootstrap ACL настраивается
отдельной операторской командой:

```bash
./public-test-bootstrap vault-worker-policy \
  --state-dir /state \
  --tenant-id <tenant-uuid> \
  --vault-addr https://vault-test:8200
```

Повторный `configure-db` сохраняет status, lease и activation state кошелька,
если network/address/path/Vault key identity не изменилась. Новая identity
допускается только для кошелька без финансовой истории и снова получает
`chain_balance_check_pending`.

## Наблюдаемость

В `sys_worker_jobs` записываются:

- `crypto.wallet_pool.reconcile`;
- `crypto.wallet_pool.provision`;
- `crypto.wallet_pool.activation`.

Event log дедуплицирует переходы `healthy`, `low`, `depleted`, `paused`.
Ошибки Vault, TRON и budget gate записываются только при изменении стабильного
machine code. Admin API получает безопасное operational message; сырой текст
Vault/TRON сохраняется только в worker logs и внутренних provisioning jobs.

Диагностика CNO, settlement и activation movements описана в
[crypto-environment-setup.md](../crypto/crypto-environment-setup.md#13-дождаться-активации-pool).
