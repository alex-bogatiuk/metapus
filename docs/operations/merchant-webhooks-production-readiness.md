# Merchant webhooks — production readiness и canary

> **Решение по умолчанию:** `NO-GO`.
>
> Этот runbook закрывает только merchant invoice API, lifecycle, webhook
> delivery и reconciliation. Он не заменяет общий chain/Vault/treasury gate.
> `GO` возможен только после одинакового conformance-прогона на TRON Nile и
> Ethereum Sepolia и двух независимых low-value Mainnet canary.

## 1. Идентичность кандидата

В отчёте зафиксировать commit, patch digest для dirty tree, image digests,
schema version `76`, OpenAPI `2026-08-26`, chain-config digest, policy versions,
UTC начала/окончания и операторов. API keys, webhook secrets, Vault refs и raw
customer metadata в отчёт не включать.

## 2. Автоматические gates

Все команды выполняются на том же commit и завершаются кодом `0`:

```powershell
go test ./...
go test -race ./internal/domain/crypto ./internal/infrastructure/crypto_worker

Push-Location frontend
npm run typecheck
npm run lint
npm run build
Pop-Location

Push-Location sdk/typescript
npm test
Pop-Location

Push-Location sdk/python
python -m unittest discover -s tests -v
Pop-Location

Push-Location sdk/go
go test ./...
Pop-Location
```

На disposable PostgreSQL обязательны fresh up, полный down/up и integration
tests атомарности:

```powershell
$env:METAPUS_TEST_MIGRATION_DSN = '<disposable-postgres-dsn>'
go test -tags=integration ./internal/infrastructure/storage/postgres/migration -count=1 -v

$env:METAPUS_TEST_DATABASE_DSN = $env:METAPUS_TEST_MIGRATION_DSN
go test -tags=integration ./internal/infrastructure/storage/postgres/crypto_repo -count=1 -v
```

Обязательная evidence из CI:

- partial `40 + 60`, exact, overpaid, underpaid, late payment, duplicate chain
  event и multiple payments;
- reorg до и после finality, immutable reversal и повторная канонизация;
- rollback merchant event/outbox и causation dedup;
- десять attempts `0..72h`, jitter, `Retry-After`, новый delivery ID/timestamp/
  signature при неизменном event ID/body;
- dual-signature rotation, SSRF/private ranges, DNS rebinding, redirects,
  timeout, TLS, response limit;
- golden OpenAPI/fixtures и TypeScript/Go/Python SDK contract tests.

## 3. Testnet conformance: Nile и Sepolia

Матрицу ниже выполнить отдельно с `network=TRON-NILE` и
`network=ETHEREUM-SEPOLIA`. Один отчёт не может закрыть две сети.

| ID | Проверка | Pass evidence |
|---|---|---|
| W-01 | Invoice create с `Idempotency-Key`, `orderId`, metadata | один invoice ID; snapshot confirmations/finality совпадает с policy |
| W-02 | Два платежа `40 + 60` | finality только первого не даёт `invoice.confirmed`; второй доводит до `completed` |
| W-03 | Endpoint fan-out и invoice `notificationUrl` override | override заменяет fan-out и использует secret default endpoint |
| W-04 | Endpoint отключён/включён | новые события не теряются; после восстановления jobs доставляются |
| W-05 | Retry chaos: timeout, 429/503, 5xx | attempts соответствуют шкале, `Retry-After` ограничен deadline |
| W-06 | Secret rotation/delete | 24 часа две `v1`; уже созданные jobs продолжают использовать snapshot secret; Vault ref удаляется janitor-ом не ранее чем через 73 часа; secret повторно не раскрывается |
| W-07 | Reorg | provisional projection откатывается; post-finality создаёт reversal, incident и network hold |
| W-08 | Reconciliation | внесённый projection drift исправлен lifecycle service не позднее двух циклов |
| W-09 | Debugger/redelivery | body immutable, response excerpt очищен и ≤8 KiB; manual job использует текущий endpoint |
| W-10 | Kill switch | новая invoice allocation и delivery остановлены; event/job не удалены и восстановлены после enable |

В Ethereum evidence обязательно содержит successful receipt, канонический
block hash, `transactionHash + logIndex`, sender и nonce. `payment.replaced`
принимается только вместе с доказанным same-sender/same-nonce replacement.
В TRON evidence содержит successful transaction result и solidified/finality
наблюдение по platform policy.

## 4. SLO и alerts

SLO: при здоровой инфраструктуре 99.9% first attempts стартуют не позднее 60
секунд после commit merchant event.

До canary dashboard и paging должны использовать:

- `crypto.webhook.event_to_dispatch_latency_seconds` — p99 и доля ≤60s;
- `crypto.webhook.delivery_attempts` — first-attempt success по event type;
- `crypto.webhook.queue_age_seconds` — warning >60s, critical >300s;
- `crypto.webhook.retry_age_seconds` и `crypto.webhook.terminal_failures`;
- `crypto.invoice.transition_lag_seconds`, chain lag и reorg count;
- `crypto.invoice_reconciliation.cycles` с `repairs`/`failures`.

Production worker не стартует без `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT` или
общего `OTEL_EXPORTER_OTLP_ENDPOINT`. TLS/headers/compression задаются
стандартными `OTEL_EXPORTER_OTLP_*` переменными; рекомендуемый
`OTEL_METRIC_EXPORT_INTERVAL=15000`.

Открытый `invoice_projection_drift` после двух циклов и любой
`posted_payment_reorg` — P1 независимо от агрегированных метрик. Проверить
реальный paging route и приложить test alert acknowledgement.

## 5. Mainnet low-value canary

TRON Mainnet и Ethereum Mainnet проходят отдельные change windows, используют
не менее двух административно независимых RPC providers и разные лимиты. Для
каждой сети заполнить:

```text
network:
candidate/image digest:
policy version:
RPC provider identities:
max invoice value / aggregate value / count:
start/end UTC:
invoice IDs, event IDs and tx hashes (без secrets/metadata):
first-attempt SLO result:
reconciliation result:
kill-switch drill result:
Engineering/SRE/Security/Finance/Risk approvals:
decision: GO / NO-GO
```

Canary содержит exact, partial, overpayment, intentional endpoint 5xx с
последующим recovery, manual redelivery и network kill-switch drill. При drift,
необъяснимом ledger delta, finality ambiguity, terminal delivery failure или
нарушении SLO: отключить новые invoice allocations, сохранить events/jobs,
открыть incident и принять `NO-GO`.

## 6. Launch gate

Launch остаётся `NO-GO`, если отсутствует хотя бы одно evidence:

- Nile и Sepolia conformance;
- Vault KV secret create/rotate/restore drill;
- endpoint delete/rotation с успешным завершением уже созданных retry jobs и
  последующей идемпотентной очисткой retired secret из Vault;
- schema 76 fresh/down/up;
- 72-hour fake-clock suite и debugger retention;
- SDK/OpenAPI golden tests;
- отдельные TRON Mainnet и Ethereum Mainnet low-value canary;
- подтверждённые dashboard, paging и network kill switch.

`returnUrl` никогда не участвует в решении. Мерчант исполняет заказ только по
идемпотентно обработанному `invoice.completed` и при сомнении читает
канонический `GET /merchant/v1/invoices/{id}`.
