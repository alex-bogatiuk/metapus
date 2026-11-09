# Криптовалютный процессинг

> **TL;DR:** Server обслуживает ERP, Merchant API, Portal и checkout; worker обнаруживает и проверяет chain effects, проводит финансовые документы и исполняет исходящие операции через Vault и durable workflow.
> **Тип:** Concept
> **Аудитория:** Developer, Admin
> **Связанные:** [Подробная архитектура](../crypto/crypto-processing-architecture.md), [Merchant API](../reference/merchant-public-api.md), [Метаданные](../METADATA.md)

## Компоненты и границы

| Контур | Реализация |
|---|---|
| HTTP и composition | `cmd/server`, `internal/infrastructure/http/v1` |
| Сценарии | `internal/application/cryptoapp`, `outboundapp`, `checkout`, `portalapp` |
| Бизнес-модель | `internal/domain/crypto`, `catalogs`, `documents`, `registers` |
| Worker lifecycle | `cmd/worker`, `internal/infrastructure/crypto_worker` |
| Chain drivers | `internal/infrastructure/blockchain/tron`, `evm`, `bootstrap` |
| Хранение | `internal/infrastructure/storage/postgres/crypto_repo`, `outbound_repo`, `money_movement_repo` |
| Custody и control signing | `vault-sign-plugins/tron`, `vault-sign-plugins/evm`, Vault Transit |

Domain не импортирует SQL/HTTP adapters. Worker выполняет orchestration и сетевые операции; durable SQL вынесен в repositories и application ports. Tenant DB и TxManager передаются через context. Текущая ожидаемая схема — **98**, источник — `internal/core/version/version.go`.

Migration 77 добавляет immutable accounting identity/occurrence/evidence index для новых проверенных TRON/EVM transfers. Migration 78 добавляет внутренний ReceiptIntakeService: custody + suspense + recognition + audit + outbox в одном коммите. Он допускается только на отключённой, освобождённой от незавершённых отправок сети и не подключён к автоматическому приёму или публичному API. Receipt не начисляет merchant balance/fee; включение сети и legacy movements по такому wallet/token блокируются до availability/reorg cutover. Действующий приём CryptoPayment ниже сохраняет прежний workflow. Границы реализации и следующие этапы: [план учёта](../howto/crypto-inbound-provider-accounting-plan.md), [карта custody writers](../reference/crypto-custody-writers.md).

Migration 79 подключает funding CLI к тому же principal registry. Обязательны persisted accounting identity и strict verification; EVM использует read-only quorum verifier. Один effect принадлежит funding либо receipt, а SQL guards отклоняют повтор через payment/operating receipt. CLI ограничен platform-owned wallet и внешним sender; старые funding rows не получают identity автоматически. Ledger, funding receipt, audit и outbox атомарны. Это подключение существующего operator workflow, не автоматическое признание неизвестного прихода или капитала.

Migration 80 подключает indexed CryptoPayment v1 в server/worker: strict proof, custody gross, merchant net, processing fee, registry, frozen economic snapshot и outbox коммитятся совместно. Физический приход остаётся у payment до v2. Новый generation требует полного сторно всех трёх регистров без смены fee/basis. Внутренний explicit adoption связывает текущий проведённый canonical legacy payment с исходной line на disabled/drained сети, не создавая денег. Unindexed legacy path сохранён. Доказанная orphaning indexed/bound payment требует независимого proof и создаёт incident/network hold даже без ingestion activation; автоматическая компенсация и снятие hold требуют отдельного recovery workflow.

Migration 81 подключает JIT native funding: независимый transfer proof получается вне финансовой транзакции; один registry owner связывает principal expense/receipt обоих controlled wallets одного custody owner. Principal, fee/cost, binding и outbox атомарны. Истёкший parent, kill switch или paused ingestion не отменяют финальный settlement; отсутствие proof оставляет его повторяемым без principal. Исторические settled fundings не переписываются, незавершённые требуют proof. Generic reversal/re-inclusion native funding ещё открыты.

Migration 82 переводит новые sweep на model 2: broadcast не создаёт principal и не освобождает источник; final successful transfer каждой строки получает independent indexed proof и один registry owner обеих controlled сторон. Principal, fee/cost, binding, outbox и attempt lifecycle атомарны. AML переносит exact net/fee split по movement IDs строки, даже если другие строки не завершены. Reverted tx создаёт только фактический fee без principal/storno. Старые model 1 завершаются прежним механизмом; adoption истории и reorg recovery model 2 ещё открыты. `posted` у v2 означает snapshot расходов на broadcast; версия модели возвращается в DTO и отображается в карточке. Порядок locks и ограничения: [custody writers, §10](../reference/crypto-custody-writers.md#10-финальный-sweep-схемы-82).

Migration 83 переводит новые treasury на model 2: principal и platform revenue expense появляются при final successful outgoing proof. До этого wallet/revenue reservations остаются active. Settlement атомарно записывает expense owner, actual fee/cost, exact revenue partitions, binding/outbox и consume резервов. Revert записывает только actual fee и release principal/revenue без storno. Native gas budget учитывает active principal reservations; сумма native sweep не расходует дневной network fee budget. Старые treasury model 1 завершаются прежним механизмом без автоматического adoption. Fee identity и recovery/reorg ещё требуют следующих этапов. Детали: [custody writers, §11](../reference/crypto-custody-writers.md#11-treasury-схемы-83).

Рабочие family adapters: TRON и EVM. Наличие TON/Solana в enum не означает наличие runtime. EVM signer v1 ограничен Ethereum Sepolia и allowlisted PYUSD/native ETH; дополнительные сети требуют отдельной реализации/приёмки. [Добавление сети](../howto/add-blockchain-network.md), [план TON](../crypto/ton-implementation-plan.md).

## Приём платежа

1. Merchant создаёт invoice через `/merchant/v1/invoices` с API key и `Idempotency-Key`; Portal использует отдельный JWT/merchant scope. `orderId`/`externalId` — бизнес-реквизиты, транспортная идемпотентность хранится отдельно.
2. Аллокатор выдаёт подходящий managed address с проверкой network policy, custody owner, готовности, holds, cooldown и watcher lag. Transient lease сохраняется в истории.
3. Watcher сохраняет chain observations и checkpoint с fencing; семейный verifier подтверждает transfer identity, canonicality и финальность. Для TRON действуют отдельные latest/solidified streams; для EVM — block/log scanner.
4. Durable inbox сопоставляет событие с allocation window и token. Повтор не создаёт второй финансовый факт. Неподходящие, дополнительные single-payment и retired-address поступления могут требовать review.
5. `CryptoPayment` после допустимой финальности проводит custody receipt, merchant net и processing fee. Invoice агрегирует received, confirmed и credited amounts.
6. `confirmed` означает chain finality; **`completed` означает достаточное зачисление merchant ledger**. Отгрузка заказа разрешается по canonical completed, а не по redirect или одному webhook о получении.

Invoice statuses: `created`, `partially_paid`, `paid`, `confirmed`, `completed`, `underpaid`, `expired`, `cancelled`, `reversed`. Late payment не открывает terminal invoice заново; публикуется отдельное lifecycle-событие. Persistent top-up имеет системный `invoice_kind=top_up`, а merchant references можно привязать отдельно.

Подробности: [адреса и late intake](../crypto/wallet-address-reuse.md), [реквизиты инвойса](../crypto/invoice-reference-ownership-design.md), [TRON ingestion](../operations/tron-block-ingestion.md), [RPC ingestion](../operations/rpc-ingestion.md).

## Финансовый учёт

`CryptoAmount` хранит точное неотрицательное целое arbitrary precision; БД использует `NUMERIC(78,0)`, HTTP — десятичные строки. Для signed deltas применяются отдельные типы. Масштаб берётся из token/currency metadata; деление на 100 или преобразование через float недопустимы.

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

[Webhook readiness](../operations/merchant-webhooks-production-readiness.md), [Auth/Security](auth-security.md), [security review](../crypto/crypto-security-and-risk-review.md).

## Сверка, запуск и приёмка

Reconciliation сравнивает chain, physical ledger и reservations, сохраняет incidents и holds. Network execution и адресная выдача не возобновляются только из-за зелёного healthcheck. Signed recovery и приём chain evidence продолжаются согласно workflow даже при запрете новых sends.

- [Выбор deployment](../operations/deployment-guide.md).
- [Локальная среда](../crypto/crypto-environment-setup.md) и [публичный testnet](../operations/public-nile-deployment.md).
- [Финансовые сценарии](../crypto/crypto-processing-test-scenarios.md), [Nile acceptance](../operations/crypto-nile-acceptance.md), [Mainnet go/no-go](../operations/crypto-mainnet-go-no-go.md).

Локальные тесты и mock fixtures не заменяют реальную Nile/Sepolia rehearsal и production acceptance. План daily liquidity-address screening и reviewed high resolution остаётся [отдельным планом](../howto/crypto-aml-liquidity-address-screening-plan.md).

## Merchant withdrawal custody recognition (84)

Новые CryptoWithdrawal model 2 на broadcast не списывают physical principal и сохраняют active principal reservation. WithdrawalRequest debit-first остаётся единственным списанием мерчанта. Independent outgoing proof, shared expense owner, binding к original merchant debit, actual fee/cost, reserve outcome, AML fact и final statuses/outbox сохраняются атомарно. Revert возвращает мерчанту только соответствующую сумму и не создаёт physical principal/storno. Старые model 1 завершаются прежним механизмом. API/портал возвращают accountingModelVersion. Подробности: [custody writers, §12](../reference/crypto-custody-writers.md#12-merchant-withdrawal-схемы-84).

## Подтверждённая потеря custody (85)

CryptoCustodyAdjustment model 2 требует independent indexed outgoing proof при maker/checker approval. Classification сама денег не списывает. Shared expense owner, physical principal, равный platform custody-loss cost, decision audit, reconciliation request и outbox сохраняются атомарно. Managed destination, known signed execution, prior owner, orphan или другая occurrence блокируют новое списание. Merchant balance не меняется. История model 1 не переписывается; generic compensation новой модели закрыт. См. [custody writers, §13](../reference/crypto-custody-writers.md#13-подтверждённая-custody-loss-схемы-85).

## Независимое признание сетевой комиссии (86)

Новые FeeReservation model 2 требуют отдельный NetworkFeeProof от двух независимых RPC sources. TRON подтверждает total fee с resource components, Ethereum mainnet/Sepolia execution/blob — фактический gas fee; неизвестные surcharge-поля блокируют признание. Immutable fee identity отделена от principal. Normalized settlement/components, proof/binding, native expense, равный platform cost и fee outbox коммитятся вместе; нулевая комиссия сохраняет факт без денежных строк. Activation использует единственного владельца CryptoNetworkOperation. Terminal replay проверяет прежнее outcome; replacement требует signed authorization и события в той же транзакции. История model 1 сохраняется без adoption. [Контракт и границы поддержки](../reference/crypto-custody-writers.md#14-независимое-признание-network-fee-схемы-86).

## Expected-transfer routing (87)

EventProcessor перед incoming/internal/outgoing exits выполняет exact economic lookup по persisted signed intent. Request/outbox атомарно передают работу исходному basis owner; independent indexed proof с однозначной coverage добавляет stable logical route. Existing funding/sweep/treasury/withdrawal/AML-line pollers получают nudge без смены lease, hold, tx или money. Pending/expired/ambiguous outcome не становится external payment/suspense. Новые SQL guards защищают shared posting и payment admission; history не переписывается. [Контракт, ограничения и pending query](../reference/crypto-custody-writers.md#15-маршрутизация-ожидаемых-переводов-схемы-87).


## Ручные корректировки (88)

Новые CryptoAdjustment model 2 имеют неизменяемый basis: source posting исходного признанного поколения платежа, unindexed legacy payment либо самостоятельная wallet variance. Sources замораживаются в proposal transaction; approval проверяет поколение и exact financial set вместе с accounting outbox. Введённый tx hash не занимает chain effect. Indexed legacy сначала требует explicit adoption. Исторические model 1 не переписываются. Это сохраняет физическое сторно CryptoPayment v1; новая attribution через suspense остаётся P4/P5. [Контракт](../reference/crypto-custody-writers.md#16-основание-ручных-корректировок-схемы-88).


## Ресурсные блокировки (схемы 89–91, этапы 92–95)

Общий scope/wallet helper берёт полный owner/network scope set до catalog wallets. Proof и batch capture получают все recipient resources до checkpoint/balance/effect; fee settle model 1/2 — payer до reservation. Maintenance policy берётся до wallet, treasury wallet — до revenue balance. PostingEngine блокирует union старых и новых wallets до validators/сторно. Это меняет сериализацию, сохраняя финансовые суммы и историю. [Контракт схемы 89](../reference/crypto-custody-writers.md#17-порядок-ресурсов-учёта-схемы-89).

Схема 90 дополняет legacy withdrawal/sweep selectors, draft linking и activation coordinator: document advisories и полный scope/wallet footprint предшествуют workflow rows. Занятый кандидат пропускается с освобождением частичных locks; withdrawal учитывает также scope заявки, sweep повторяет eligibility до записи. Native-funding operational updates/claims, handoff и sweep bulk expiry дополнены схемой 91: полный resource prefix и фиксированный набор кандидатов предшествуют row updates. Replacement/authorization publication callers дополнены этапом 92; общий аудит остальных SQL prefixes продолжается. [Контракт и проверки схемы 90](../reference/crypto-custody-writers.md#18-selectors-и-activation-схемы-90).

[Контракт и проверки схемы 91](../reference/crypto-custody-writers.md#19-native-funding-operational-updates-и-bulk-maintenance-схемы-91) описывают единый funding preflight, атомарный confirmation claim, immutable replay и фиксированный maintenance cohort. Финансовые правила подписанного/broadcast резерва и reconciliation holds сохраняются.

[Этап 92](../reference/crypto-custody-writers.md#20-публикация-execution-и-authorization--этап-92) получает execution advisories/scopes/wallets до replacement UPDATE и authorization INSERT, корректно различает AML line и legacy request basis, проверяет текущее состояние и точного signed predecessor. Signed-envelope persistence требует полного единственного expected effect; конфликт откатывается атомарно. Новая миграция не нужна, схема остаётся 91.

[Этап 93](../reference/crypto-custody-writers.md#21-queue-barriers-и-semantic-replay--этап-93) закрепляет read-committed snapshot для inventory/resource capture/ack и проверяет полный semantic replay signing tuple и normalized fee outcome/components/movements. Повтор сохраняет первоначальные signature deadline и accounting period. Коллизия pending settlement требует reconciliation. Схема остаётся 91.

[Этап 94, схема 92](../reference/crypto-custody-writers.md#22-reconciliation-identity-и-ручная-aml-cancellation--этап-94) защищает immutable identity и исходный target reconciliation request. Producer replay проверяет сохранённый payload и сохраняет completed/failed projection; два native-funding requests записываются атомарно после полного scope/wallet prefix. Upgrade требует разбора старых несовместимых targets. Ручная AML allocation/resource cancellation проверяет исходные reason/actor; изменённое основание возвращает conflict. Финансовые правила и owners сохраняются.

[Этап 95](../reference/crypto-custody-writers.md#23-пакетная-отмена-withdrawal-и-sweep-expiry--этап-95) проверяет audit payload batch cancellation/expiry и сохраняет отдельный credit только по новым remainder facts. AML approval/rejection/cancellation получают полный advisory/scope/wallet prefix до request rows и повторно сверяют footprint после ожидания. Planning требует read committed, cancellation/expiry — общей compensation/maintenance transaction; конфликт откатывает все изменения. Новая миграция не нужна, схема 92.

[Этап 96](../reference/crypto-custody-writers.md#24-resource-rental-charge-и-cumulative-refund--этап-96) исправляет cumulative refund regression: 400 → 200 → 700 больше не создаёт повторный credit. Меньший/чрезмерный refund отклоняется; charge/refund сверяют полный provider/platform набор и сохраняют первое признание. Account lock предшествует rental, rental lock — allocation settlement. Replay не переоткрывает terminal status и не перезаписывает confirmed usage/outcome; receipt network проверяется. Схема 92, legacy revert/storno и activation audit продолжаются.

[Этап 97](../reference/crypto-custody-writers.md#25-legacy-compensation-и-resource-allocation-replay--этап-97) связывает legacy compensation с exact original debit и posted owner, проверяет recorded outcome/fee/usage и весь physical/merchant/fee set. Preflight выполняется до fee writes. Resource reservation/broadcast replay проверяет dimensions/quantity/signed effect/network/event, сохраняя первое состояние. Missing/foreign/pending facts требуют reconciliation, а другая quantity — отдельной безопасной generation. Схема 92; activation selectors и оставшиеся operational callers продолжают P3 audit.


[Этап 98, схема 93](../reference/crypto-custody-writers.md#26-activation-selectorsstate-и-resource-release--этап-98-схема-93) переносит activation selectors/state на task/operation advisories → policy → scopes/wallets → task row. Занятые кандидаты откатывают partial locks; отсутствие native/payer не создаёт task. Terminal replay сохраняет timestamps/reason и не скрывает новый workflow запоздалым callback. SQL guard сохраняет task identity и exact immutable operation link; upgrade требует разбора несовместимой history. Resource release сравнивает первоначальный reason/event либо проверяет parent rental close без нового event. P3 audit продолжается; рабочая БД не обновлялась.


[Этап 99, схема 94](../reference/crypto-custody-writers.md#27-fencing-rental-maintenance--этап-99-схема-94) защищает rental maintenance новым token каждой попытки. Account → rental locks предшествуют проверке token и wall-clock lease; stale worker не может изменить state, провести refund или очистить новый claim. Unclaimed recovery отклоняет занятую lease, held сохраняет retry delay. Service проверяет ownership до внешней работы и guarded close после RPC. Старые leases сохраняются; для выкладки требуется остановить прежние workers. P3 audit продолжается; рабочая БД не обновлялась.


[Этап 100, схема 95](../reference/crypto-custody-writers.md#28-activation-cost-repair-fencing--этап-100-схема-95) защищает activation cost-repair отдельным token каждой попытки. Service проверяет claim до RPC; held/financial completion проверяют token, wall-clock lease и exact linked identity после общего resource prefix. Старый worker не снимает новую lease и не создаёт fee writes. Authoritative finality остаётся отдельным путём и отзывает token при completion. Legacy leases/financial history сохраняются, для выкладки остановить старые activation workers. Main execution fencing и общий P3 audit остаются открытыми; рабочая БД не обновлялась.


[Этап 101, схема 96](../reference/crypto-custody-writers.md#29-activation-execution-fencing-и-dispatch-recovery--этап-101-схема-96) передаёт основной activation claim token в budget/state/authorization/dispatch boundaries. Local DB callbacks атомарно проверяют lease, внешние signer/RPC остаются вне transaction. Pool slot и fee reserve коммитятся одной serializable transaction с network fee policy перед wallets; state/dispatch сохраняют read committed. Immutable sign intent блокирует второй Sign и release при неизвестном ответе. Поздние signed bytes и known broadcast/receipt metadata сохраняются по original binding, без нового dispatch и снятия чужой lease; finality сохраняет одного fee owner. Старые workers нужно остановить, rollback после dispatch audit запрещён. Общий P3 audit и operator forward recovery остаются открытыми; рабочая БД не обновлялась.

[Этап 102, схема 96](../reference/crypto-custody-writers.md#30-cryptonetworkoperation-metadataprotection-и-posting-prefix--этап-102) выравнивает metadata/protection/FOR UPDATE и post/unpost/repost существующего CryptoNetworkOperation с activation finality prefix. Union current/proposed/historical wallets проверяется после ожидания; manual-edit permission также читается под locks. Document commands требуют read committed, FOR UPDATE — явной transaction; serializable pool/fee reserve не меняется. Exact protection replay сохраняет actor/financial state, legacy repost — один net fee. Общие P3 audits продолжаются, рабочая БД не обновлялась.

[Этап 103, схема 97](../reference/crypto-custody-writers.md#31-повторы-инцидентов-и-финансового-сторно-платежей--этап-103-схема-97) проверяет повторы incident и существующего финансового сторно платежей. Новая compensation хранит immutable request_basis; replay требует того же основания и полного результата по custody/merchant/fee. Legacy NULL basis не дописывается автоматически. Read committed и actual document/movement validation обязательны; прежние workers остановить перед upgrade. Network incident повтор использует semantic legacy lookup и сохраняет first evidence/status/hold owner. Автоматическое сторно indexed платежей не включено, общий P3 audit остаётся открытым.

[Этап 104, схема 97](../reference/crypto-custody-writers.md#32-повторные-команды-постановки-и-снятия-запретов--этап-104) защищает общий API постановки/снятия operational holds. Read-committed owner gate без ожидания объединяет network/wallet cohort; exact payload/actor/reason replay не меняет timestamps, changed basis не снимает часть запретов. Сверка сохраняет initial observation в hold, последующие измерения пишет отдельно. New target после resolution запрещён, прямые SQL writers остаются в P3 audit; финансовые движения/схема не меняются.

[Этап 105, схема 98](../reference/crypto-custody-writers.md#33-прямые-sql-запреты-и-отдельные-основания-кошельков--этап-105-схема-98) распространяет hold replay/identity guard на прямые SQL insert/update/delete. Go и SQL используют общий nonblocking owner gate. Pool/preflight сохраняют первоначальный source и отдельные wallet-bound lifecycle для новых findings; legacy rows/DTO/денежные движения не меняются. Down после новых lifecycle facts закрыт; старые workers/controllers остановить перед upgrade.


[Этап 106, схема 99](../reference/crypto-custody-writers.md#34-отмена-заявки-на-кошелёк-и-первое-подтверждение-наблюдения--этап-106-схема-99) проверяет причину и ответственного повторной отмены заявки на кошелёк через приложение и прямой SQL. Обновление наблюдения за адресами сохраняет первое подтверждение каждого адреса/актива; новые lease/scan heights не меняют его историю. Просроченный исполнитель отклоняется после ожидания блокировки по текущему времени БД, в том числе при пустом списке. Миграция меняет функции без финансовых движений и backfill; перед upgrade/downgrade остановить прежние workers/controllers. P3 audit продолжается, рабочая БД не обновлялась.


[Этап 107, схема 100](../reference/crypto-custody-writers.md#35-первое-решение-проверки-риска-и-история-расхождений-счёта--этап-107-схема-100) сохраняет первоначальное время оценки AML и не продлевает срок решения при повторе. История invoice drift отделена от текущего счётчика: первые details/acknowledgement сохраняются, после устранения новый случай получает отдельный инцидент после двух наблюдений. Команды атомарны и используют общий SQL/Go invoice NOWAIT gate. Старая противоречивая история требует разбора. Перед upgrade остановить старые workers; Down сохраняет evaluated_at и запрещён после новой invoice history. P3 audit остаётся открытым; рабочая БД не обновлялась.


[Этап 108, схема 101](../reference/crypto-custody-writers.md#36-основания-попыток-проверки-и-очередь-активации--этап-108-схема-101) сохраняет оригинальные job identity/initial versions и отдельное основание каждой preflight попытки. Callback требует того же basis и текущей lease; used token не открывается заново. Claim/Save/Retry получают full scopes/wallet перед job, сетевой checkpoint читается вне transaction. Activation queue пропускает неподготовленную сеть при выборе готовой и повторно проверяет fixed wallet. Legacy sources не выдумываются; перед upgrade остановить старые workers, Down после новых registrations/attempts закрыт. P3 audit продолжается, рабочая БД не обновлялась.


[Этап 109, схема 102](../reference/crypto-custody-writers.md#37-начальные-блоки-и-владельцы-наблюдения--этап-109-схема-102) защищает исходный shadow block и отдельную первую финансовую границу существующего TRON reader. Mode changes сохраняют active boundary, financial mode требует activated_at; legacy противоречие блокирует upgrade. Stale/foreign asset metadata и другой watch owner отклоняются. Общий read-committed network try-gate используется cutover и callbacks, full wallet prefix берётся только для новых targets; driver повторяет busy вне SQL/RPC locks. Legacy history не восстанавливается предположительно, Down сохраняет столбцы и закрыт после новых activation facts. Это не включает автоматический intake/P12 новой модели. Старые workers/catalog writers остановить перед upgrade; рабочая БД не обновлялась.


[Этап 110, схема 103](../reference/crypto-custody-writers.md#38-первоначальные-сведения-внешних-списаний--этап-110-схема-103) сохраняет первое основание внешнего списания, прежних catalog owners и источник каждой новой observation. Старое сообщение не смешивает номер/хеш/подтверждения разных блоков. Полный source validation и early try-prefix защищают detection/workflow; busy возвращается в очередь без исчерпания попыток. Legacy NULL сохраняются, новые source facts требуют forward recovery вместо Down. Решения и новые эпизоды после закрытия остаются отдельной работой; автоматический intake/P12 не включён. Рабочая БД не обновлялась.


[Этап 111, схема 104](../reference/crypto-custody-writers.md#39-основания-и-повторы-решений-внешних-списаний--этап-111-схема-104) защищает основания классификации, approval/rejection и закрытия внешних списаний. Exact actor/reason/evidence replay возвращает сохранённое решение без повторного posting/release. DB clock/source, AFTER audit и deferred exact reconciliation/incident/hold cohort не допускают частичного решения. Legacy NULL остаются неизвестными; после новых фактов нужен forward recovery вместо Down. Автоматический intake остаётся открытым; сплошной аудит P3 прекращён 09.11.2026. Рабочая БД не обновлялась.
