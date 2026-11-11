# Криптопроцессинг: pre-production acceptance protocol

> **TL;DR:** Единый release-blocking протокол для проверки приёма, ledger,
> custody, исходящих операций, webhooks и восстановления. Продакшна пока нет:
> документ не содержит бессрочных `passed` и по умолчанию имеет вердикт
> `NO-GO`, пока конкретный release candidate не приложил все обязательные
> evidence.
>
> **Тип:** Rule
>
> **Аудитория:** QA, Developer, SRE, Security, Finance, Risk
>
> **Связанные:** [crypto-processing-architecture.md](crypto-processing-architecture.md),
> [crypto-security-and-risk-review.md](crypto-security-and-risk-review.md),
> [merchant-webhooks-production-readiness.md](../operations/merchant-webhooks-production-readiness.md),
> [crypto-mainnet-go-no-go.md](../operations/crypto-mainnet-go-no-go.md)

---

## 1. Статус и границы

Этот файл проверяет код и disposable pre-production окружения на schema `76`.
Он не подтверждает production readiness сам по себе и не разрешает Mainnet.

Правила вердикта:

| Вердикт | Значение |
|---|---|
| `NOT_RUN` | Проверка ещё не выполнялась для указанного candidate. Значение по умолчанию. |
| `PASS` | Есть воспроизводимое evidence именно для указанного commit/image/schema. |
| `FAIL` | Полученный результат нарушает хотя бы один ожидаемый инвариант. |
| `BLOCKED` | Проверку нельзя выполнить из-за отсутствующего control, окружения или внешней зависимости. |
| `WAIVED` | Допустимо только для non-release проверки. Для финансового, custody или security gate эквивалентно `FAIL`. |

Исторические tx hash, invoice ID, UUID tenant и результаты старых schema не
переносятся на новый candidate. Их хранят в отдельном test report, но не в
нормативном сценарии.

### 1.1 Release identity

До запуска заполнить:

```text
candidate commit:
dirty patch digest (если tree не clean):
server image digest:
worker image digest:
frontend image digest:
schema version: 76
OpenAPI version/digest:
chain config digest:
network policy versions:
Vault plugin digests:
TRON environment: Nile
EVM environment: Sepolia
started/finished UTC:
operators:
```

Секреты, API keys, webhook secrets, Vault tokens, raw customer metadata и
seed material в evidence не попадают.

## 2. Непереговорные инварианты

Проверка прекращается с `FAIL` при нарушении любого правила:

1. Денежные и blockchain-значения передаются как canonical decimal strings.
   JSON number для `CryptoAmount` запрещён.
2. Мутирующая операция использует стандартный `Idempotency-Key`.
   `X-Idempotency-Key` не является частью контракта.
3. Один chain event создаёт не более одного payment/outflow economic effect.
4. `receivedAmount` — канонически наблюдаемая сумма, `confirmedAmount` —
   chain-final сумма, `creditedAmount` — сумма, проведённая в merchant ledger.
5. Заказ исполняется только после `invoice.completed` или подтверждения тем же
   состоянием через canonical `GET /merchant/v1/invoices/{id}`.
6. `overpaid` — условие `creditedAmount > amount`, а не статус инвойса.
   Partial expiry имеет статус `underpaid`; неоплаченный expiry — `expired`.
7. Финансовый регистр append-only. Исправление — reversal/compensating movement,
   а не `UPDATE` исторического движения.
8. Managed custody address не может быть withdrawal/treasury destination.
   Внутреннее перемещение оформляется отдельным типом операции.
9. Неизвестный исходящий transfer атомарно создаёт outflow, reconciliation
   incident, immutable observation и fail-closed hold.
10. Внешний вызов RPC, provider или webhook не выполняется внутри финансовой
    PostgreSQL-транзакции.
11. Sign/broadcast разрешён только по immutable intent, policy digest,
    approval bundle и signing authorization.
12. Ошибка fee/config/provider/readiness не создаёт частичный финансовый эффект.
13. Tenant определяется доверенным контекстом. Merchant ID из URL/query/body
    всегда повторно проверяется по merchant scope.

## 3. Обязательные автоматические gates

Команды выполняются на candidate из release identity:

```powershell
go build ./...
go test -short -race -count=1 ./...

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

На отдельной disposable PostgreSQL:

```powershell
$env:METAPUS_TEST_MIGRATION_DSN = '<disposable-postgres-dsn>'
$env:METAPUS_TEST_CRYPTO_DSN = $env:METAPUS_TEST_MIGRATION_DSN

go test -tags=integration ./internal/infrastructure/storage/postgres/migration -count=1 -v
go test -tags=integration ./internal/infrastructure/storage/postgres/crypto_repo -count=1 -v
go test -tags=integration ./internal/infrastructure/storage/postgres/portal_repo -count=1 -v
go test -tags=integration ./internal/infrastructure/crypto_worker -count=1 -v
go test -tags=integration ./internal/infrastructure/blockchain/evm -count=1 -v
```

Migration gate включает fresh `Up`, полный `Down → Up` и проверку итоговой
schema `76`. Test database после прогона уничтожается.

> [!IMPORTANT]
> Committed Playwright UI-contract specs уже проверяют production HTML публичного
> checkout, stable-key invoice/withdrawal retries, Owner/Manager/Viewer webhook
> registry, external-outflow workflow и staged merchant CIDR. До release
> дополнительно обязательны full-stack
> сценарии checkout, portal withdrawal, webhook registry и security-negative
> flows с disposable PostgreSQL/backend. Одни route-mocked UI contracts не
> переводят финансовый сценарий в `PASS`.

## 4. Test environment

Обязательная топология:

| Компонент | Требование |
|---|---|
| PostgreSQL | Новый disposable cluster, применены только текущие migrations |
| Tenant | Новый tenant, без данных прошлых прогонов |
| Vault | TLS, audit enabled, отдельные TRON/EVM mounts, worker без static root token |
| TRON | Nile; независимые fullnode, solidity и event/indexer roles |
| EVM | Sepolia; правильный chain ID, минимум два административно независимых RPC |
| Worker | Один экземпляр для базового прогона; два экземпляра для concurrency/restart |
| Webhook sink | TLS endpoint с режимами 2xx, 429, 503, timeout, redirect, large body |
| Clock | Управляемые часы для expiry, retry и 72-hour webhook schedule |
| Observability | Метрики, структурные логи, trace ID и tested alert route |

Test values задаются через seed/bootstrap, а не редактированием регистров после
старта сценария. Для TRON и EVM формируется отдельный отчёт: evidence одной сети
не закрывает другую.

## 5. Матрица покрытия

`Source state` описывает только наличие реализации/автотеста в текущем дереве.
Колонка не заменяет execution verdict конкретного candidate.

| ID | Риск | Основной автоматический evidence | Source state | Candidate |
|---|---|---|---|---|
| T01 | Pool activation/allocation | wallet pool и activation integration suites | реализован | `NOT_RUN` |
| T02 | API key isolation/scopes | merchant auth/API key tests | реализован | `NOT_RUN` |
| T03 | Invoice idempotency/exact amount | handler, SDK и idempotency tests | реализован | `NOT_RUN` |
| T04 | Public checkout | handler tests + production-HTML Playwright contract | реализован; full-stack E2E обязателен | `NOT_RUN` |
| T05 | Exact payment | `TestPaymentCycle_FullFlow` | реализован | `NOT_RUN` |
| T06 | Partial payment | payment cycle multi-payment tests | реализован | `NOT_RUN` |
| T07 | Overpayment | provisional/final overpayment tests | реализован | `NOT_RUN` |
| T08 | Dust | `TestPaymentCycle_DustRejection` | реализован | `NOT_RUN` |
| T09 | Expiry/late/reuse | allocation-window и late-payment tests | реализован | `NOT_RUN` |
| T10 | Persistent address/top-up | wallet allocation tests + testnet | реализован | `NOT_RUN` |
| T11 | Native watcher | watcher/driver tests + testnet | реализован | `NOT_RUN` |
| T12 | Sweep threshold | payment/sweep tests | реализован | `NOT_RUN` |
| T13 | Sweep retain | sweep reserve integration/acceptance | реализован | `NOT_RUN` |
| T14 | Resource rental | resource manager tests | реализован | `NOT_RUN` |
| T15 | Withdrawal allowlist/custody | custody-boundary integration test | реализован | `NOT_RUN` |
| T16 | Merchant withdrawal | approval/executor/settlement integration | реализован | `NOT_RUN` |
| T17 | Reject/pre-broadcast failure | withdrawal and approval tests | реализован | `NOT_RUN` |
| T18 | Failed on-chain withdrawal | atomic compensation integration | реализован | `NOT_RUN` |
| T19 | Treasury withdrawal | approval/fee reservation/testnet | реализован | `NOT_RUN` |
| T20 | Outbound network fee | outbound settlement integration | реализован | `NOT_RUN` |
| T21 | Balance reconciliation | reconciliation tests + injected drift | реализован | `NOT_RUN` |
| T22 | Unknown external outflow | exact-effect/outflow integration + workflow/UI tests | реализован | `NOT_RUN` |
| T23 | Provider reconciliation | resource/provider tests + `TestProviderOpening.*` + fake/real provider | opening защищён; verified manual funding/refund/adjustment и live acceptance не завершены | `NOT_RUN` |
| T24 | Webhook security/delivery | webhook unit/integration/SDK suites | реализован | `NOT_RUN` |
| T25 | Fee resolution failure | `TestPaymentCycle_FeeLookupFailureCreatesNoFinancialEffects` | реализован | `NOT_RUN` |
| T26 | Restart/idempotency | broadcast recovery and execution draft integration | реализован | `NOT_RUN` |
| T27 | Concurrency | targeted PostgreSQL integration matrix | реализован; scheduled `-count=100` обязателен | `NOT_RUN` |
| T28 | Security controls | auth/MFA/SSRF/Vault/CIDR unit и UI-contract tests | реализован; full-stack security-negative gate обязателен | `NOT_RUN` |
| T29 | Watcher refresh | processor/watcher refresh integration | реализован | `NOT_RUN` |
| T30 | Multiple token contracts | processor multicontract integration | реализован | `NOT_RUN` |

Source-level blocker T28 снят реализацией отдельных Portal/API/per-key CIDR
policy и system-admin recovery. Candidate остаётся `NOT_RUN`: общий release
verdict по-прежнему `NO-GO`, пока policy не пройдёт full-stack negative tests.

## 6. Приём платежей

### T01 — Pool activation и безопасная allocation

Предусловие: policy включена, pool ниже low watermark, activation budget задан.

1. Запустить два pool controller одновременно.
2. Дождаться derivation, funding/activation при необходимости и первой
   balance reconciliation.
3. Создать несколько transient invoices параллельно.

Ожидается: derivation path уникален; deficit не превышен; один wallet не выдан
двум invoice; до успешной activation/reconciliation действует hold; приватный
ключ не покидает Vault; activation cost и network fee отражены один раз.

### T02 — Merchant API key

1. Owner со свежим step-up создаёт key с минимальными scopes.
2. Проверить read/create, запрещённый scope, revoked/expired key и key другого
   tenant/merchant.
3. Убедиться, что plaintext показан один раз, а в БД и логах есть только hash и
   prefix.

Ожидается: tenant определяется по key index, BOLA/cross-tenant запросы дают
`401/403`, revoke действует немедленно, `lastUsedAt` не влияет на ответ.

### T03 — Idempotent invoice creation

Canonical request:

```http
POST /merchant/v1/invoices
X-Api-Key: <merchant-key>
Idempotency-Key: order-100045-create
Content-Type: application/json

{
  "amount": "11000000",
  "currency": "USDT-TRC20",
  "ttlMinutes": 60,
  "orderId": "order-100045"
}
```

Проверки:

- тот же key и byte-equivalent business payload возвращает тот же invoice;
- тот же key и другой payload возвращает `409`;
- новый key и тот же `orderId` не создаёт второй business invoice;
- отсутствующий `Idempotency-Key`, numeric amount, exponent, fraction, sign,
  whitespace и leading zero отклоняются;
- одновременно отправленные 20 одинаковых запросов создают одну invoice row,
  одну allocation и один initial event.

### T04 — Public checkout

Открыть opaque `payUrl` без tenant/user identifiers. Проверить amount, token,
network, address, expiry, QR, refresh и terminal state. Изменение `returnUrl`
или browser navigation не меняет финансовый статус. Enumeration случайных
checkout refs не раскрывает существование tenant/invoice.

`frontend/tests/public-checkout.spec.ts` исполняет JavaScript из фактического
embedded `payment.html` и доказывает opaque-ref/refresh/terminal/not-found UI
contract. Для `PASS` всё равно нужен отдельный full-stack прогон через реальный
backend и disposable PostgreSQL: route interception не доказывает storage,
rate-limit и HTTP middleware boundary.

### T05 — Точный входящий платёж

Отправить ровно invoice amount. Зафиксировать detection, confirmations и
finality. Ожидаемый внешний lifecycle:

```text
created → paid → confirmed → completed
```

Переходы `confirmed → completed` могут произойти в одной транзакции; мерчант
всегда ориентируется на terminal `completed`. Payment проходит
`detected/confirming → confirmed → settled`. Posting, fee movement, merchant
credit, event/outbox и wallet transition атомарны.

### T06 — Частичная оплата

Отправить `40%`, затем `60%` двумя tx. После первого платежа invoice имеет
`partially_paid`; финализация первого tx не даёт `completed`. После finality и
posting второго tx суммы складываются без потери и invoice становится
`completed`. Дубликат любого chain event не меняет balances.

### T07 — Переплата

Отправить сумму больше invoice amount одним или несколькими tx. До finality
invoice может быть `paid`; после posting — `completed`. Условие переплаты:
`creditedAmount > amount`. Вся фактически полученная сумма учитывается; excess
не исчезает и не создаёт отдельный несуществующий статус `overpaid`.

### T08 — Dust

Отправить значение ниже configured dust threshold. Ожидается audit/telemetry
с явной причиной, отсутствие merchant credit, fee, sweep и fulfillment event.
Повтор события остаётся идемпотентным.

### T09 — Expiry, late payment и reuse boundary

Проверить три случая:

- без оплаты: `created → expired`;
- частичная оплата: `partially_paid → underpaid`;
- transfer после закрытия: запись inbound inbox, match по immutable allocation
  window, credit исходному invoice и `invoice.late_payment` без reopen.

Затем выдать адрес повторно после cooldown. Transfer на границе старого окна
относится к старому allocation; transfer в gap имеет `needs_review`; новый
invoice не получает чужую оплату.

### T10 — Persistent address и top-up

Два одинаковых concurrent create по `merchant + network + customerId`
возвращают один wallet. Повторные top-up создают отдельные payments и postings,
но не новый address. Другой merchant/customer не видит привязку.

### T11 — Native watcher

Отправить native asset на managed wallet и проверить отдельный watcher key,
token resolution по пустому contract и отсутствие ложного USDT payment.
Управляемое внешнее funding допускается только через подтверждённый funding
workflow; произвольный outgoing native transfer попадает в T22.

## 7. Свипы и ресурсы

### T12 — Sweep threshold

Ниже threshold sweep не создаётся. При достижении threshold или max age один
worker захватывает wallet через lease/`SKIP LOCKED`, создаёт execution document,
immutable intent и signing authorization. Destination совпадает с Vault-derived
allowlist, а повторный worker не создаёт второй tx.

### T13 — Sweep retain

Для reusable deposit wallet выполнить sweep с `sweepRetainAmount > 0`.
Ожидается `sweepAmount = chainBalance - retain`; повторный sweep не расходует
reserve; значения выше `int64` остаются точными. Live acceptance выполняется
отдельно на Nile и Sepolia для поддерживаемой family semantics.

### T14 — External resource rental

Проверить successful rental, повторное использование, transient failover,
provider timeout/ambiguous order и refund. Ambiguous status fail-closed: система
сначала восстанавливает order по provider identity и не заказывает второй
resource. Rental cost, allocation и provider balance изменяются один раз.

## 8. Merchant withdrawal и treasury

### T15 — Address allowlist и custody boundary

1. Owner со свежим step-up добавляет внешний адрес.
2. Повторяет запрос для managed pool/hot/fee/treasury wallet той же сети.
3. Для EVM повторяет managed address с другим регистром.
4. После approval подменяет состояние так, чтобы destination стал managed, и
   запускает executor.

Ожидается: внешний адрес принят; managed address отклонён repository и DB
trigger; last-mile executor переводит документ в `held` до sign/broadcast.

### T16 — Merchant withdrawal happy path

Создание заявки сразу резервирует/debits merchant available balance по
debit-first модели. Approval следует frozen policy и maker-checker. Executor
выбирает ликвидный hot wallet, проверяет destination, fee budget, intent,
approvals и signer readiness, затем сохраняет signed envelope до broadcast.
После final receipt principal и network fee отражены ровно один раз.

### T17 — Rejection и pre-broadcast failure

Rejection до broadcast создаёт compensating merchant movement и освобождает
reservations один раз. Ошибка policy/Vault/provider/fee до broadcast не создаёт
chain tx и platform cost; документ получает `held` или `failed` согласно
retriability. Повтор rejection/failure не удваивает компенсацию.

### T18 — On-chain failed withdrawal

Broadcast tx с terminal failed/reverted receipt не считается выплатой. В одной
транзакции фиксируются settlement evidence, status, release principal
reservation и merchant compensation. Реально понесённый network fee учитывается
как platform cost. Повтор settlement идемпотентен.

### T19 — Treasury withdrawal

Create/approve/reject требуют стандартный `Idempotency-Key`; чувствительные
операции требуют step-up и maker-checker. Source — активный custody hot wallet;
destination — внешний treasury allowlist, не managed address. Platform revenue
reservation не может превысить available revenue. Intent/approval/signing и
settlement проверяются так же строго, как merchant withdrawal.

### T20 — Outbound network fee settlement

Для sweep, withdrawal, treasury и activation проверить successful и failed
receipt. Principal, processing revenue, resource rent и network fee не
смешиваются. `(basisType, basisId, txHash)` не создаёт второй settlement;
unknown/ambiguous receipt остаётся `held`, а не guessed success.

## 9. Reconciliation и инциденты

### T21 — Balance mismatch и allocation hold

Внести controlled divergence между chain balance и ledger projection.
Reconciliation создаёт один active incident и immutable observation на каждый
run, увеличивает occurrence count и ставит pool wallet на hold. После точного
совпадения следующий run закрывает incident и снимает только принадлежащий ему
hold. Read error не трактуется как нулевой баланс.

Повтор `(run, wallet, token)` не увеличивает occurrence count и не переписывает
hold acquisition details; изменённый payload того же run отклоняется. Проверить
20 конкурентных повторов, retry после resolution, независимые token/native и
external-outflow holds, новый mismatch после resolution. Сбой записи incident,
observation или hold должен откатить весь check и завершить run как `failed`,
а не скрыться за успешным статусом. Регрессия: `TestReconciliationHold*`.

### T22 — Unknown external outflow

Наблюдать outgoing transfer с managed wallet, отсутствующий в withdrawal,
sweep, treasury и signing authorizations.

Ожидается одна атомарная транзакция:

- `reg_crypto_external_outflows` получает отдельный immutable event identity
  `(network, txHash, eventKind, eventIndex)`, поэтому несколько transfers одного
  tx не схлопываются;
- создаются `reg_crypto_reconciliation_runs/items/observations` с kind
  `external_outflow`;
- создаётся отдельная immutable `reg_crypto_external_outflow_observations`,
  связанная composite FK с полной identity исходного outflow;
- source pool wallet получает allocation hold;
- network получает fail-closed hold `external_outflow`;
- повторное наблюдение обновляет confirmations, но не создаёт второй incident.

`matched_basis` требует exact effect по source/destination/contract/token/amount,
а не присутствие tx hash. `unauthorized_loss` создаёт ровно один immutable
`CryptoCustodyAdjustment`; его maker не может быть checker. Общий
`posting.Engine` создаёт wallet expense и platform `custody_loss`, но не merchant
balance; deferred DB constraint отклоняет `posted` без этого точного movement set.
Проведённые custody movements immutable и не поддерживают unpost.
`false_positive_or_reorg` требует сохранённый chain proof. Снятие принадлежащих
incident holds допускается только после классификации, успешной reconciliation,
завершённой позже posting decision, и auditируемого resolution другим actor. Ручное удаление
outflow/observation/action запрещено. В той же транзакции resolution закрывается
точный `reg_crypto_reconciliation_items`; другие incidents и holds не затрагиваются.

### T23 — Provider balance reconciliation

Сверить provider API balance с provider ledger после rental, failure, refund и
manual opening. Несовпадение создаёт incident без автоматического создания
дохода/расхода. Проведённый opening не удаляется и не распроводится.

Opening проверяется отдельным audited decision workflow. Политика версии `1`:
другой оператор принимает решение с отдельным permission `approve`/`reject`;
системный администратор (`is_admin` или роль `admin`) также может одобрить или
отклонить собственный opening. `security_admin` сам по себе не даёт исключения.
Для treasury, withdrawal, custody adjustment и resolution правила не меняются.

1. Maker выбирает immutable `observationId` из списка incidents, указывает reason;
   fresh object-bound step-up и `Idempotency-Key` обязательны.
2. Создаётся один pending-документ с ненулевым `createdBy`. Сумма, account/token,
   observation, basis и reason неизменяемы. Разные keys/операторы не создают
   второй активный opening того же account/token.
3. Решение требует свежий object-bound step-up, ожидаемую version и reason.
   Сохраняется режим `independent` или `admin_self` и `decisionPolicyVersion=1`.
   Самостоятельное решение обычного автора и нулевой maker запрещены.
   Для `admin_self` текущие полномочия повторно проверяются в БД с блокировками
   membership → user, сериализующими решение с отзывом роли/`is_admin`.
   Approval отклоняется при старой version, новом observation даже с той же
   суммой, изменившемся ledger или закрытом incident. Reject не создаёт движений.
4. Approval, единственный точный provider receipt, immutable audit и durable command result
   фиксируются атомарно. Opening не изменяет wallet/merchant/fee/platform ledgers.
   Сбой перед complete полностью откатывает команду; retry с тем же ключом безопасен.
5. Старое observation не закрывает incident после posting. Нужна новая сверка
   с провайдером и нулевой delta; старое свидетельство и документы сохраняются.

Обязательные negative/concurrency tests: 20 одинаковых creates/approvals;
разные keys на один opening; approve против reject; opening против rental charge;
изменение fingerprint при том же key; replay без fresh step-up; SQL delete,
deletion mark, unpost/repost, подмена автора/суммы/observation, posted без
точного movement или audit; UPDATE/DELETE observations, movements и decision audit.
Проверить обе формы административного права, отказ `security_admin`, запрет
самостоятельного решения обычного автора, обе стороны гонки с отзывом права,
rollback при ошибке аудита и отсутствие второго audit на replay. После отзыва
права ранее принятое решение остаётся неизменяемым историческим фактом.

В карточке существующего opening проверить: серверную `actionAvailability`,
причину недоступности, предупреждение о собственном решении администратора,
обязательную причину (до 2000 байт UTF-8), MFA cancel/expiry, duplicate submit,
потерю HTTP-ответа и version conflict. Пока результат неизвестен, исходный DTO
и ключ зафиксированы, противоположное действие недоступно. При stale version
карточка перечитывается, новая команда требует нового подтверждения.
UI-contract suite: `frontend/tests/provider-opening.spec.ts`; сетевые ответы
замоканы, реальные backend/DB гарантии проверяются отдельно integration suite.
`TestProviderOpening.*` запускается с `-race -count=100` в release gate.

Ручные funding/refund/adjustment через generic CRUD запрещены до реализации
серверного verified-receipt workflow: клиентские `chainReceiptVerified` /
`providerReceiptVerified` не являются доказательствами. Автоматические rental
charges/refunds остаются отдельным worker workflow. Это fail-closed ограничение,
а не PASS полного T23; real-provider и UI acceptance требуют отдельного evidence.

## 10. Webhooks

### T24 — Security, delivery и manual redelivery

Portal policy:

- read: не ниже Manager;
- create/update/delete/rotate/test: Owner + fresh step-up;
- manual redelivery: Owner + fresh step-up и только после terminal failed
  automatic job.

Machine API дополнительно требует минимальный webhook scope. Проверить:

1. HTTPS URL validation, private/link-local/loopback block, DNS rebinding,
   redirect block, TLS validation, timeout и response limit.
2. Event/body/event timestamp стабильны на всех retries; delivery ID,
   delivery timestamp и signature новые для каждой попытки.
3. Десять attempts по шкале до 72 часов, bounded jitter и ограниченный
   `Retry-After`.
4. Secret rotation: 24 часа dual signature; secret показывается один раз;
   старый Vault ref удаляется только после retry horizon.
5. Response excerpt очищен и не больше 8 KiB; secrets/PII не логируются.
6. Transaction rollback не оставляет event/job; causation key не допускает
   duplicate lifecycle event.

Webhook не является единственным источником истины: consumer дедуплицирует по
event ID и при сомнении читает canonical invoice.

## 11. Отказы, restart и конкуренция

### T25 — Fee resolution failure

Через controlled repository fault вернуть ошибку fee schedule/config. Нельзя
удалять реальный production-like тариф из общего стенда. Ожидается rollback
payment posting, invoice projection, merchant balance, fee movements,
event/outbox и sweep scheduling. Chain inbox сохраняет retriable evidence.

### T26 — Restart и exactly-once economic effect

Остановить worker в точках: после claim, после signed envelope persist, после
broadcast до DB update и после receipt до posting commit. После restart worker
использует тот же persisted envelope/tx hash, выполняет lookup перед resend и
не rebuild/re-sign ambiguous transaction. Economic effect и settlement один.

### T27 — Обязательная concurrency matrix

Каждый пункт запускается минимум 100 раз под `-race` и на PostgreSQL:

| Конкурентная операция | Обязательный результат |
|---|---|
| 20 одинаковых invoice create | 1 invoice, allocation, initial event |
| 2 разных invoice при одном free wallet | только 1 успешная allocation |
| 2 approve одного withdrawal | 1 frozen approval transition |
| 2 workers claim одного sweep | 1 lease/execution attempt |
| 2 workers reserve fee/native funding | budget не превышен, 1 idempotent funding |
| 2 webhook secret rotations | serial versions, оба retired refs сохранены |
| 2 EVM nonce reservations | уникальный nonce, unsigned tail reclaim only |
| одновременный settlement всех outbound kinds | 1 movement set на basis/tx |
| expiry против full payment | terminal state соответствует committed winner без потери credit |

Каждая строка имеет committed DB-level test; scheduled/manual workflow запускает
единый targeted regex с `-tags=integration -race -count=100 -p=1` и сохраняет
JUnit. В частности, invoice/allocation, approval, sweep claim, fee/native budget,
webhook rotation, EVM nonce, settlement всех outbound kinds и expiry/payment
race покрыты отдельными PostgreSQL integration tests. Отсутствие любого теста
или execution artifact у конкретного candidate делает T27 `BLOCKED`, даже если
unit test проходит.

## 12. Security и runtime

### T28 — Security controls

Проверить:

- portal RBAC/BOLA, Owner/Manager/Viewer и object-bound step-up;
- global admin CIDR mode `off/warn/enforce`, MFA, session revocation;
- merchant-specific CIDR policies отдельно для Portal и Merchant API;
- trusted proxy chain и spoofed forwarding headers;
- SSRF/DNS rebinding/TLS для webhook и RPC configuration;
- tenant/family separation Vault policies, tokenless worker, audit devices;
- отсутствие static `VAULT_TOKEN`, local signing authorization key и private
  keys в production-mode config;
- log redaction, rate limits, request limits и secret scanning;
- kill switch: новые allocations/outbound attempts остановлены, а finality и
  reconciliation уже broadcast операций продолжаются.

Проверка merchant CIDR обязана покрыть baseline Portal/API и per-key intersection,
staged `off → warn → enforce`, IPv4-mapped IPv6, deny при policy read failure,
cache invalidation не позднее пяти секунд, current-IP/recovery lockout guard и
system-admin recovery. Реализация и UI-contract существуют, но candidate получает
`PASS` только после full-stack прогона со spoofed forwarding headers.

### T29 — Watcher address refresh

После старта worker создать/активировать новый wallet. Без restart адрес должен
появиться в atomic monitored snapshot. Параллельный poll не видит partial map и
не создаёт data race. Checkpoint сдвигается только после успешной обработки.

### T30 — Multiple token contracts

В одной сети включить минимум два token contracts. Для каждого существует
отдельный watcher key/checkpoint; token разрешается по immutable
`network + contract`; чужой contract не кредитует invoice. Ошибка одного
contract не останавливает здоровый watcher другого и не продвигает его
checkpoint.

Runtime discovery и targeted preflight должны загружать полный каталог:
проверить более 1000 сетей, а также одну TRON/EVM-сеть с 1005 contracts и native
token в начале/конце сортировки. Неактивная сеть доступна targeted preflight,
но не запускается обычным discovery; inactive/deleted tokens исключены.
Некорректная конфигурация за прежней границей 1000 строк не должна скрываться
частичной выборкой. DB regression: `TestProcessorNetworkCatalog*`.

## 13. Финансовая сверка

Finance подписывает отчёт только после проверки по каждому token/network:

```text
canonical received = sum(canonical inbound chain events)
confirmed          = sum(final chain payments)
credited           = sum(posted merchant principal movements)
merchant available = credited - processing fees - active withdrawals/reservations + reversals
custody assets      = sum(reconciled managed wallet chain balances)
platform revenue   = posted processing/withdrawal revenue - active treasury reservations
platform costs     = settled network fees + settled resource rents
```

Допустимая разница всегда объясняется именованным open incident, finality lag
или active reservation. Необъяснимая ненулевая разница — `FAIL`. Нулевой
агрегат при read error — также `FAIL`.

Обязательные выборки отчёта:

- invoices/payments и их three-amount projection;
- merchant principal/fee movements и balances;
- wallet balances/reservations;
- sweeps/withdrawals/treasury плюс signing authorization;
- chain settlements и platform costs;
- reconciliation runs/items/observations/outflows;
- merchant events, jobs и delivery attempts;
- security/network holds и audit/event log.

## 14. Evidence bundle

Для каждого Txx приложить:

```text
scenario/verdict:
candidate identity:
network and UTC interval:
sanitized request/response:
invoice/payment/document IDs:
event IDs, tx hashes and block/finality evidence:
relevant immutable movement/observation IDs:
metrics/log trace IDs:
expected vs actual:
defect/incident link if FAIL or BLOCKED:
operator and reviewer:
```

CI manifest обязан фиксировать commit, schema, migration/config digest, точную
test command, UTC, JUnit/Playwright artifacts и ссылки на финансовые before/after
snapshots. Manifest без scenario-specific financial snapshots является только
индексом артефактов и не переводит Txx в `PASS`. Поэтому генератор принимает
явный mode: `verified` разрешён только целевому прогону конкретного Txx, а
`index` всегда сохраняет verdict `NOT_RUN`, даже если общий software job зелёный.

Screenshots без DB/chain/trace evidence не закрывают финансовый сценарий.
Ручной SQL, меняющий business state ради получения `PASS`, аннулирует прогон.

## 15. Release gate

Итог остаётся `NO-GO`, если выполняется хотя бы одно условие:

- любой Txx не `PASS`;
- schema не `76` или migration fresh/down/up не доказан;
- integration DSN suites, race, frontend, SDK или Playwright gate отсутствует;
- Nile и Sepolia не прошли отдельный conformance;
- merchant-specific CIDR policy не реализована;
- нет audited workflow классификации и снятия external-outflow/network hold;
- есть unexplained ledger/chain/provider delta или open unknown outflow;
- Vault/signing/RPC/webhook readiness fail-open;
- не проверены restart, concurrency, backup/restore, alert routing и kill switch.

После полного pre-production `PASS` документ разрешает только отдельное
голосование за low-value Mainnet canary по
[crypto-mainnet-go-no-go.md](../operations/crypto-mainnet-go-no-go.md). Он не
разрешает production launch.

## 16. Файловая карта

```text
internal/domain/crypto/payment_cycle_test.go                    — invoice/payment lifecycle и financial rollback
internal/application/command/                                  — durable application-command idempotency
internal/application/cryptoapp/external_outflow_service.go      — audited classification/adjustment/resolution FSM
internal/application/cryptoapp/external_outflow_detection_service.go — atomic exact-match/outflow/incident/observation orchestration
internal/application/cryptoapp/operational_hold_service.go      — owner-bound network/wallet holds
internal/application/cryptoapp/merchant_network_policy_service.go — Portal/API/per-key CIDR policy
internal/infrastructure/crypto_worker/                          — activation, sweep, withdrawal, settlement, reconciliation
internal/infrastructure/storage/postgres/crypto_repo/           — event/outflow/webhook/wallet persistence
internal/infrastructure/storage/postgres/portal_repo/           — portal read models и custody allowlist
internal/infrastructure/blockchain/tron/                        — TRON watcher/driver/funding
internal/infrastructure/blockchain/evm/                         — EVM watcher/finality/nonce
internal/infrastructure/storage/postgres/migration/             — schema contract и fresh/down/up
db/migrations/00006_crypto_core.sql                         — network/token baseline
db/migrations/00006_crypto_core.sql                         — merchant/wallet/API key baseline
db/migrations/00007_crypto_outbound.sql                        — sweep и merchant withdrawal baseline
db/migrations/00007_crypto_outbound.sql     — treasury/outflow/reconciliation baseline
db/migrations/00006_crypto_core.sql                   — EVM nonce, network holds, Sepolia
db/migrations/00006_crypto_core.sql           — allocation history и inbound inbox
db/migrations/00006_crypto_core.sql            — merchant events, jobs, deliveries и endpoint registry
scripts/dev/crypto-create-invoice.ps1                           — canonical invoice smoke request
frontend/tests/treasury-outflow.spec.ts                         — outflow UI-contract и idempotent mutation
frontend/tests/merchant-network-policy.spec.ts                  — staged CIDR UI-contract
frontend/tests/public-checkout.spec.ts                          — production HTML checkout/refresh/terminal/enumeration contract
frontend/tests/portal-financial-mutations.spec.ts               — stable-key invoice retry и withdrawal step-up
frontend/tests/portal-webhooks.spec.ts                          — Owner/Manager/Viewer, rotation и terminal redelivery
frontend/tests/security-negative.spec.ts                        — merchant-scoped BOLA и stale step-up fail-closed UI contract
frontend/playwright.config.ts                                   — HTML/JUnit/trace/screenshot evidence runner
.github/workflows/crypto-release-gate.yml                       — scheduled/manual T27 x100 и full gate
scripts/ci/write-crypto-evidence-manifest.sh                    — commit/schema/config/artifact manifest
```

## Связанные документы

- [crypto-processing-architecture.md](crypto-processing-architecture.md) — фактические компоненты и финансовая модель.
- [wallet-address-reuse.md](wallet-address-reuse.md) — allocation windows, cooldown и late attribution.
- [crypto-security-and-risk-review.md](crypto-security-and-risk-review.md) — security controls и residual risks.
- [merchant-webhooks-production-readiness.md](../operations/merchant-webhooks-production-readiness.md) — webhook conformance и canary.
- [production-deployment.md](../operations/production-deployment.md) — deployment, rollback и DR.
