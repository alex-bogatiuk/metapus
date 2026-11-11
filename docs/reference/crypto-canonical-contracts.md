# Канонические контракты криптопроцессинга

> **TL;DR:** В каждом chain family работает один декодер; версия evidence, подписываемые bytes и бизнес-политика имеют независимые контракты.
> **Тип:** Reference
> **Аудитория:** Developer, AI-Agent
> **Связанные:** [JSON-контракты](json-contracts.md), [TRON ingestion](../operations/tron-block-ingestion.md), [Merchant custody](../operations/crypto-merchant-custody.md)

## 1. Алгоритмы и сохраняемые форматы

Accounting identity использует отдельную schema accounting-effect.v1. Она не меняет исторические transfer/economics/signing bytes. Новые успешные доказательства TRON/EVM содержат optional accountingIdentity; исторические proofs остаются читаемыми без него.

Logical key состоит из network, family, canonical transaction hash, effect kind и transaction-local ordinal. Для внутренних TRON effects используется также полный trace hash. EVM block-global log.Index остаётся observation alias и не входит в logical key. Block hash и нормализованные economics образуют отдельную occurrence.

Migration 77 индексирует persisted proofs в reg_crypto_accounting_effects, reg_crypto_accounting_effect_occurrences и reg_crypto_accounting_effect_evidence. Это evidence index без денежных движений и без выбора активной canonical generation.

Migration 78 добавляет reg_crypto_custody_effect_postings и reg_crypto_custody_effect_recognitions. Recognition ссылается на точную physical movement line; допускается не более одной principal receipt и одной expense стороны, в том числе при catalog aliases. Migration 80 разрешает поколения 2+ только для того же CryptoPayment/effect: predecessor, следующая posted version, полная компенсация прежних custody/net/fee и неизменяемый economic/fee snapshot обязательны. Receipt/funding/sweep/treasury остаются generation 1. Смена block hash сама не открывает второе признание.

Подключены внутренний maintenance-only CryptoCustodyReceipt, в migration 79 operator CryptoWalletFunding, в migration 80 indexed CryptoPayment v1, в migration 81 CryptoNativeFunding , в migration 82 sweep с одним owner обеих controlled principal сторон, в migration 83 treasury с одним outgoing expense owner. Funding/payment требуют strict persisted proof и immutable audit/snapshot/outbox; разные effects одной tx различаются по identity. Исторический funding без occurrence/posting требует будущего explicit adoption. EVM funding использует тот же независимый verifier, что processing, без signer/nonce dependencies. Различие display casing EVM address не меняет сохранённые proof bytes. Индекс и recognition сами не разрешают расходование; остальные legacy writers требуют отдельного перехода. Полный список: [custody writers](crypto-custody-writers.md).

Final native funding settlement получает proof до DB-транзакции и проверяет его также при paused ingestion/kill switch: восстановление принятого перевода не является новым dispatch. Новые external admission сохраняют policy guards. Native principal требует точного двухстороннего набора; fee/cost остаются отдельно, без собственного accounting identity role. Reverted tx не создаёт principal. Новые native generations и generic reversal закрыты до recovery workflow.

Sweep признаёт principal каждой строки только при final successful settlement. `VerifyExpectedTransfer` ищет transfer по signed economics и возвращает доказанный observation index/transaction-local identity; неоднозначные одинаковые effects отклоняются. Старый `VerifyTransfer` проверяет заданный index как прежде; persisted evidence и EconomicsHash не меняются. Broadcast не создаёт principal, reverted tx создаёт только actual fee. Signing basis и final outcome immutable. Indexed tx требует registry даже при другом legacy payment alias; unsigned alias не даёт второго principal. Fee proof/identity и sweep reorg/re-inclusion ещё требуют отдельного workflow.

Treasury использует тот же strict proof/index для expense controlled source; внешний recipient берётся из frozen approved document. Receipt-side AccountingTransferBasis по умолчанию сохраняется. На broadcast отсутствуют principal/revenue movements; final successful principal и actual fee имеют разные финансовые роли. Независимая fee identity и recovery следующих поколений ещё не подключены. Native principal reservation не превращает principal в fee: дневной network fee budget учитывает только fee, а JIT funding — principal + fee.

| Контракт | Текущее представление | Инвариант |
|---|---|---|
| TRON block decoding | Единственный `assembleBlock` с нормализацией receipts | RPC configuration не выбирает реализацию декодера |
| TRON transfer evidence | `TRONTransferEvidenceSchema = 2` | Число 2 и policy string `tron-transfer-v2` входят в существующий hash contract |
| EVM transfer evidence | `TransferEvidenceSchemaV1 = 1` | Схема 1 остаётся действующим EVM-форматом |
| Исторические events | Отсутствующий `decoderVersion` интерпретируется как схема 1 | Чтение старых bytes не повышает полноту evidence |
| Подписываемый unsigned fingerprint | Schema 1, `CanonicalizeLegacyV1` | Семейство, версия и точные исторические bytes связаны хешем |
| iTRX HMAC | `MarshalLegacyV1NoHTMLEscape` | Vendor body codec изолирован от product JSON |
| AML signing readiness | `aml_signing.v1` и `money_movement_intent.v2` | Совпадение поддерживаемых схем проверяется независимо от custody readiness |

Новый TRON capture всегда задаёт схему 2 явно. Значения 0, 1 и неизвестные значения отклоняются до сохранения событий. Fresh DDL фиксирует `decoder_version = 2`. Verifier, watcher, funding CLI и bootstrap проверяют тот же контракт.

Идентификаторы сохраняемых схем не перенумеровываются при переименовании Go-символов. Для нового byte-протокола требуется отдельная версия, спецификация и общие golden vectors производителей/потребителей.

## 2. Полнота AML coverage

`AMLCoverageIndex` получает все эффекты полного согласованного блока до фильтрации адресов и активов. `TransferCoverage` разделяет наблюдаемое количество и доказанность его полноты:

| Статус | Значение |
|---|---|
| `CoverageUnknown` | Полнота не доказана; в том числе неизвестная схема |
| `CoverageIncomplete` | У источника присутствуют исключённые traces |
| `CoverageComplete` | Для соответствующего вида эффектов доказан полный набор |

`ObservedEffects()` возвращает число наблюдаемых эффектов. `MatchingEffects()` возвращает число только при `CoverageComplete`, иначе 0. Таким образом сохраняется существующий wire-контракт proofs и AML requests. Отсутствие совпадений в полном блоке отличается от неизвестной полноты в доменной модели. Исключённые native traces не лишают полноты отдельно декодированные contract logs.

EVM verifier доказывает собственные условия полноты прямого native transfer и logs на согласованном final head; номер схемы сам по себе не является доказательством EVM coverage.

## 3. Авторизация и восстановление

AML scope является явным состоянием политики. Включённый scope требует точного зарегистрированного source plan. SQL error, отсутствие плана и несовпадение плана не превращаются в разрешение. Подпись и отправка дополнительно требуют durable dispatch permit.

`money_movement_intent.v1` используется действующим classless withdrawal при выключенном AML scope и treasury. `money_movement_intent.v2` связывает merchant withdrawal с risk class, explicit high/unknown acceptance и точными source lines. Производители указывают схему явно. Оба формата сохраняют исходные правила авторизации; treasury не получает искусственную AML-классификацию.

Допуск новой операции и наблюдение сохранённой попытки имеют разные обязанности. Сохранённые signed bytes, tx hash, reservations и authorizations используются при восстановлении без пересоздания идентичности. Неизвестный исход сохраняет резерв. Финансовые движения защищены от повторного применения; окончательное подтверждение обрабатывается независимо от текущей доступности нового funding source.

Domain проверяет значения и переходы. PostgreSQL обеспечивает конкурентную целостность и immutable evidence. Vault независимо проверяет подписанные полномочия и custody registry. Эти границы доверия сохраняют собственные проверки.

## 4. Источники native funding

`FeeFundingPolicy` хранит режим, лимиты и минимальные остатки. Глобального `feeWalletId` в HTTP/DTO, frontend и `sys_crypto_network_policies` нет.

Источник определяется по сети, неизменяемому custody owner и роли `network_fee`. Для merchant owner допустим только его fee wallet. Для платформенного кошелька при выключенном enforcement используется зарегистрированный платформенный fee wallet. При включённом enforcement непривязанный destination отклоняется. Ошибка точного поиска не запускает поиск другого owner.

Readiness проверяет native fee capital для каждого активного владельца. При резервировании минимальный остаток выбирается по роли заблокированного payer: `network_fee` использует `MinFeeWalletBalance`, остальные источники — `MinSourceWalletResidual`. Проверка применяется также при увеличении fee reservation.

## 5. Схема и операторские переходы

Исходные миграции 00041 и 00050 содержат финальную схему по pre-production правилам проекта. Временные CLI upgrades ingestion/decoder/shadow columns и старого EVM header encoding удалены вместе с их SQL helpers. `crypto-preflight` поддерживает read-only режимы `aml`, `balance`, `ingestion`, `rpc`, `evidence`.

Рабочие shadow pause/resume, fencing leases, canonical header validation, financial adjustments и settlement сохранены. Исторические proof bytes и audit records не переписываются. Multi-payment policy инвойса является бизнес-режимом и сохраняется при включении single-payment настройки для остальных подходящих инвойсов.

Редактирование исходной миграции не обновляет уже применённую tenant schema. Новый release требует финальных объектов схемы. Disposable стенд создаётся заново штатным provisioning; данные сохраняемого стенда требуют отдельного проверенного плана переноса схемы с backup/restore. Автоматического пересоздания или изменения существующих БД этот рефакторинг не выполняет. Старые schema-60 upgrades и исправление прежнего client header encoding больше не входят в поддерживаемый CLI.

## 6. Проверки

Golden-тест `TestCanonicalTransferEvidenceGolden` фиксирует source/canonical digest и economics hashes native/internal transfers до изменения реализации. Отдельные golden vectors unsigned fingerprint и iTRX защищают их byte-контракты.

Unit tests покрывают явные coverage states, точный custody owner, отсутствие обходного источника, остатки payer и recovery при выключенной текущей политике. PostgreSQL integration suite проверяет fresh schema, `Up → Down → Up`, capture fencing, immutable evidence, частичное сторно, fee reservations, dispatch и settlement с race detector. Оба Vault plugin modules проверяются отдельно. HTTP declarations и Zod schemas генерируются из Go DTO.

Проверено 04.11.2026:

- `go build ./...` — успешно.
- `go run ./cmd/integration-test` — полный suite на новых временных PostgreSQL 17 базах, `-tags=integration -race -count=1 -p=1`; 122 пакетов с тестами, ошибок нет.
- Оба отдельных Vault-модуля: `go test ./... -race -count=1` — успешно.
- `golangci-lint run ./...` — 0 issues.
- `go run ./cmd/http-contracts -check` — 436 актуальных HTTP contracts/schemas.
- Frontend: `tsc --noEmit --incremental false`, `npm run lint` — успешно.

Существующие tenant БД и запущенные сервисы не изменялись. Тесты использовали отдельный PostgreSQL-контейнер с временным хранилищем.

## Merchant withdrawal recognition (84)

Withdrawal использует тот же independent outgoing locator и строгий expense AccountingTransferBasis, что treasury. Proof получается до финансового transaction. Original WithdrawalRequest merchant debit не повторяется. Final principal owner, binding к исходному debit и authorization, actual fee/cost, normalized receipt, principal reserve outcome, AML fact и outbox атомарны. Failed v2 AML fact не имеет physical expense/refund; он подтверждает только точный merchant refund и освобождение резерва. Expected signed effect защищён от других legacy outflow writers. Изменение decoder/signing identity и новое accounting generation не добавлены. Детали: [withdrawal custody](crypto-custody-writers.md#12-merchant-withdrawal-схемы-84).

## Custody loss recognition (85)

Approval CryptoCustodyAdjustment использует strict expense AccountingTransferBasis по observed outflow: полный network/event/index/block/source/destination/asset/amount basis, независимый indexed canonical proof и общий owner. Матч signed expected effect требует исходного execution workflow. Detection/classification не заменяют proof. Loss cost отделён от network fee; merchant liability не меняется. Новые decoder/signing schemas не добавлены. См. [контракт и блокировки](crypto-custody-writers.md#13-подтверждённая-custody-loss-схемы-85).

## Network fee proof и recognition (86)

Fee role имеет независимый `network-fee.v1` key по network/family/canonical tx hash. NetworkFeeProof подтверждает controlled payer, canonical final outcome и точный total/components независимо от principal и transfer ordinal. TRON и Ethereum mainnet/Sepolia execution/blob используют два независимых источника/common head; неизвестный fee profile отклоняется. Fee reservations требуют proof/binding/exact financial set. Binding generation 1 не открывает автоматическую reorg-компенсацию. [Профили, блокировки и миграция](crypto-custody-writers.md#14-независимое-признание-network-fee-схемы-86).

## Expected-transfer routing (87)

Expected matching использует полный economic tuple и сохранённый signed intent; tx hash и controlled source сами по себе недостаточны. Logical route требует independent indexed proof/current confirmations/MatchingEffects=1 и одного expected transfer на authorization. Chain-specific ordinal берётся из accounting identity, без переписывания legacy event index. Pending request/outbox не создаёт financial owner или signing permission; original settlement basis сохраняется после lease expiry и получает nudge через прежний poller. Legacy/ambiguous/unsupported intents требуют review, без external fallback. [Подробности](crypto-custody-writers.md#15-маршрутизация-ожидаемых-переводов-схемы-87).


## Manual correction basis (88)

CryptoAdjustment фиксирует `basisKind` при proposal. `payment_attribution` (режим `payment_reversal`) сторнирует выбранные строки распределения текущего поколения платежа обратно в невыясненные receipt; custody не меняется, выбранные строки заморожены. `wallet_variance` (режим `wallet_balance`) не имеет chain recognition, даже если пользователь ввёл hash/proof ID в evidence. Корректировка не приобретает logical effect и не создаёт нового principal posting. DB проверяет точные противоположные движения и accounting outbox. [Полный контракт и границы](crypto-custody-writers.md#16-основание-ручных-корректировок).
