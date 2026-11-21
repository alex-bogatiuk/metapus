# Криптовалютный процессинг

> **TL;DR:** Server обслуживает ERP, Merchant API, Portal и checkout; worker обнаруживает и проверяет chain effects, проводит финансовые документы и исполняет исходящие операции через Vault и durable workflow.
> **Тип:** Concept
> **Аудитория:** Developer, Admin
> **Связанные:** [Подробная архитектура](../crypto/crypto-processing-architecture.md), [Merchant API](../reference/merchant-public-api.md), [Метаданные](../METADATA.md)

## Компоненты и границы

| Контур | Реализация |
|---|---|
| HTTP и composition | `cmd/crypto-server`, `internal/configuration/crypto/content/server.go`, `internal/infrastructure/http/v1` |
| Сценарии | `internal/configuration/crypto/application/cryptoapp`, `outboundapp`, `checkout`, `portalapp` |
| Бизнес-модель | `internal/configuration/crypto/domain/crypto`, `catalogs`, `documents`, `registers` |
| Worker lifecycle | `cmd/crypto-worker`, `internal/configuration/crypto/content/worker.go`, `internal/configuration/crypto/infrastructure/crypto_worker` |
| Chain drivers | `internal/configuration/crypto/infrastructure/blockchain/tron`, `evm`, `bootstrap` |
| Хранение | `internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo`, `outbound_repo`, `money_movement_repo` |
| Custody и control signing | `vault-sign-plugins/tron`, `vault-sign-plugins/evm`, Vault Transit |

Domain не импортирует SQL/HTTP adapters. Worker выполняет orchestration и сетевые операции, durable SQL вынесен в repositories и application ports. Tenant DB и TxManager передаются через context. Ожидаемую версию схемы задаёт [ExpectedSchemaVersion](../../internal/core/version/version.go).

Рабочие family adapters — TRON и EVM. Поддержку новой сети или family расширяйте по [инструкции добавления сети](../howto/add-blockchain-network.md).

## Приём платежа

1. Merchant создаёт invoice через `/merchant/v1/invoices` с API key и `Idempotency-Key`; Portal использует отдельный JWT/merchant scope. `orderId`/`externalId` — бизнес-реквизиты, транспортная идемпотентность хранится отдельно.
2. Аллокатор выдаёт подходящий managed address с проверкой network policy, custody owner, готовности, holds, cooldown и watcher lag. Transient lease сохраняется в истории.
3. Watcher сохраняет chain observations и checkpoint с fencing; семейный verifier подтверждает transfer identity, canonicality и финальность. Для TRON действуют отдельные latest/solidified streams; для EVM — block/log scanner.
4. Durable inbox сопоставляет событие с allocation window и token. Повтор не создаёт второй финансовый факт. Неподходящие, дополнительные single-payment и retired-address поступления могут требовать review.
5. После допустимой финальности `CryptoCustodyReceipt` признаёт физический приход. `CryptoPayment` в той же транзакции распределяет невыясненный остаток receipt в merchant net и processing fee. Invoice агрегирует received, confirmed и credited amounts.
6. `confirmed` означает chain finality; **`completed` означает достаточное зачисление merchant ledger**. Отгрузка заказа разрешается по canonical completed, а не по redirect или одному webhook о получении.

Invoice statuses: `created`, `partially_paid`, `paid`, `confirmed`, `completed`, `underpaid`, `expired`, `cancelled`, `reversed`. Late payment не открывает terminal invoice заново; публикуется отдельное lifecycle-событие. Persistent top-up имеет системный `invoice_kind=top_up`, а merchant references можно привязать отдельно.

Подробности в [архитектуре приёма](../crypto/crypto-processing-architecture.md#4-приём-платежа), [контракте входящего учёта](../crypto/crypto-inbound-provider-accounting-design.md) и [правилах повторной выдачи адресов](../crypto/wallet-address-reuse.md).

## Финансовый учёт

`RawAmount` хранит точное неотрицательное целое arbitrary precision; БД использует `NUMERIC(78,0)`, HTTP — десятичные строки. Для signed deltas применяются отдельные типы. Масштаб берётся из token/currency metadata; деление на 100 или преобразование через float недопустимы.

| Регистр | Экономический смысл |
|---|---|
| Crypto balance | Физический учёт managed wallet/token |
| Merchant balance | Обязательство платформы перед merchant |
| Crypto fee | Комиссионная выручка |
| Platform cost | Реальные network/resource расходы |
| Provider balance | Остаток/операции внешнего resource provider |

Crypto movements **append-only**: reversal добавляет противоположные записи, исходная история сохраняется. UPDATE/DELETE запрещены SQL guards. Триггеры поддерживают агрегированные balances. Ledger, principal reservations, fee income и operating gas — разные сущности.

[Posting Engine](posting-engine.md), [JSON и деньги](../reference/json-contracts.md), [корректировки](../crypto/crypto-adjustment-v2-design.md).

## Свип и исходящие операции

Свип консолидирует допустимые source wallets в collector того же custody owner. Настройки threshold/max age/retain amount определяют выбор; конкретный план и reservations сохраняются до подписи. TRON может использовать внешнюю аренду Energy и JIT native funding; EVM — gas/nonce и native funding.

Исходящая операция проходит durable reserve → build/authorization → Vault sign → сохранение signed envelope → broadcast → receipt/finality → settlement/posting. Неопределённый broadcast требует lookup и повторения сохранённых bytes. Истёкший lease сам по себе не разрешает повторную экономическую операцию. Fee и resource caps проверяются независимо от merchant principal.

JIT funding и свип связывают обе controlled стороны одним владельцем признания. При успешном финальном доказательстве строка свипа проводит principal и применяет свой AML net/fee split независимо от соседних строк. Казначейский вывод держит wallet/revenue reservations до финального исхода. Детали проводок и обработки revert описаны в [Posting Engine](posting-engine.md#3-неизменяемый-crypto-ledger).

`WithdrawalRequest` использует **debit-first**: merchant debit происходит при создании заявки. `CryptoWithdrawal` фиксирует исполнение и не списывает merchant balance второй раз. Approval policy, frozen intent и object-bound step-up связывают решение с точной суммой, активом и адресом. Treasury withdrawal — отдельный платформенный сценарий.

[Custody](../operations/crypto-merchant-custody.md), [ресурсный provider](../howto/add-resource-provider.md), [Vault TRON](../operations/vault-tron-production.md), [Vault EVM](../operations/vault-evm-sepolia.md).

## AML/KYT

AML scope включается явно для merchant/network/token. Миграция не объявляет старые остатки чистыми. Verified effects → pinned jobs/reports → decisions → wallet/token risk projection; paid submit ограничен отдельным локальным budget, poll не расходует его повторно.

Классы: low/medium/high/unknown. Полнота inventory, evidence TTL, source revisions, owner и batch учитываются при каждом допуске. Новый AML outbound использует class-specific source plan, explicit high/unknown acceptance и dispatch permits; classless legacy spending включённого scope закрыт.

Merchant principal, processing fee и operating gas имеют отдельные partitions. Token sweep reservations и immutable source plan связываются с chain/ledger evidence. Testnet emulator и opening inventory допустимы только для точных Nile/Sepolia identities; это не live AML acceptance.

[Архитектура AML](../crypto/aml-kyt-architecture.md), [runtime](../operations/crypto-aml-runtime.md), [risk projection](../operations/crypto-aml-risk-projection.md), [risk balances](../operations/crypto-aml-risk-balances.md), [allocations](../operations/crypto-aml-inventory-allocations.md), [resources](../operations/crypto-aml-resources.md), [batches](../operations/crypto-aml-batches.md), [merchant pools](../operations/crypto-aml-merchant-pools.md), [outbound](../operations/crypto-aml-outbound.md), [preflight](../operations/crypto-aml-preflight.md).

## API, webhooks и безопасность

Точный публичный контракт и curl-примеры поддерживаются в одном месте: [Merchant Public API](../reference/merchant-public-api.md) и `api/openapi/merchant-v1.yaml`. Portal endpoints и DTO определены `routes_portal.go` и HTTP contracts registry; ERP actions — соответствующими document registrations и capabilities.

Webhook использует immutable event ID/resource version, raw-body HMAC, snapshot endpoint/secret и повторную доставку. Получатель дедуплицирует ID атомарно с бизнес-переходом. Redirect checkout не является подтверждением оплаты. Notification/return URLs проходят `urlsafe.ValidatePublicURL`; dispatcher проверяет DNS, закрепляет IP и запрещает redirects для защиты от SSRF.

[Webhook readiness](../operations/merchant-webhooks-production-readiness.md), [Auth/Security](auth-security.md), [Merchant network policies](../crypto/crypto-processing-architecture.md#111-merchant-network-policies).

## Сверка, запуск и приёмка

Reconciliation сравнивает chain, physical ledger и reservations, сохраняет incidents и holds. Network execution и адресная выдача не возобновляются только из-за зелёного healthcheck. Signed recovery и приём chain evidence продолжаются согласно workflow даже при запрете новых sends.

- [Выбор deployment](../operations/deployment-guide.md).
- [Локальная среда](../crypto/crypto-environment-setup.md) и [публичный testnet](../operations/public-nile-deployment.md).
- [Финансовые сценарии](../crypto/crypto-processing-test-scenarios.md), [Nile acceptance](../operations/crypto-nile-acceptance.md), [Mainnet go/no-go](../operations/crypto-mainnet-go-no-go.md).

Для приёмки релиза нужны отдельные прогоны на Nile и Sepolia с фактическим chain evidence.

## Признание выплаты мерчанту

CryptoWithdrawal на broadcast не списывают physical principal и сохраняют active principal reservation. WithdrawalRequest debit-first остаётся единственным списанием мерчанта. Independent outgoing proof, shared expense owner, binding к original merchant debit, actual fee/cost, reserve outcome, AML fact и final statuses/outbox сохраняются атомарно. Revert возвращает мерчанту только соответствующую сумму и не создаёт physical principal/storno.

## Подтверждённая потеря custody

CryptoCustodyAdjustment требует independent indexed outgoing proof при maker/checker approval. Classification сама денег не списывает. Shared expense owner, physical principal, равный platform custody-loss cost, decision audit, reconciliation request и outbox сохраняются атомарно. Managed destination, known signed execution, prior owner, orphan или другая occurrence блокируют новое списание. Merchant balance не меняется. Generic compensation закрыт.

## Независимое признание сетевой комиссии

FeeReservation требует отдельный NetworkFeeProof от двух независимых RPC sources. TRON подтверждает total fee с resource components, Ethereum mainnet/Sepolia execution/blob — фактический gas fee; неизвестные surcharge-поля блокируют признание. Immutable fee identity отделена от principal. Normalized settlement/components, proof/binding, native expense, равный platform cost и fee outbox коммитятся вместе; нулевая комиссия сохраняет факт без денежных строк. Activation использует единственного владельца CryptoNetworkOperation. Terminal replay проверяет прежнее outcome; replacement требует signed authorization и события в той же транзакции.

## Expected-transfer routing

EventProcessor перед incoming/internal/outgoing exits выполняет exact economic lookup по persisted signed intent. Request/outbox атомарно передают работу исходному basis owner; independent indexed proof с однозначной coverage добавляет stable logical route. Existing funding/sweep/treasury/withdrawal/AML-line pollers получают nudge без смены lease, hold, tx или money. Pending/expired/ambiguous outcome не становится external payment/suspense. Новые SQL guards защищают shared posting и payment admission; history не переписывается.


## Ручные корректировки

CryptoAdjustment имеет неизменяемый basis: `payment_attribution` (сторно распределения текущего поколения платежа обратно в невыясненные receipt) либо самостоятельная `wallet_variance`. Sources замораживаются в proposal transaction; approval проверяет поколение и exact financial set вместе с accounting outbox. Введённый tx hash не занимает chain effect.


## Блокировки и повтор команд

Перед изменением workflow rows сервис получает полный набор затрагиваемых ресурсов. Общий порядок для custody — owner/network scopes перед wallets. Posting Engine блокирует объединение прежних и новых wallets до валидации и сторно. Outbound selectors пропускают занятых кандидатов и повторно проверяют eligibility после получения locks.

Повтор команды сверяет сохранённое основание и возвращает прежний результат. Замена подписанной операции требует точного predecessor и актуального execution state. Опоздавший callback может сохранить доказательство для исходной операции, но не открывает новый dispatch и не снимает чужую lease. Детали — в [метаданных исполнения и восстановления](../METADATA.md#исполнение-и-восстановление-криптоопераций).
