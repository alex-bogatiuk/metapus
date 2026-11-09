# Учёт внешних криптопоступлений и авансов ресурсным провайдерам

> **TL;DR:** Подтверждённый внешний приход создаёт один физический учётный факт; назначение средств, обязательство мерчанту, риск и разрешение на расходование определяются независимо.
> **Тип:** Concept
> **Аудитория:** Developer, AI-Agent, Admin
> **Статус:** целевая архитектура от 05.11.2026. Схемы 77–104 реализуют identity/evidence index, внутренний maintenance-only receipt + suspense и общий registry для funding CLI, CryptoPayment v1 и двух controlled сторон JIT native funding и final per-line sweep v2. Broadcast principal новых sweep исключён, legacy model 1 сохраняется. Treasury model 2 признаёт outgoing principal и platform revenue expense только при final proof с сохранением резервов до результата сети. Merchant withdrawal model 2 сохраняет единственный debit-first расход, удерживает principal reserve до final proof и учитывает exact AML partial success/refund без physical storno. Custody loss model 2 требует independent indexed outgoing proof и связывает principal/loss cost/decision audit/reconciliation/outbox с общим owner. Новые FeeReservation model 2 требуют отдельный independent TRON/Ethereum execution+blob fee proof и один owner расхода/cost/binding/outbox; история model 1 сохраняется. Схема 87 добавляет exact expected-transfer routing: pending handoff исходному owner, independent logical proof binding и защита payment admission без снятия lease/hold. Схема 88 фиксирует source posting для новых manual payment compensations, замораживает selections и отделяет wallet variance без chain owner; historical model 1 сохраняется. Схемы 89–91 выравнивают порядок scope/wallet locks и early resource preflight, включая legacy selectors, activation coordinator, native-funding metadata/handoff и bulk maintenance; общий lock-order audit продолжается. Этап 92 выравнивает replacement/authorization publication, проверяет exact signed predecessor и полный expected-effect replay; схема остаётся 91. Этап 93 закрепляет read-committed capture/ack barrier и полную проверку semantic authorization/fee replay. Этап 94 (схема 92) защищает reconciliation identity/source target и проверяет исходное основание ручной AML cancellation. Этап 95 дополняет batch cancellation/expiry полным payload audit и AML request resource prefix до workflow rows, сохраняя credit только по новым remainder facts; схема 92. Этап 96 обеспечивает monotonic cumulative rental refund и полный charge/refund replay, сохраняя первое признание и terminal state; схема 92. Этап 97 связывает legacy compensation с exact posted basis/debits и проверяет recorded outcome/resource allocation replay; схема 92. Этап 98 (схема 93) выравнивает activation selectors/state и resource release replay, фиксирует неизменяемую task identity и operation link; несовместимая история требует разбора до upgrade. Этап 99 (схема 94) вводит rental maintenance claim fencing и блокирует refund/state writes от запоздалого worker. Этап 100 (схема 95) защищает activation cost-repair token каждой попытки и guarded completion до fee writes, сохраняя authoritative finality. Этап 101 (схема 96) закрывает основной execution claim, разделяя guarded dispatch и original signed/broadcast evidence recovery; unknown Sign не повторяется автоматически. Этап 102 выравнивает metadata/protection/posting prefix существующего CryptoNetworkOperation, сохраняя actor/financial history. Этап 103 (схема 97) фиксирует основание новых финансовых сторно и semantic replay incident, сохраняя старую историю. Этап 104 защищает повторы общего OperationalHold API и сохраняет initial evidence сверки; схема 97. Этап 105 (схема 98) добавляет SQL replay guard и отдельные wallet/source owner identities pool/preflight. Этап 106 (схема 99) проверяет исходное основание отмены pool intent и сохраняет first watch coverage с актуальной проверкой полномочий после lock wait. Этап 107 (схема 100) сохраняет clock первого решения AML и immutable историю отдельных invoice drift episodes. Этап 108 (схема 101) фиксирует основания preflight attempts и выравнивает выбор готового activation queue target. Этап 109 (схема 102) сохраняет основания существующей ingestion registration/activation и первого watch owner. Этап 110 (схема 103) фиксирует первое outflow basis, observation incident links, paired block projection и busy defer. Этап 111 (схема 104) фиксирует decision basis/audit/exact closure и безопасные повторы решений, задаёт конечную приёмку P3-R1–R8. Есть явный adoption и полностью компенсированные поколения payment. Автоматический intake, распределение, receipt-aware компенсации, остальные исходящие writers и переключение учёта ещё не подключены.
> **Связанные:** [План реализации](../howto/crypto-inbound-provider-accounting-plan.md), [Процессинг](../systems/crypto-processing.md), [Проведение](../systems/posting-engine.md), [Канонические контракты](../reference/crypto-canonical-contracts.md).

## 1. Границы решения

Решение покрывает:

- Депозит ресурсному провайдеру, перевод в пути, аренду, частичное исполнение, возврат и сверку.
- Входящие средства без allocation, с неверным поддерживаемым токеном, до начала lease, после его закрытия, на retired/deposit/liquidity/treasury/network-fee адрес.
- Поддерживаемый dust: точный custody-учёт независимо от порога merchant matching.
- Техническую регистрацию неподдерживаемых активов без автоматического создания финансового актива.
- Полную и частичную идентификацию, возврат, казначейскую классификацию, обработку невостребованных сумм.
- Дедупликацию между watcher, payment, sweep, JIT funding, funding CLI, исправлениями и backfill.
- AML-инвентарь, адресные ограничения, финальность, reorg, миграцию действующих данных.

Полнота наблюдения ограничена явно поддержанными chain families и видами эффектов. Для неподдерживаемых traces, rebasing/fee-on-transfer активов и протокольных наград, которые существующий decoder не доказывает, система сохраняет incident/coverage gap. Она не конструирует приход из одной разницы балансов.

Регистры ниже — управленческие субрегистры в натуральных единицах активов. Юридическое признание активов/обязательств и выгрузка в регламентированную бухгалтерию требуют отдельной mapping policy. Суммы разных токенов не складываются в одно балансовое равенство; оценка в отчётной валюте хранит курс, момент и источник отдельно.

Все новые имена объектов и API в документе являются предлагаемыми контрактами.

## 2. Текущее состояние и подтверждённые ограничения

Исходная инвентаризация выполнена по локальной схеме 76. Добавленная migration 77 индексирует identity/occurrence/evidence и не меняет перечисленные ниже денежные механизмы. Проверка production-данных, RPC, аккаунта iTRX и реального исполнения не выполнялась.

| Механизм | Текущее поведение | Следствие |
|---|---|---|
| [CryptoPayment](../../internal/domain/documents/crypto_payment/model.go) | Один документ создаёт custody receipt, merchant net и processing fee | Нельзя добавить первичный receipt, оставив прежний payment posting без изменений |
| [EventProcessor](../../internal/domain/crypto/event_processor.go) | Retired/unknown-token/dust/unmatched могут завершить обработку до создания payment; confirmation неизвестного payment обновляет review evidence | Независимому receipt требуется свой durable finality/canonicality lifecycle |
| [DecideMatch](../../internal/domain/crypto/wallet_allocation.go) | Attribution определяется историческим окном и токеном | timestamp_before_lease не означает поздний платёж; matching не должен определять существование custody |
| [InboundReviewRepo](../../internal/infrastructure/storage/postgres/crypto_repo/inbound_review.go) | Журнал ограничен additional_single_payment и retired_deposit_address; результат содержит текстовую reference | Это операторская отметка, а не доказанная финансовая развязка всех unmatched |
| [ValidatePosting](../../internal/infrastructure/storage/postgres/crypto_repo/block_ingestion.go) | Финансовая проверка специализируется на CryptoPayment | Проверку evidence нужно распространить на receipt и другие chain-backed документы |
| [FinancialReorgRepo](../../internal/infrastructure/storage/postgres/crypto_repo/financial_reorg.go) | Компенсация ищет custody/merchant/fee исходного CryptoPayment и учитывает уже выполненные reversal | Требуется версия для раздельных receipt и attribution; старый SQL нельзя применять к новым payment |
| [CryptoAdjustment](crypto-adjustment-v2-design.md) | Частичное сторно payment и свободная wallet-коррекция с variance | Исправление принадлежности средств следует отделить от изменения физического остатка |
| [AML observation](../../internal/infrastructure/storage/postgres/crypto_repo/aml_routing.go) | В enabled merchant scope видит доказанные поступления, включая unmatched/retired/dust, до attribution; финансовых движений не создаёт | Наблюдение уже существует; требуется объединить provenance, а не дублировать AML |
| [AML inventory SQL](../../db/migrations/00065_crypto_aml_inventory.sql) | Allocatable payment требует физическую строку с recorder_type=CryptoPayment; financial partitions — principal/service_fee | Новая модель требует изменения SQL views, guards, очередей и coverage |
| [BalanceObservation](../../internal/domain/crypto/balance_reconciliation.go) и [worker](../../internal/infrastructure/crypto_worker/reconciliation.go) | Наблюдение не содержит block height/hash; сумма ledger загружается отдельно от RPC-чтения | Текущая сверка не доказывает сравнение на одном chain/accounting checkpoint |
| [Provider operation](../../internal/domain/documents/crypto_provider_balance_operation/model.go) | Аудируемый opening; generic funding/refund/adjustment запрещены без verified-receipt workflow | Простое включение enum funding не реализует пополнение |
| [Rental settlement](../../internal/infrastructure/storage/postgres/outbound_repo/resource_rental_manager.go) | Active rental непосредственно пишет SQL cost/provider movements; close дописывает refund; стоимость распределяется по allocations | Финансовый lifecycle уже существует вне model generators; мигрировать нужно фактических writers |
| [iTRX adapter](../../internal/infrastructure/crypto/resources/itrx/adapter.go) | Balance() возвращает ошибку; balance наблюдается из ответа на заказ; Place сопоставляет ответ с Active | Отдельная периодическая сверка и точное подтверждение делегирования требуют доработки |
| [Signing purposes](../../internal/domain/crypto/signing_purpose.go) | Отдельного refund/provider_funding/provider_refund purpose нет | Для новых отправок нужны изменения application, intent/authorization, Vault и readiness |
| [EVM watcher](../../internal/infrastructure/blockchain/evm/watcher.go) | EventIndex контрактного лога равен log.Index внутри блока | При повторном включении tx индекс может измениться; новый accounting key нельзя свести к прежнему event_index |

## 3. Изменения существующих механизмов

| Изменение | Решение | Совместимость |
|---|---|---|
| Разделение payment posting | Новый CryptoCustodyReceipt владеет внешним физическим приходом; CryptoPayment v2 распределяет уже принятый receipt | Явная accounting_model_version, legacy v1 остаётся читаемым |
| Дедупликация chain effects | Общий реестр признания custody effects для всех writers | UNIQUE внутри payment или funding CLI больше не является достаточной защитой |
| AML ownership | Добавить unattributed и treasury ownership, связать merchant split с receipt allocation | Это изменение схемы inventory, не новый AML-провайдер |
| Refund | Отдельное обязательство/резерв из suspense и собственный outbound basis | Не создавать фиктивный merchant и не дебетовать merchant balance |
| Provider funding | Два независимых подтверждения: chain transfer и provider credit | Между ними отдельный актив в пути |
| Resource rental | Разделить provider charge, услугу в ожидании и оказанную услугу | Прямые SQL writers и model methods переводятся на один финансовый координатор |
| Сверка | Block-bound chain snapshot и финансовая проекция на том же checkpoint | Existing amount-only observations сохраняются как исторически ограниченные |
| Ручные исправления | Ownership correction отдельно от custody correction и chain compensation | Исторические движения не переписываются |
| Административные действия | Review outcome не заменяет финансовый документ; новые денежные решения — независимое подтверждение | Существующее admin_self исключение provider opening не расширяется автоматически |

## 4. Инварианты

### 4.1. Физическое признание

1. Подтверждённый, успешный, доказанный внешний перевод поддерживаемого актива на managed address отражается один раз независимо от invoice matching.
2. До finality хранится наблюдение; деньги не появляются в financially final custody ledger.
3. Неуспешная транзакция не создаёт token principal. Фактически списанная с managed payer сетевая комиссия остаётся отдельным расходом.
4. Внутренний перевод между managed wallets не является новым внешним поступлением. Его source/destination проводит один settlement owner.
5. Unknown expected outcome не разрешает альтернативному worker создать приход или новую отправку.
6. Dust threshold управляет сопоставлением/экономикой обработки, но не округляет и не удаляет физическое поступление поддерживаемого токена.
7. Ни API клиента, ни TX hash из обращения в поддержку не являются достаточным chain proof.

### 4.2. Принадлежность и обязательства

Подтверждённый внешний receipt первоначально создаёт custody и равную сумму в suspense. Для обычного идентифицированного платежа оба шага можно провести в одной DB-транзакции: receipt → attribution. Внутри коммита остаются разные документы и разные учётные основания.

Suspense означает сумму с неразрешённым экономическим назначением. Он не утверждает наличие долга конкретному клиенту и не является выручкой.

В нормальном canonical lifecycle:

    Принято по receipt = Свободно и нераспределено + Зарезервировано для решений + Распределено
    Gross merchant attribution = Merchant net + Договорная processing fee

Резерв не меняет итог обязательства: он уменьшает доступную для новых решений часть suspense. Перевод в refund payable одновременно потребляет suspense и создаёт равное обязательство возврата.

Каждый расход allocation capacity ссылается на receipt и неизменяемое решение. Частичные решения и конкурирующие команды не превышают доступную сумму. Финансовый возврат уже выплаченных средств или reorg не разрешается через отрицательный suspense: создаются отдельные deficit/receivable/loss facts и incident.

Для merchant-owned адреса custody owner неизменяем. Заявление, что деньги принадлежат другому merchant, требует расследования и отдельного допустимого межвладельческого процесса. Receipt allocation не меняет owner в SQL/Vault и не обходит existing custody policy.

### 4.3. Независимые оси состояния

| Ось | Примеры значений |
|---|---|
| Chain | provisional, final, orphaned, conflicted |
| Accounting | unposted, receipted, compensated |
| Attribution | unresolved, partially_resolved, resolved |
| Risk | unknown, low, medium, high; отдельные decision/evidence |
| Execution | available, reserved, dispatch_held, signed_outcome_unknown, settled |
| Review | awaiting_evidence, reviewed, escalated |

Финансовое признание принадлежности допустимо при unknown/high: установленное обязательство не исчезает из-за риска. Возможность вывода определяется отдельно. Reviewed/not_attributable не закрывает ненулевой suspense и не снимает AML/financial holds.

### 4.4. Натуральные единицы и противоположные стороны

Суммы — CryptoAmount/отдельный signed delta, NUMERIC(78,0), HTTP strings. Масштаб, сеть и контракт закреплены metadata/evidence. Symbol USDT не является asset identity.

Регистры накопления не образуют GL автоматически. Для каждого типа документа задаётся проверяемый набор встречных учётных эффектов. Проверка выполняется domain policy и deferred SQL constraint в общей транзакции. Это не глобальное суммирование всех custody и merchant регистров: оно задвоило бы экономический смысл.

При суммах выше диапазона NUMERIC(78,0) операция получает явный overflow conflict. Неподдерживаемые token mechanics не маскируются округлением.

## 5. Общая идентичность и признание chain effect

### 5.1. Два уровня идентичности

Существующие evidence bytes, decoder versions, economics hashes и EventIndex не переписываются.

Добавляется versioned accounting identity, вычисляемая из проверенного полного receipt:

    logical effect: network + canonical tx hash + effect kind + transaction-local effect ordinal
    occurrence: logical effect + block hash + verified economics fingerprint
    recognition: logical effect + accounting generation + controlled wallet side + ledger role

Для EVM transaction-local ordinal доказывается по порядку logs в receipt, а block-global log.Index остаётся alias наблюдения. Для TRON используется семейное доказанное соответствие существующему decoder. Native value, internal value и contract log не смешиваются. Unsupported internal traces остаются coverage gap.

Реестр alias связывает прежние watcher/inbox/proof identities с logical effect. Если ordinal нельзя доказать, effect уходит в conflict, а не сопоставляется только по сумме/адресам.

Изменившийся payload в той же occurrence — аномалия. Новое включение после доказанного reorg может иметь другие economics или результат исполнения: старая occurrence компенсируется, новая получает отдельную generation после повторной проверки. В каждый момент активна не более одной canonical generation.

### 5.2. Реестр признания

Предлагаемый reg_crypto_custody_effect_recognitions связывает effect generation с точными custody movement line IDs и владельцем проведения. Immutable facts описывают признание/компенсацию; текущий статус — производная проекция.

Один внешний receipt получает одного posting owner. У внутреннего transfer один owner проводит обе managed стороны, каждую на точную сумму. Commission/gas — отдельная роль доказанного fee effect; тот же principal не принимается за fee.

К реестру подключаются CryptoCustodyReceipt, legacy/new payment, sweep settlement, native funding, funding CLI, provider funding/refund, refund execution и исправления пропущенного chain effect. Generic manual correction без chain basis не может занять произвольный effect.

Exact expected effect проверяется по сети, активу/контракту, from/to, amount, kind/ordinal и persisted authorization/envelope. Совпадение tx hash или владение адресом отправителя недостаточно.

Watcher, увидевший ожидаемый внутренний перевод до settlement, регистрирует pending settlement и будит его owner. Он не создаёт внешний suspense. Истёкший workflow lease не передаёт право повторного проведения.

### 5.3. Полнота подключения writers

| Writer | Что закрепляет registry |
|---|---|
| Legacy CryptoPayment / новый CryptoCustodyReceipt | Одна incoming principal сторона |
| CryptoSweep / CryptoNativeFunding | Source expense и managed destination receipt одного transfer |
| Funding CLI | Та же incoming principal identity; отдельный канал запуска не создаёт второго владельца |
| CryptoWithdrawal / TreasuryWithdrawal / RefundExecution / ProviderFunding | Исходящий principal; incoming managed side при её допустимости получает того же owner |
| FeeBudget settlement / CryptoNetworkOperation | Реально списанный native fee; duplicate native fee writers не могут занять одну fee роль |
| CryptoCustodyAdjustment по external outflow | Доказанный ранее неучтённый расход; detection без approval не пишет расход |
| Reorg / FinancialAdjustment | Ссылка на компенсируемые строки и generation; correction не объявляет повторное получение тех же монет |

Для legacy writers, которые проводят fee и principal разными координаторами, registry хранит отдельные роли, но проверяет общий receipt. Нельзя по наличию network-fee recognition считать principal уже проведённым или наоборот.

## 6. Учётные объекты

| Объект | Назначение и ключевые данные |
|---|---|
| CryptoCustodyReceipt / doc_crypto_custody_receipts | Сеть, wallet, token, custody owner snapshot, logical effect/occurrence, proof, accounting generation, amount, причина первичной классификации |
| reg_crypto_receipt_suspense_movements | Остатки по receipt/token/custody scope; append-only receipt/expense/reversal; остаток не зависит от физического местонахождения после допустимого перемещения |
| reg_crypto_receipt_decision_reservations | Резервы доступного suspense под attribution/refund/classification; immutable intent, revisions и terminal release/application facts |
| CryptoPayment v2 | Receipt ID, allocation ID/amount, invoice/merchant, fee snapshot; suspense expense + merchant net + fee income; custody receipt отсутствует |
| CryptoTreasuryReceiptClassification | Receipt allocation и доказанное основание: external own account, receivable recovery, loan, contribution, income |
| cat_crypto_external_accounts и reg_crypto_external_position_movements | Подконтрольные биржевые счета/внешние требования; актив, network mapping, контрагент, договор; средства в пути отдельным position kind |
| reg_crypto_treasury_source_movements | Типизированные встречные позиции loan_payable/equity_contribution с counterparty/legal basis; не merchant balance |
| reg_crypto_platform_other_income_movements | Признанный прочий доход, включая допустимые rebates/unclaimed; отдельно от processing fee |
| CryptoReceiptRefundRequest | Claimant evidence, проверенный destination, receipt allocations, policy/approval snapshot, amount и fee treatment |
| reg_crypto_refund_payable_movements | Обязательство возврата в активе receipt; available/reserved/executed не подменяют друг друга |
| CryptoRefundExecution | Chain execution без фиктивного merchant debit и без автоматической withdrawal fee |
| CryptoProviderFunding | Durable outgoing intent, provider account, проверенный destination, chain principal/fee, pending credit и credit evidence |
| reg_crypto_provider_transit_movements | Актив в пути до подтверждения зачисления на provider account; отдельно от доступного provider prepayment |
| reg_crypto_provider_service_pending_movements | Уже списанная провайдером стоимость ещё не подтверждённой услуги/требования на возврат |
| Provider credit/charge/refund facts | Immutable statement/API evidence, vendor operation ID либо проверяемые cumulative totals и order binding |
| CryptoProviderBalanceOperation | Opening и доказанная exception-коррекция; обычные funding/refund исполняются специализированным workflow |
| CryptoReceiptIncomeRecognition | Закрытие suspense только по юридически и учётно допустимому основанию; timer создаёт review task |
| reg_crypto_unsupported_asset_observations | Contract/network, raw observed units, proof/decoder coverage, reason, policy version; не merchant/custody финансовый регистр |

Существующий reg_crypto_wallet_variance_movements сохраняется для доказанных учётных расхождений. Он не используется вместо suspense и не объявляется целиком пассивом.

У новых финансовых документов generic Create/Post/Unpost/repost отключены. System intake выполняется worker use case; денежное решение оператора — command.Executor + actor/permission + object-bound step-up + independent approval + обязательный аудит/outbox. Для provider opening сохраняется текущая отдельная policy с admin_self; у новых операций такого исключения нет.

## 7. Поток поступления и распределения

~~~mermaid
flowchart TD
    Proof[Доказанное chain observation] --> Identity[Accounting identity и occurrence]
    Identity --> Expected{Есть exact expected effect?}
    Expected -->|Да| Owner[Существующий settlement owner]
    Expected -->|Нет| Supported{Актив и effect поддерживаются?}
    Supported -->|Нет| Technical[Технический журнал и coverage review]
    Supported -->|Да| Final{Успешная финальная occurrence?}
    Final -->|Нет| Await[Durable finality monitoring]
    Final -->|Да| Receipt[CryptoCustodyReceipt: custody плюс suspense]
    Receipt --> Match{Есть доказанное назначение?}
    Match -->|Merchant| Payment[CryptoPayment v2: suspense в net и fee]
    Match -->|Treasury| Treasury[Классификация по экономическому основанию]
    Match -->|Нет| Review[Очередь расследования и ageing]
    Review --> Payment
    Review --> Refund[Refund payable и отдельное исполнение]
    Review --> Treasury
    Review --> Legal[Правовое решение о невостребованной сумме]
~~~

Для обычного matched payment receipt и attribution проводятся атомарно при наличии всех локальных доказательств. При ожидании manual evidence receipt проводится самостоятельно. Недоступность merchant matching/AML-провайдера не должна бесконечно задерживать доказанный физический учёт; при отказе автоматики остаётся durable retry и запрет использования неучтённого остатка.

### 7.1. Сценарии

| Сценарий | Первичный учёт | Разрешение |
|---|---|---|
| Нет allocation | Custody + suspense | Claim review → merchant/treasury/refund |
| Неверный поддерживаемый токен | Приход фактического токена + suspense этого же токена | Не конвертировать в ожидаемый токен; отдельный FX/recovery workflow при необходимости |
| timestamp_before_lease | Custody + suspense | Исследовать историческую принадлежность; не зачислять текущему арендатору |
| Late payment по покрывающему историческому lease | Custody + suspense → merchant attribution, если разрешает сохранённая политика | Terminal invoice не открывается повторно; отдельное late lifecycle event |
| Retired address | Custody + suspense, owner/address history сохраняется | Ручное решение с доказательствами; retired не возвращается в пул |
| Additional single payment | Самостоятельный receipt + suspense | Первый payment не теряет выбранное место; дополнительный receipt не завершает заказ автоматически |
| Dust поддерживаемого токена | Точная сумма custody + suspense | Группируемый review, отдельный budget скрининга; сумма не исчезает |
| Неподдерживаемый/спам актив | Техническое наблюдение | Проверка contract/mechanics → controlled token onboarding → admission исходного effect ровно один раз |
| Liquidity/treasury receipt без заявки | Custody + suspense | Назначение кошелька не является доказательством собственного капитала |
| Exact internal sweep/JIT | Existing settlement owner | Нет внешнего suspense, merchant credit или второй receipt |
| TRX снаружи для operating gas | Custody + suspense | После доказанной классификации — operating inventory; до неё не финансирует gas |
| Протокольная награда/unstaking | Только при поддержанном доказанном effect | Разделить возврат собственного principal и reward; иначе incident и adapter work |

### 7.2. Числовые примеры

Знаки ниже описывают экономический эффект. В текущем Metapus положительный platform expense записывается RecordTypeExpense; таблица не задаёт enum напрямую.

| Операция | Custody | Suspense | Merchant payable | Доход / другой актив / обязательство |
|---|---:|---:|---:|---|
| Неопознанные 1 000 USDT | +1 000 USDT | +1 000 USDT | 0 | 0 |
| Доказанная атрибуция с согласованной fee 20 USDT | 0 | −1 000 USDT | +980 USDT | Processing income +20 USDT |
| Частичная атрибуция 400 USDT с fee 8 | 0 | −400 USDT | +392 USDT | Processing income +8 USDT; suspense remainder 600 |
| Согласован возврат 600 USDT | 0 | −600 USDT | 0 | Refund payable +600 USDT |
| Успешный возврат 600 USDT | −600 USDT | 0 | 0 | Refund payable −600 USDT; gas отдельным токеном |
| Своё пополнение с биржи 1 000 USDT | +1 000 USDT, затем 0 | +1 000, затем −1 000 USDT | 0 | External own asset −1 000 USDT; капитал не растёт |
| Доказанный заём 1 000 USDT | +1 000 USDT, затем 0 | +1 000, затем −1 000 USDT | 0 | Loan payable +1 000 USDT |

Если внешняя asset position раньше не учитывалась, нельзя создавать необоснованный отрицательный остаток: сначала оформить доказанный opening внешнего счёта либо оставить классификацию pending.

### 7.3. Payment, fee и публичные проекции

Для v2 цепочка receipt → allocations допускает несколько частичных решений. Существующее ограничение «один payment на chain event» нельзя механически сохранить и одновременно разрешить несколько payment allocations.

Целевой контракт: CryptoPayment v2 представляет одну атрибуцию receipt к invoice/merchant. Его учётная сумма — attributed gross portion; отдельные поля хранят receipt ID, chain receipt amount, allocation ID и fee assessment ID. Ключ уникальности v2 — allocation identity, а не повторяющийся TX hash. Для v1 сохраняются прежние ограничения. Chain verification проверяет полный receipt, затем отдельный guard доказывает право allocation на его часть.

Обычный полный payment сохраняет прежние суммы. Частичный payment требует явного API/DTO-контракта: amount относится к атрибуции, chainAmount — к внешнему переводу. Это поведенческое изменение отражается в merchant API/OpenAPI/SDK и Portal; до готовности клиентов partial merchant attribution остаётся выключенной. Внутренняя partial refund/classification не требует выдавать старым клиентам неоднозначный payment.

Invoice received/confirmed projections не суммируют полный chainAmount каждого allocation. Связанные receipt contributions учитываются без повторов, credited amounts — по merchant attribution и её компенсациям. Completed требует достаточного ledger credit; suspense и один receipt сами заказ не закрывают. Single-payment selection выбирает внешний receipt, а не каждую его attribution как новый платёж.

Fee assessment закрепляет тариф и коммерческую область начисления. Для повторных portions одного receipt/merchant/invoice применяется разница fee от накопленного attributed gross, чтобы fixed/min fee не списывалась заново при каждом ручном разбиении. Fee(0)=0; net неотрицателен; округление выполняется в raw units. Иное правило требует явной договорной policy. Отмена attribution компенсирует фактически начисленные fee lines, не пересчитывая их по сегодняшнему тарифу.

### 7.4. Refund

1. Claimant доказывает право на распоряжение. TX hash, сумма или знание старого invoice сами по себе недостаточны.
2. Destination проверяется отдельно; возврат на from_address по умолчанию запрещён. Для биржи/контракта сохраняется доказанный recovery route.
3. Approval фиксирует receipt portions, token/network, destination, источник principal/gas, лимит fees, способ оплаты комиссии и risk decision. Экономический плательщик gas не заменяет custody owner подписывающего fee wallet: existing owner-based funding policy сохраняется.
4. Approval переносит выбранную сумму из suspense в refund payable и резервирует physical source. Если source ещё нельзя выбрать, обязательство остаётся неисполненным, без разрешения dispatch.
5. Успех: expense custody + уменьшение refund payable. Reverted: principal/payable остаются, фактический gas cost учитывается. Unknown: резервы и исходный envelope сохраняются.
6. Отмена до необратимой отправки возвращает сумму в suspense компенсирующими записями. Подписанный intent нельзя отменить только по TTL.
7. Для USDT principal и TRX gas нельзя вычитать raw amounts. Platform-paid gas — начальный поддержанный вариант. Удержание в токене возврата требует договорного основания, явной фиксированной суммы либо отдельной conversion quote, округления, лимита и раскрытого fee/reimbursement учёта.
8. После merchant attribution это уже merchant refund: используется merchant debit-first с собственным договорным основанием. Его нельзя провести ещё раз через suspense.

### 7.5. Невостребованные средства

Ageing policy создаёт задания на пересмотр и уведомления. 180 дней или другой внутренний срок не создаёт доход.

Income recognition хранит юрисдикцию/legal entity, вид права, применимое основание прекращения обязательства или иной допустимой классификации, даты, уведомления, ограничения, документ решения и independent approval. При неизвестном основании сумма остаётся suspense.

При решении перечислить сумму уполномоченному органу используется payable соответствующему получателю, а не доход. При допустимом признании дохода меняются suspense и other income; custody и AML-риск не изменяются.

## 8. AML, инвентарь и расходование

Существующее раннее ObserveVerification переиспользуется. Receipt не запускает повторный платный screening того же subject/generation. При выключенном AML scope действуют бухгалтерские suspense/refund reservations; отключение AML не делает чужие/неатрибутированные деньги расходуемыми.

Новая inventory-модель хранит financial ownership отдельно от риска:

- unattributed — доказанные средства с открытым suspense;
- merchant_principal и platform_service_fee — после merchant allocation;
- treasury и operating — после доказанной собственной классификации;
- refund_reserved — связанная часть исходного ownership, недоступная другим отправкам.

Unattributed не попадает в available merchant balance, treasury spend или operating gas. Сумма всех ownership partitions должна покрывать фактический финансовый inventory с учётом переводов и исходящих операций. Новый receipt создаёт revision даже без merchant credit.

Риск остаётся консервативным на wallet/token. Учётные portions не являются физически раздельными монетами. Новый внешний effect инвалидирует прежний dispatch permit до разрешения новых расходов; source/risk/batch/scope revisions проверяются перед подписью и отправкой.

Добавление Low к receipt не разрешает расход старого High. Привязка merchant не снимает unknown/high или чужие holds. Бухгалтерский quarantine не следует реализовывать одним глобальным sweep_blocked.

Область ограничения зависит от причины:

| Причина | Ограничение |
|---|---|
| Нераспределённая принадлежность | Unattributed amount недоступен; допустимость остальных сумм дополнительно зависит от wallet/token risk |
| Новый внешний effect с ещё не оценённым риском | Инвалидация актуального wallet/token permit и пересчёт допуска |
| Высокий риск/неполное coverage | Существующий консервативный policy gate; внутренние ordinary sweeps закрыты |
| Identity conflict, финансовое расхождение | Финансовый hold с собственным incident owner |
| Известный gas top-up | После доказанной собственной классификации отдельный operating provenance |

Защита от dust DoS — budgets, queue grouping, активы allowlist, повторно используемая свежая evidence и безопасная ротация адресов. Она не состоит в игнорировании поддерживаемого прихода или принудительном Low. Расход из смешанного wallet/token возможен только по явной доказуемой policy; начальная версия сохраняет conservative gating.

Scope без установленного merchant, включая platform treasury, требует owner-aware accounting/risk adapter. Нельзя подставлять произвольный merchant UUID в existing sys_crypto_aml_routing_scopes. До готовности такого adapter receipt учитывается, а новое расходование остаётся закрытым.

Связанные будущие изменения адресного screening и причинного распространения риска согласуются с [существующим AML-планом](../howto/crypto-aml-liquidity-address-screening-plan.md); этот документ не объявляет их реализованными.

## 9. Ресурсный провайдер

### 9.1. Funding

    Wallet principal → Provider transfer in transit → Provider prepayment

Источник — доказанный platform-owned available inventory; merchant principal, suspense и refund payable не финансируют платформенный депозит. Merchant-owned gas budget не превращается в общий аванс платформы. Если провайдер обслуживает несколько custody owners, стоимость и плательщик хранятся раздельно.

CryptoProviderFunding сохраняет account/provider/network/token, immutable destination и account binding, gross send amount, ожидаемый net credit/fee terms, max fee, idempotency, approval и persisted envelope.

После успешного final chain transfer: wallet expense + provider transit receipt. После доказанного provider credit: transit expense + provider prepayment receipt. Chain fee — отдельный platform cost. Если зачислено меньше, различие классифицируется по доказанным fee terms либо остаётся incident.

Частичные provider credits потребляют transit в пределах исходного transfer. Два частичных credit не создают два chain wallet расхода. При failed chain principal не признаётся; фактический gas учитывается. При timeout используется lookup исходной tx и credit reference, а не повторное funding.

Проверяемое provider credit evidence — API operation/statement с account, asset, transfer/reference, суммой и временем. Изменение общего balance не доказывает конкретное пополнение.

Текущий resource-provider port не предоставляет funding receipts и рабочий standalone iTRX balance. На этапе capability discovery проверяются реальные account APIs/выгрузки. Если подтверждающего API нет, первая версия допускает проверенный импорт statement с сохранённым оригиналом/digest и независимым подтверждением. Она не объявляется полностью автоматической.

Автопополнение включается только после готовности credit evidence, verified destination и budgets. Один активный intent учитывает pending credits и разрешённые резервы; параллельные workers не отправляют каждый свою сумму ниже порога. Жёсткий лимит не позволяет бесконечно пополнять account при задержке credit.

### 9.2. Аренда, услуга и возврат

Внешний order имеет независимые charge, delegation/delivery и refund facts. Успешный HTTP create или request-accepted reclaim не доказывает исполнение.

| Доказанный этап | Provider prepayment | Pending service asset | Platform expense |
|---|---:|---:|---:|
| Charge C, услуга ещё не подтверждена | −C | +C | 0 |
| Подтверждена готовность оплаченной услуги D | 0 | −D | +D |
| Refund R до признания услуги | +R | −R | 0 |
| Refund R ранее признанной услуги | +R | 0 | −R |

Если charge и delivery доказаны одновременно, обе записи выполняются одним коммитом. Успешность пользовательского вывода не определяет существование provider expense. Для фиксированной краткой аренды управленческая policy v1 признаёт стоимость при доказанной готовности услуги; иной период признания требует отдельной версии policy.

Затраты распределяются между несколькими execution allocations без нового расхода денег. Allocation хранит фактическую Energy/receipt и неизменяемую basis. Failed chain execution с фактическим расходом ресурсов участвует в себестоимости. Остаток неиспользованной аренды показывается отдельно.

    Net recognized expense = Сумма распределённых затрат + Нераспределённый остаток

Стоимость других токенов и прямой TRX burn не смешиваются с оплатой аренды. Bandwidth, activation и network fee учитываются по фактическому receipt; аренда Energy не гарантирует их отсутствие.

Provider cumulative charge/refund observations применяются как дельты относительно последнего доказанного состояния с lock и сравнениями payload. Decreasing refund/charge, refund > charge и конфликт order identity создают incident; никаких silent clamp или отрицания сохранённой истории. Correction vendor statement проводится отдельным компенсирующим фактом.

В опубликованной документации iTRX есть order query по serial/out_trade_no, но наличие клиентского ключа само по себе не доказывает договорную гарантию идемпотентного Place. Неопределённый create восстанавливается query; новый Place допускается только при доказанном отсутствии прежнего заказа и разрешении adapter policy. Противоречивые описания vendor statuses сверяются с реальными fixture/acceptance, а не превращаются в guessed success.

### 9.3. Сверка и opening

Provider reconciliation сравнивает internal ledger с statement/checkpoint, учитывая его timestamp/sequence, lag и операции после checkpoint. Признак stale или incomparable не становится ни нулём, ни разрешением correction.

Observation, incident и correction остаются разными объектами. Причины: missing funding, missing charge, missing refund, manual vendor order, stale response, documented provider fee, unexplained discrepancy.

Opening используется один раз для доказанной исходной позиции, не для выравнивания любого положительного расхождения. Existing negative pre-opening evidence сохраняется. Новые confirmed расходы не исчезают при нехватке внутреннего provider balance: отражается полный факт, блокируются новые заказы, открывается incident.

Provider cash refund на собственный wallet: provider balance уменьшается после доказанного списания в возвратный transit; chain receipt закрывает transit. Он не создаёт merchant credit или platform income. Обратный порядок evidence удерживает incoming в suspense/pending match до полного сопоставления.

## 10. Сверка на сопоставимом состоянии

### 10.1. Chain balance

BalanceObservation v2 содержит network/wallet/token, final block height/hash, finality policy, read source, read time, ledger checkpoint/generation, completeness и суммы.

Предпочтительный режим — историческое чтение chain balance на фиксированном final block и финансовая проекция всех доказанных effects до того же блока. Opening и manual corrections имеют явную basis height/financial effective checkpoint. Физические movements получают ссылку на recognition, из которой восстанавливается block binding.

Если provider не поддерживает историческое чтение, adapter возвращает доказанный stable finalized window: до/после фиксируются final head и ingestion/accounting watermark, а любое движение внутри окна делает результат incomparable. Обычный RPC без block context нельзя называть точной block-bound сверкой. Для периодически закрываемых контрольных точек допустим checkpoint с запретом новых отправок и полным drain; это отдельный операционный режим.

Сумма текущего mutable balance, загруженная до RPC, не используется как историческая сумма. Accounting worker должен завершить все эффекты до целевого watermark; неполнота даёт processing_lag, а не автоматическую денежную корректировку.

### 10.2. Три проверки

1. Доказанные chain effects ↔ custody movements по каждой controlled стороне.
2. Custody inventory ↔ ownership partitions/reservations и допустимая доступность.
3. Provider/external-account statements ↔ prepayment/transit/pending service и встречные позиции.

Balance mismatch не создаёт receipt на величину дельты. Backfill ищет пропущенные effects; только затем проводит подтверждённые документы. Настоящие excess/shortage требуют отдельного основания и согласования.

Новая успешная сверка снимает только hold соответствующего incident owner. Risk, refund, identity и custody holds остаются независимыми.

## 11. Reorg и исправления

До физического признания orphaned observation не создаёт финансового движения. После признания reorg обрабатывает весь receipt, даже если CryptoPayment не существует.

Для v2: остановить новые расходы источника; доказать noncanonical occurrence; компенсировать ещё не компенсированные receipt/attribution строки; инвалидировать AML/source permits; синхронизировать invoice и incident атомарно. Legacy v1 сохраняет существующий reorg path.

Ограничение финансового регистра не должно откатывать факт доказанной orphaning. Canonical invalidation, dispatch hold и durable compensation job фиксируются вместе; полный финансовый набор компенсируется в том же коммите, когда это допустимо. Если немедленная компенсация упирается в уже потраченный inventory/ограничение баланса, job остаётся compensation_pending, exposure публикуется отдельно, invoice не считается canonical completed и новые расходы закрыты. Денежные записи не создаются частично ради обхода constraint.

Уже исполненный refund/sweep/provider transfer не удаляется и не считается отменённым из-за reorg исходного поступления. Его chain факт сохраняется; возникает custody/ownership deficit или требование к ответственному контрагенту. Результат нельзя спрятать отрицательным suspense или исправлением чужого merchant баланса.

При повторном включении исходной tx новая final occurrence может вернуть financial coverage через новую accounting generation. Автоматическое повторное зачисление merchant после уже принятых manual resolution запрещено без generation-aware проверки сумм.

Ручное исправление ошибочной attribution возвращает net/fee в suspense без нового custody движения. Custody correction исправляет доказанную ошибку физического ledger, ссылается на missing/duplicate effect либо самостоятельное variance evidence и не заменяет refund.

Частичное сторно и reorg используют общий лимит compensation по source line. Единственный reversal coordinator задаёт поколение, порядок блокировок и остаток уже компенсированных сумм.

## 12. Конкурентность и транзакционные границы

RPC, vendor API, upload verification и Vault выполняются вне коротких DB-транзакций. Под транзакцией повторно проверяются immutable evidence refs, current canonicality/revisions, actor policy и остатки.

Application use cases владеют порядком операций. Domain policies вычисляют допустимые эффекты; PostgreSQL guards подтверждают точность и атомарность. PostingEngine остаётся общим механизмом записи, а не местом сетевых вызовов.

Порядок для нового inbound: enabled custody/risk scope → ordered wallet/token coordination keys → logical effect/generation → receipt/decision → financial resource locks в отсортированном порядке. Если один шаг затрагивает два wallet, блокируются оба до effect/receipt. Существующие lock orders нужно инвентаризировать до включения нового пути; добавление общего wallet lock поверх обратного legacy order запрещено.

Для provider commands сохраняется account → document → incident. Cross-domain funding settlement блокирует provider account до участвующих wallet/resources; все участвующие writers переводятся на тот же порядок. Vendor observation без wallet не захватывает wallet lock. Долгие сетевые ожидания внутри этих locks запрещены.

Один command commit содержит state transition, движения, recognition/consumption facts, reservation terminal facts, аудит и обязательный outbox. Повтор команды возвращает тот же result; тот же key с другим payload отклоняется.

## 13. ERP и fintech основания

ERP Insight: Odoo применяет suspense до банковского reconciliation; ERPNext отделяет Bank Transaction от её распределения по vouchers. Для Metapus из этих двух подходов следует раздельное хранение внешнего факта, attribution и журнала расследования, с типизированными документами и immutable ledger.

ERP Insight: ERPNext Journal Entry и механизмы reversal Odoo дают основание оформлять изменение назначения компенсирующим документом. Для Metapus correction ссылается на source line и version, сохраняя исходный chain факт.

Fintech Insight: идемпотентность команды и дедупликация внешнего effect решают разные задачи. Approvals связываются с точным intent; внешний timeout означает unknown outcome. В Metapus эти принципы реализуются через command.Executor, immutable effect recognition и persisted signed envelope.

Источники проверены 05.11.2026:

- [TRON confirmation semantics](https://developers.tron.network/docs/confirmation-semantics) — различие receipt, исполнения и finality.
- [iTRX order](https://develop.itrx.io/api/order-create.html), [query](https://develop.itrx.io/api/order-query.html), [reclaim](https://develop.itrx.io/api/order-recliam.html) — раздельные стадии заказа и асинхронный reclaim; полнота funding APIs здесь не подтверждается.
- [Odoo bank reconciliation](https://www.odoo.com/documentation/17.0/applications/finance/accounting/bank/reconciliation.html), [ERPNext Bank Reconciliation](https://docs.frappe.io/erpnext/bank-reconciliation), [ERPNext Journal Entry](https://docs.frappe.io/erpnext/journal-entry) — ERP-паттерны.
- [AASB 9, derecognition](https://standards.aasb.gov.au/node/2333) — пример принципа прекращения обязательства; не определяет применимое право и классификацию конкретного crypto receipt.

## 14. Файловая карта

Существующие точки изменения:

    internal/domain/crypto/event_processor.go
    internal/domain/crypto/wallet_allocation.go
    internal/domain/crypto/balance_reconciliation.go
    internal/domain/crypto/financial_adjustment.go
    internal/domain/crypto/signing_purpose.go
    internal/domain/documents/crypto_payment/
    internal/domain/documents/crypto_provider_balance_operation/
    internal/domain/documents/crypto_resource_rental/
    internal/application/cryptoapp/
    internal/application/outboundapp/
    internal/infrastructure/storage/postgres/crypto_repo/
    internal/infrastructure/storage/postgres/outbound_repo/
    internal/infrastructure/crypto_worker/
    internal/infrastructure/crypto/resources/itrx/
    internal/infrastructure/blockchain/tron/
    internal/infrastructure/blockchain/evm/
    internal/infrastructure/http/v1/handlers/inbound_review.go
    internal/content/
    vault-sign-plugins/tron/
    vault-sign-plugins/evm/
    db/migrations/
    frontend/app/(main)/crypto/inbound-reviews/

Новые пакеты, schema changes, этапы миграции и критерии приёмки перечислены в [плане реализации](../howto/crypto-inbound-provider-accounting-plan.md).

[Этап 98, схема 93](../reference/crypto-custody-writers.md#26-activation-selectorsstate-и-resource-release--этап-98-схема-93) реализует общий порядок activation selector/state locks, проверку полного footprint после ожидания, неизменяемую task/operation identity и сверку исходного resource release reason. Upgrade останавливается на несовместимой истории; рабочая БД не обновлялась. Остальные P3 callers, автоматический intake, attribution и cutover остаются в плане.


[Этап 99, схема 94](../reference/crypto-custody-writers.md#27-fencing-rental-maintenance--этап-99-схема-94) реализует rental maintenance fencing с token каждой acquisition и wall-clock lease check после account → rental locks. Старый worker не может снять новую lease или записать refund. Legacy leases и financial history сохраняются; для выкладки остановить workers прежней версии. Fencing остальных claims, общий P3 audit и последующие функциональные этапы остаются в плане.


[Этап 100, схема 95](../reference/crypto-custody-writers.md#28-activation-cost-repair-fencing--этап-100-схема-95) реализует activation cost-repair fencing, сохраняя отдельный authoritative finality/reconciliation путь и единственного fee owner. Проверка token/lease/linked identity предшествует финансовым writes; completion отзывает token и сохраняет признанную стоимость от позднего callback. Основной activation execution fencing и остальные P3/функциональные этапы остаются в плане.


[Этап 101, схема 96](../reference/crypto-custody-writers.md#29-activation-execution-fencing-и-dispatch-recovery--этап-101-схема-96) реализует activation execution fencing с durable dispatch audit. Новый dispatch требует текущего token; уже полученные signed bytes/known transaction metadata сохраняются для исходного owner после lease loss. Pool slot и fee reserve коммитятся атомарно в serializable transaction; state/dispatch сохраняют read committed. Неизвестный Sign остаётся held без второго Sign/release, finality признаёт fee один раз. Legacy history/HTTP DTO сохраняются; rollout требует остановки старых workers, Down после audit требует forward recovery. Остальные P3 writers и функциональные этапы остаются в плане.

[Этап 102, схема 96](../reference/crypto-custody-writers.md#30-cryptonetworkoperation-metadataprotection-и-posting-prefix--этап-102) выравнивает metadata/protection/row и posting boundaries существующего CryptoNetworkOperation. Общий workflow/operation/policy/scopes/wallet prefix включает historical/proposed wallets; permission и footprint проверяются после ожидания. Legacy repost сохраняет один net fee, model 2 guards остаются действующими. Новых financial owners/HTTP DTO нет; общий P3 audit продолжается.

[Этап 103, схема 97](../reference/crypto-custody-writers.md#31-повторы-инцидентов-и-финансового-сторно-платежей--этап-103-схема-97) защищает повтор существующего financial reorg сторно: current document/generation и полный source movement set, immutable request_basis, exact replay и проверка полного reversal. Network incidents получают deterministic key и semantic legacy lookup без удаления history. Новые generic financial recovery сценарии не включаются; P3 и P4–P13 продолжаются.

[Этап 104, схема 97](../reference/crypto-custody-writers.md#32-повторные-команды-постановки-и-снятия-запретов--этап-104) проверяет полный acquisition/resolution replay общих operational holds и весь owner cohort до write. Exact replay сохраняет history, новые observations сверки не меняют первоначальное основание запрета. New target завершённого owner требует новой identity, busy conflict требует fresh retry. Прямые SQL writers и общий P3 audit остаются открытыми.

[Этап 105, схема 98](../reference/crypto-custody-writers.md#33-прямые-sql-запреты-и-отдельные-основания-кошельков--этап-105-схема-98) защищает также raw SQL hold writers. Новые pool/preflight owners разделены по wallet/source; exact replay сохраняет first basis, новое finding создаёт отдельный lifecycle. Legacy history не переписывается, downstream readiness proofs сохраняются; общий P3 audit и функциональные P4–P13 остаются открытыми.


[Этап 106, схема 99](../reference/crypto-custody-writers.md#34-отмена-заявки-на-кошелёк-и-первое-подтверждение-наблюдения--этап-106-схема-99) различает неизменяемую команду отмены заявки и сохраняемое первое наблюдение за адресом. Отмена сверяет reason/actor даже через ON CONFLICT; новое наблюдение не переписывает первоначальную границу, а устаревший исполнитель получает отказ после ожидания блокировки. Нет новых financial owners, backfill или HTTP DTO. Остальные P3 writers и функциональные P4–P13 остаются в плане.


[Этап 107, схема 100](../reference/crypto-custody-writers.md#35-первое-решение-проверки-риска-и-история-расхождений-счёта--этап-107-схема-100) сохраняет первую оценку report/policy на исходный момент и проверяет сохранённый новый результат при повторной доставке. Mutable invoice reconciliation отделена от первой истории инцидента; closed case не открывается, новое расхождение получает отдельный lifecycle. Legacy sources/clock не выдумываются, деньги и DTO не меняются. Down сохраняет AML assessment clock; новая invoice history требует forward recovery. Остальные P3 writers и P4–P13 остаются в плане.


[Этап 108, схема 101](../reference/crypto-custody-writers.md#36-основания-попыток-проверки-и-очередь-активации--этап-108-схема-101) отделяет исходное preflight job от версий каждой попытки и сохраняет её полный basis. Поздние callbacks не завершают новую lease; source/expiry проверяются общим Go/SQL prefix. Activation queue — work hint с повторной readiness проверкой fixed wallet, не новое исполнение существующей task. Нового признания денег или derive/sign/broadcast нет. Legacy sources/unknown initial versions сохраняются, новые attempt facts требуют forward recovery. Остальные P3 writers и P4–P13 остаются в плане.


[Этап 109, схема 102](../reference/crypto-custody-writers.md#37-начальные-блоки-и-владельцы-наблюдения--этап-109-схема-102) разделяет сохранённые основания shadow registration и первой финансовой активации существующего reader. Новые головы при refresh не сдвигают active boundary, watch target сохраняет первого owner/start/source, существенная catalog identity не переписывается. Source metadata validation и общий read-committed network gate защищают атомарность и исключают stale snapshot обход; busy driver retry находится вне DB/RPC locks. Legacy NULL не угадываются; financial mode без activated_at блокируется до разбора; новые activation facts требуют forward recovery, начальные additive audit columns сохраняются при допустимом Down. Автоматический intake/P12 новой модели этим не включается; остальные P3 и P4–P13 остаются в плане.


[Этап 110, схема 103](../reference/crypto-custody-writers.md#38-первоначальные-сведения-внешних-списаний--этап-110-схема-103) отделяет первое основание внешнего списания от следующих observations и сохраняет исходных catalog owners. Block tuple и confirmations обновляются согласованно, floor не снижается. Source validation, read-committed try-prefix и отдельный busy defer защищают текущие writers без изменения финансовых движений/DTO. Legacy NULL не угадываются; новые source facts закрывают Down. Decision/reappearance audit P3, автоматический intake и P4–P13 остаются незавершёнными.


[Этап 111, схема 104](../reference/crypto-custody-writers.md#39-основания-и-повторы-решений-внешних-списаний--этап-111-схема-104) сохраняет immutable command/source/clock для решений внешних списаний. Повтор нового доказанного решения не повторяет money/audit/release; raw SQL требует согласованный seed/audit/source. Legacy NULL не достраиваются, новые decision facts требуют forward recovery. Отказ допускает одну новую классификацию с отдельным basis, повторное появление остаётся R2. [Восемь конечных пакетов P3](../reference/crypto-p3-acceptance.md) и индексы отделяют проверенные функции от оставшейся приёмки; P4–P13 не объявляются завершёнными.
