# TRON v1 — Nile acceptance protocol

> **Статус по умолчанию:** `NOT RUN`. Документ является исполняемым протоколом
> приёмки новой schema `76`; исторические прогоны более ранних схем его не
> закрывают.
>
> **Результат:** `NILE ACCEPTED` или `NILE REJECTED`. Успешная приёмка означает
> только Mainnet-ready candidate и не разрешает Mainnet canary.

## 1. Неизменяемая идентификация прогона

До запуска сохранить в acceptance report:

```text
git commit:
working tree dirty: yes/no + приложенный patch digest
backend image digest:
worker image digest:
frontend image digest:
TRON Vault plugin SHA-256:
tenant ID:
schema version: 76
chain config digest:
policy version before/after:
started/finished at UTC:
operators:
```

В report запрещены API keys, адреса с привязкой к tenant, signed/unsigned
payload, authorization JWS, Vault paths и recovery material.

## 2. Автоматические gates

Все команды должны завершиться с exit code `0` на том же commit:

```powershell
go test ./...

Push-Location vault-sign-plugins/tron
go test ./...
Pop-Location

Push-Location frontend
npm run typecheck
npm run lint
npm run build
Pop-Location

docker compose -f deployments/single-tenant/docker-compose.yml config --no-interpolate --quiet
docker compose -f single-tenant-deployment/docker-compose.yml config --no-interpolate --quiet
docker compose -f single-tenant-deployment/docker-compose.yml -f single-tenant-deployment/docker-compose.public-test.yml config --no-interpolate --quiet
```

На одноразовом PostgreSQL выполнить fresh migration, полный `Down`, повторный `Up` и
конкурентные crypto tests:

```powershell
$env:METAPUS_TEST_MIGRATION_DSN = '<disposable-postgres-dsn>'
go test -tags=integration ./internal/infrastructure/storage/postgres/migration -run TestRunAllFreshCryptoLifecycleSchema -count=1 -v

$env:METAPUS_TEST_CRYPTO_DSN = $env:METAPUS_TEST_MIGRATION_DSN
go test -tags=integration ./internal/infrastructure/crypto_worker -count=1 -v
```

## 3. Preflight

- [ ] `CRYPTO_PROCESSING_ENABLED=true`; legacy `TRON_RPC_URL`/`TRON_API_KEY`
  отсутствуют в runtime environment.
- [ ] `CRYPTO_CHAIN_CONFIG_FILE` доступен worker и содержит минимум по одному
  endpoint каждой роли: `indexer`, `fullnode`, `solidity`.
- [ ] API keys читаются через `apiKeyFile`; значений ключей нет в JSON/env.
- [ ] Каталог содержит `TRON-NILE` с `chain_family=tron` и
  `network_identifier=nile`.
- [ ] Единственный активный `network_fee` wallet не участвует в invoice или
  hot-wallet allocation.
- [ ] Policy изначально выключена; budgets равны нулю до явной настройки.
- [ ] Включённая policy проходит внутренние budget-инварианты: JIT amount
  покрывает transaction cap и residual, а daily JIT budget больше одного
  funding principal и оставляет положительную ёмкость на фактическую комиссию.
- [ ] Vault family plugin, authorization Transit и tokenless proxy проходят
  readiness до первого iTRX заказа.
- [ ] Admin UI показывает свежий snapshot всех RPC roles, signer, iTRX,
  fee-wallet и policy version.

Enable выполняется только через Admin API/UI со step-up MFA и фиксируется
событием `crypto.network.policy_updated`.

## 4. Обязательная Nile-матрица

| ID | Сценарий | Проверяемый инвариант | Pass evidence |
|---|---|---|---|
| N-01 | iTRX доступен | Предпочтена аренда; fee reservation и provider cost созданы один раз | resource allocation/order, reservation events, receipt |
| N-02 | iTRX timeout/429/circuit-open | Только подтверждённый transient failure разрешает bounded burn | decision metric + `native_burn`; расход не превышает policy |
| N-03 | iTRX order status неизвестен | Нет burn и новой транзакции; attempt переходит в held | один provider order, ноль broadcast, incident |
| N-04 | iTRX 401/403 или invalid response | Fail closed без burn | held reason taxonomy, неизменившийся burn budget |
| N-05 | Pool wallet без TRX | Уникальный fee-wallet → pool JIT funding solidified до sweep | один funding, две balance movements, один network fee |
| N-06 | Signer/Vault недоступен | Отказ возникает до resource order и fee reservation | trace order, ноль новых iTRX orders |
| N-07 | Indexer primary down | Discovery продолжен через secondary, остальные сети/loops живы | RPC metrics и найденный payment |
| N-08 | FullNode primary down | Build/broadcast выполнены через secondary | provider IDs в безопасной telemetry |
| N-09 | Solidity primary lagging/down | Endpoint исключён; settlement ждёт healthy Solidity receipt | lagging state, отсутствие преждевременной проводки |
| N-10 | Ответ accepted broadcast потерян | Сначала lookup, затем byte-identical envelope на secondary; re-sign отсутствует | один authorization, один signed envelope, same fingerprint |
| N-11 | Duplicate/already-known | Повтор считается accepted без новой подписи | immutable attempt и один tx hash |
| N-12 | Per-tx/daily budget exhausted | Новая подпись и broadcast отсутствуют; reservation не превышает budget | reservation events + aggregate SQL |
| N-13 | Minimum residual нарушен | Attempt held; source/fee wallet не расходуется | balance snapshot + held reason |
| N-14 | Provisional reorg inbound | Payment `reorged`; financial movements отсутствуют | event block hash/index + zero settlement movements |
| N-15 | Solidified success/revert | Settlement только по `/walletsolidity/*` receipt | raw receipt + normalized settlement + one fee cost |
| N-16 | Reconciliation | Pool/hot/fee wallets, reservations, fundings и unknown outflows охвачены | completed run, объяснённые incidents |

Для каждого outbound-сценария дополнительно проверить:

```sql
-- Не более одной authorization и одного persisted signed envelope на attempt.
SELECT basis_type,basis_id,attempt,COUNT(*),COUNT(signed_envelope)
FROM reg_crypto_signing_authorizations
GROUP BY basis_type,basis_id,attempt
HAVING COUNT(*) > 1 OR COUNT(signed_envelope) > 1;

-- Финансовый network fee не продублирован telemetry-стратегией native_burn.
SELECT recorder_id,cost_type,COUNT(*)
FROM reg_crypto_platform_cost_movements
WHERE cost_type='chain_network_fee'
GROUP BY recorder_id,cost_type
HAVING COUNT(*) > 1;
```

Оба запроса должны вернуть `0 rows` для области прогона.

## 5. Crash/restart matrix

Для withdrawal, sweep и JIT funding остановить worker в каждой точке и после
restart довести attempt до детерминированного terminal/held состояния:

- [ ] до и после fee reservation;
- [ ] до и после authorization persistence;
- [ ] после family signing, но до persistence signed envelope;
- [ ] после persistence signed envelope, но до broadcast;
- [ ] после принятого broadcast с потерянным response;
- [ ] после funding solidification, но до principal movements;
- [ ] после receipt persistence, но до business settlement.

Pass: нет второй authorization, новой подписи, второго principal movement или
второго `chain_network_fee`; active reservation либо settled/released, либо
явно held с incident.

## 6. Observability acceptance

- [ ] Trace содержит `resource → fee.reserve → signing.authorize →
  signing.transaction → broadcast → finality → fee.settle`.
- [ ] Dashboard показывает RPC request/duration/circuit, signer readiness,
  resource decisions, burn/funding, budget remaining, ambiguous broadcast и
  settlement lag.
- [ ] Alert test доставлен для signer down, all-provider RPC down, ambiguous
  broadcast, held reservation, budget exhaustion и reconciliation incident.
- [ ] Проверка telemetry export подтверждает отсутствие запрещённых attributes.

## 7. Решение

`NILE ACCEPTED` разрешено только при закрытых N-01…N-16, crash matrix,
нулевых необъяснённых ledger differences и приложенных Security/Finance/QA
подписях. Иначе результат — `NILE REJECTED` с incident IDs и владельцами
исправлений.

Следующий отдельный gate:
[crypto-mainnet-go-no-go.md](crypto-mainnet-go-no-go.md).
